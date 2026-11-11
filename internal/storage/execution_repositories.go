package storage

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type executionRepository struct{ store *Store }

func (r *executionRepository) Admit(ctx context.Context, admission ExecutionAdmission) (TaskExecution, error) {
	return r.admit(ctx, admission, false)
}

// AdmitIfTaskRevisionCurrent binds a product execution to the task revision
// observed at admission. It leaves idempotent replays untouched, but rejects a
// first admission whose task is absent or has advanced.
func (r *executionRepository) AdmitIfTaskRevisionCurrent(ctx context.Context, admission ExecutionAdmission) (TaskExecution, error) {
	return r.admit(ctx, admission, true)
}

func (r *executionRepository) admit(ctx context.Context, admission ExecutionAdmission, requireCurrentTaskRevision bool) (TaskExecution, error) {
	e := admission.Execution
	p := admission.Plan
	if e.ID == "" || e.TaskID == "" || e.CorrelationID == "" || e.IdempotencyKey == "" || p.Version == "" || p.PlanDigest == "" {
		return TaskExecution{}, fmt.Errorf("storage: invalid execution admission")
	}
	tx, err := r.store.db.BeginTx(ctx, nil)
	if err != nil {
		return TaskExecution{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var existing TaskExecution
	if err := scanExecution(tx.QueryRowContext(ctx, r.store.bind(`SELECT id,task_id,task_revision,status,revision,mode,plan_version,plan_digest,capability_digest,projection_digest,parent_run_id,correlation_id,idempotency_key,input,result,failure_class,created_at,updated_at FROM task_harness_executions WHERE idempotency_key=?`), e.IdempotencyKey), &existing); err == nil {
		return existing, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return TaskExecution{}, err
	}
	if requireCurrentTaskRevision {
		if e.TaskRevision <= 0 {
			return TaskExecution{}, ErrRevisionConflict
		}
		taskQuery := `SELECT revision FROM tasks WHERE id=?`
		if r.store.driver == "postgres" {
			taskQuery += ` FOR UPDATE`
		}
		var currentTaskRevision int64
		if err := tx.QueryRowContext(ctx, r.store.bind(taskQuery), e.TaskID).Scan(&currentTaskRevision); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return TaskExecution{}, ErrRevisionConflict
			}
			return TaskExecution{}, err
		}
		if currentTaskRevision != e.TaskRevision {
			return TaskExecution{}, ErrRevisionConflict
		}
	}
	now := time.Now().UTC()
	e.Revision = 1
	if e.Status == "" {
		e.Status = ExecutionAdmitted
	}
	e.CreatedAt, e.UpdatedAt = now, now
	if e.Result == nil {
		e.Result = []byte{}
	}
	if e.FailureClass == nil {
		e.FailureClass = []byte{}
	}
	if p.ExecutionID == "" {
		p.ExecutionID = e.ID
	}
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	if _, err := tx.ExecContext(ctx, r.store.bind(`INSERT INTO task_harness_executions(id,task_id,task_revision,status,revision,mode,plan_version,plan_digest,capability_digest,projection_digest,parent_run_id,correlation_id,idempotency_key,input,result,failure_class,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`), e.ID, e.TaskID, e.TaskRevision, e.Status, e.Revision, e.Mode, e.PlanVersion, e.PlanDigest, e.CapabilityDigest, e.ProjectionDigest, e.ParentRunID, e.CorrelationID, e.IdempotencyKey, e.Input, e.Result, e.FailureClass, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
		return TaskExecution{}, fmt.Errorf("admit execution: %w", err)
	}
	if err := insertPlan(ctx, tx, r.store, p); err != nil {
		return TaskExecution{}, err
	}
	if err := tx.Commit(); err != nil {
		return TaskExecution{}, fmt.Errorf("commit admission: %w", err)
	}
	return e, nil
}

func (r *executionRepository) GetExecution(ctx context.Context, id string) (TaskExecution, error) {
	var e TaskExecution
	err := scanExecution(r.store.db.QueryRowContext(ctx, r.store.bind(`SELECT id,task_id,task_revision,status,revision,mode,plan_version,plan_digest,capability_digest,projection_digest,parent_run_id,correlation_id,idempotency_key,input,result,failure_class,created_at,updated_at FROM task_harness_executions WHERE id=?`), id), &e)
	if errors.Is(err, sql.ErrNoRows) {
		return TaskExecution{}, ErrNotFound
	}
	return e, err
}

func (r *executionRepository) UpdateExecution(ctx context.Context, id string, expectedRevision int64, status ExecutionStatus, result, failure []byte) (TaskExecution, error) {
	current, err := r.GetExecution(ctx, id)
	if err != nil {
		return TaskExecution{}, err
	}
	if current.Revision != expectedRevision {
		return TaskExecution{}, ErrRevisionConflict
	}
	if current.Status != status && !validExecutionTransition(current.Status, status) {
		return TaskExecution{}, fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, current.Status, status)
	}
	if result == nil {
		result = []byte{}
	}
	if failure == nil {
		failure = []byte{}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := r.store.db.ExecContext(ctx, r.store.bind(`UPDATE task_harness_executions SET status=?,result=?,failure_class=?,revision=revision+1,updated_at=? WHERE id=? AND revision=?`), status, result, failure, now, id, expectedRevision)
	if err != nil {
		return TaskExecution{}, err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		if _, getErr := r.GetExecution(ctx, id); errors.Is(getErr, ErrNotFound) {
			return TaskExecution{}, ErrNotFound
		}
		return TaskExecution{}, ErrRevisionConflict
	}
	return r.GetExecution(ctx, id)
}

// CommitExecutionIfTaskRevisionCurrent commits a product terminal execution
// only while both the application execution and its authoritative task remain
// at the revisions admitted for this run. Runtime evidence is intentionally
// persisted before this product-side commit, so a task conflict never erases a
// completed native outcome or causes it to be re-dispatched.
func (r *executionRepository) CommitExecutionIfTaskRevisionCurrent(ctx context.Context, id string, expectedExecutionRevision, expectedTaskRevision int64, status ExecutionStatus, result, failure []byte) (TaskExecution, error) {
	if expectedExecutionRevision <= 0 || expectedTaskRevision < 0 {
		return TaskExecution{}, fmt.Errorf("storage: invalid conditional execution commit")
	}
	if result == nil {
		result = []byte{}
	}
	if failure == nil {
		failure = []byte{}
	}
	tx, err := r.store.db.BeginTx(ctx, nil)
	if err != nil {
		return TaskExecution{}, err
	}
	defer func() { _ = tx.Rollback() }()
	query := `SELECT id,task_id,task_revision,status,revision,mode,plan_version,plan_digest,capability_digest,projection_digest,parent_run_id,correlation_id,idempotency_key,input,result,failure_class,created_at,updated_at FROM task_harness_executions WHERE id=?`
	if r.store.driver == "postgres" {
		query += ` FOR UPDATE`
	}
	var current TaskExecution
	if err := scanExecution(tx.QueryRowContext(ctx, r.store.bind(query), id), &current); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return TaskExecution{}, ErrNotFound
		}
		return TaskExecution{}, err
	}
	if current.Revision != expectedExecutionRevision {
		return TaskExecution{}, ErrRevisionConflict
	}
	if current.TaskRevision != expectedTaskRevision {
		return TaskExecution{}, ErrRevisionConflict
	}
	if current.Status != status && !validExecutionTransition(current.Status, status) {
		return TaskExecution{}, fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, current.Status, status)
	}
	var taskRevision int64
	taskQuery := `SELECT revision FROM tasks WHERE id=?`
	if r.store.driver == "postgres" {
		taskQuery += ` FOR UPDATE`
	}
	if err := tx.QueryRowContext(ctx, r.store.bind(taskQuery), current.TaskID).Scan(&taskRevision); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Existing callers may admit synthetic task IDs without a row. Keep
			// those legacy executions compatible; real task rows are authoritative
			// whenever present and are always checked below.
			if expectedTaskRevision != current.TaskRevision {
				return TaskExecution{}, ErrRevisionConflict
			}
			taskRevision = expectedTaskRevision
		} else {
			return TaskExecution{}, err
		}
	}
	if taskRevision != expectedTaskRevision {
		return TaskExecution{}, ErrRevisionConflict
	}
	now := time.Now().UTC()
	res, err := tx.ExecContext(ctx, r.store.bind(`UPDATE task_harness_executions SET status=?,result=?,failure_class=?,revision=revision+1,updated_at=? WHERE id=? AND revision=?`), status, result, failure, now.Format(time.RFC3339Nano), id, expectedExecutionRevision)
	if err != nil {
		return TaskExecution{}, err
	}
	if rows, _ := res.RowsAffected(); rows != 1 {
		return TaskExecution{}, ErrRevisionConflict
	}
	if err := tx.Commit(); err != nil {
		return TaskExecution{}, err
	}
	current.Status = status
	current.Result = append([]byte(nil), result...)
	current.FailureClass = append([]byte(nil), failure...)
	current.Revision++
	current.UpdatedAt = now
	return current, nil
}

func validExecutionTransition(from, to ExecutionStatus) bool {
	switch from {
	case ExecutionAdmitted:
		return to == ExecutionDispatching || to == ExecutionPaused || to == ExecutionReplanning || to == ExecutionFailed || to == ExecutionRecoveryRequired
	case ExecutionDispatching:
		return to == ExecutionRunning || to == ExecutionPaused || to == ExecutionFailed || to == ExecutionRecoveryRequired
	case ExecutionRunning:
		return to == ExecutionDispatching || to == ExecutionPaused || to == ExecutionRetryable || to == ExecutionSucceeded || to == ExecutionFailed || to == ExecutionBlocked || to == ExecutionReplanning || to == ExecutionRecoveryRequired
	case ExecutionPaused:
		return to == ExecutionRunning || to == ExecutionReplanning
	case ExecutionRetryable:
		return to == ExecutionDispatching || to == ExecutionPaused || to == ExecutionReplanning
	case ExecutionFailed:
		// Retry is an explicit product action. It reopens dispatch only after
		// the caller presents a new idempotency key and source causation.
		return to == ExecutionRetryable
	case ExecutionReplanning:
		return to == ExecutionDispatching || to == ExecutionPaused || to == ExecutionRunning
	case ExecutionRecoveryRequired:
		return to == ExecutionDispatching || to == ExecutionPaused || to == ExecutionFailed || to == ExecutionSucceeded
	default:
		return false
	}
}

func (r *executionRepository) UpdateExecutionPlan(ctx context.Context, id string, expectedRevision int64, mode, version, planDigest, capabilityDigest, projectionDigest, input string) (TaskExecution, error) {
	current, err := r.GetExecution(ctx, id)
	if err != nil {
		return TaskExecution{}, err
	}
	if current.Revision != expectedRevision {
		return TaskExecution{}, ErrRevisionConflict
	}
	if current.Status == ExecutionSucceeded || current.Status == ExecutionFailed || current.Status == ExecutionBlocked {
		return TaskExecution{}, fmt.Errorf("%w: %s -> replan", ErrInvalidTransition, current.Status)
	}
	res, err := r.store.db.ExecContext(ctx, r.store.bind(`UPDATE task_harness_executions SET mode=?,plan_version=?,plan_digest=?,capability_digest=?,projection_digest=?,input=?,revision=revision+1,updated_at=? WHERE id=? AND revision=?`), mode, version, planDigest, capabilityDigest, projectionDigest, input, time.Now().UTC().Format(time.RFC3339Nano), id, expectedRevision)
	if err != nil {
		return TaskExecution{}, err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		if _, getErr := r.GetExecution(ctx, id); errors.Is(getErr, ErrNotFound) {
			return TaskExecution{}, ErrNotFound
		}
		return TaskExecution{}, ErrRevisionConflict
	}
	return r.GetExecution(ctx, id)
}

// BindExecutionTaskRevision atomically moves an execution to the
// authoritative task revision selected by a replan. Both the execution
// revision and task revision are checked in one transaction so a stale
// planner cannot silently rebind a newer execution or task.
func (r *executionRepository) BindExecutionTaskRevision(ctx context.Context, id string, expectedExecutionRevision, taskRevision int64) (TaskExecution, error) {
	if id == "" || expectedExecutionRevision <= 0 || taskRevision <= 0 {
		return TaskExecution{}, fmt.Errorf("storage: invalid task revision binding")
	}
	tx, err := r.store.db.BeginTx(ctx, nil)
	if err != nil {
		return TaskExecution{}, err
	}
	defer func() { _ = tx.Rollback() }()

	executionQuery := `SELECT id,task_id,task_revision,status,revision,mode,plan_version,plan_digest,capability_digest,projection_digest,parent_run_id,correlation_id,idempotency_key,input,result,failure_class,created_at,updated_at FROM task_harness_executions WHERE id=?`
	if r.store.driver == "postgres" {
		executionQuery += ` FOR UPDATE`
	}
	var current TaskExecution
	if err := scanExecution(tx.QueryRowContext(ctx, r.store.bind(executionQuery), id), &current); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return TaskExecution{}, ErrNotFound
		}
		return TaskExecution{}, err
	}
	if current.Revision != expectedExecutionRevision {
		return TaskExecution{}, ErrRevisionConflict
	}

	taskQuery := `SELECT revision FROM tasks WHERE id=?`
	if r.store.driver == "postgres" {
		taskQuery += ` FOR UPDATE`
	}
	var currentTaskRevision int64
	if err := tx.QueryRowContext(ctx, r.store.bind(taskQuery), current.TaskID).Scan(&currentTaskRevision); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return TaskExecution{}, ErrRevisionConflict
		}
		return TaskExecution{}, err
	}
	if currentTaskRevision != taskRevision {
		return TaskExecution{}, ErrRevisionConflict
	}

	now := time.Now().UTC()
	result, err := tx.ExecContext(ctx, r.store.bind(`UPDATE task_harness_executions SET task_revision=?,revision=revision+1,updated_at=? WHERE id=? AND revision=? AND EXISTS (SELECT 1 FROM tasks WHERE id=? AND revision=?)`), taskRevision, now.Format(time.RFC3339Nano), id, expectedExecutionRevision, current.TaskID, taskRevision)
	if err != nil {
		return TaskExecution{}, err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return TaskExecution{}, ErrRevisionConflict
	}
	if err := tx.Commit(); err != nil {
		return TaskExecution{}, err
	}
	current.TaskRevision = taskRevision
	current.Revision++
	current.UpdatedAt = now
	return current, nil
}

func (r *executionRepository) AppendPlan(ctx context.Context, plan ExecutionPlanRecord) error {
	if plan.ExecutionID == "" || plan.Version == "" {
		return fmt.Errorf("storage: invalid plan")
	}
	var existing ExecutionPlanRecord
	var expanded int
	var created string
	err := r.store.db.QueryRowContext(ctx, r.store.bind(`SELECT execution_id,version,parent_version,mode,profile_digest,plan_digest,capability_digest,projection_digest,rationale,replan_reason,budget,write_scope,authority_expanded,created_at FROM task_harness_plans WHERE execution_id=? AND version=?`), plan.ExecutionID, plan.Version).Scan(&existing.ExecutionID, &existing.Version, &existing.ParentVersion, &existing.Mode, &existing.ProfileDigest, &existing.PlanDigest, &existing.CapabilityDigest, &existing.ProjectionDigest, &existing.Rationale, &existing.ReplanReason, &existing.Budget, &existing.WriteScope, &expanded, &created)
	if err == nil {
		if existing.PlanDigest == plan.PlanDigest && existing.CapabilityDigest == plan.CapabilityDigest {
			return nil
		}
		return ErrRevisionConflict
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	return insertPlan(ctx, r.store.db, r.store, plan)
}

func (r *executionRepository) Plans(ctx context.Context, executionID string) ([]ExecutionPlanRecord, error) {
	rows, err := r.store.db.QueryContext(ctx, r.store.bind(`SELECT execution_id,version,parent_version,mode,profile_digest,plan_digest,capability_digest,projection_digest,rationale,replan_reason,budget,write_scope,child_limit,per_child_budget,aggregation_limit,authority_expanded,completed_effect_keys,diagnostics,created_at FROM task_harness_plans WHERE execution_id=? ORDER BY created_at,version`), executionID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var plans []ExecutionPlanRecord
	for rows.Next() {
		var p ExecutionPlanRecord
		var expanded int
		var created string
		if err := rows.Scan(&p.ExecutionID, &p.Version, &p.ParentVersion, &p.Mode, &p.ProfileDigest, &p.PlanDigest, &p.CapabilityDigest, &p.ProjectionDigest, &p.Rationale, &p.ReplanReason, &p.Budget, &p.WriteScope, &p.ChildLimit, &p.PerChildBudget, &p.AggregationLimit, &expanded, &p.CompletedEffectKeys, &p.Diagnostics, &created); err != nil {
			return nil, err
		}
		p.AuthorityExpanded = expanded != 0
		p.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		plans = append(plans, p)
	}
	return plans, rows.Err()
}

func (r *executionRepository) SaveCheckpoint(ctx context.Context, checkpoint ExecutionCheckpoint) error {
	if checkpoint.ExecutionID == "" || checkpoint.PlanVersion == "" || checkpoint.PauseReason == nil {
		return fmt.Errorf("storage: invalid checkpoint")
	}
	if checkpoint.CompletedEffectKeys == nil {
		checkpoint.CompletedEffectKeys = []byte("[]")
	}
	if checkpoint.Diagnostics == nil {
		checkpoint.Diagnostics = []byte("[]")
	}
	now := time.Now().UTC()
	var current int64
	err := r.store.db.QueryRowContext(ctx, r.store.bind(`SELECT revision FROM task_harness_checkpoints WHERE execution_id=?`), checkpoint.ExecutionID).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) {
		checkpoint.Revision = 1
		_, err = r.store.db.ExecContext(ctx, r.store.bind(`INSERT INTO task_harness_checkpoints(execution_id,run_id,plan_version,plan_digest,capability_digest,projection_digest,current_step,pause_reason,completed_effect_keys,diagnostics,revision,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`), checkpoint.ExecutionID, checkpoint.RunID, checkpoint.PlanVersion, checkpoint.PlanDigest, checkpoint.CapabilityDigest, checkpoint.ProjectionDigest, checkpoint.CurrentStep, checkpoint.PauseReason, checkpoint.CompletedEffectKeys, checkpoint.Diagnostics, checkpoint.Revision, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
		return err
	}
	if err != nil {
		return err
	}
	if checkpoint.Revision != 0 && checkpoint.Revision != current {
		return ErrRevisionConflict
	}
	res, err := r.store.db.ExecContext(ctx, r.store.bind(`UPDATE task_harness_checkpoints SET run_id=?,plan_version=?,plan_digest=?,capability_digest=?,projection_digest=?,current_step=?,pause_reason=?,completed_effect_keys=?,diagnostics=?,revision=revision+1,updated_at=? WHERE execution_id=? AND revision=?`), checkpoint.RunID, checkpoint.PlanVersion, checkpoint.PlanDigest, checkpoint.CapabilityDigest, checkpoint.ProjectionDigest, checkpoint.CurrentStep, checkpoint.PauseReason, checkpoint.CompletedEffectKeys, checkpoint.Diagnostics, now.Format(time.RFC3339Nano), checkpoint.ExecutionID, current)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ErrRevisionConflict
	}
	return nil
}

func (r *executionRepository) SaveNativeCheckpointAndPause(ctx context.Context, checkpoint ExecutionCheckpoint, binding ExecutionAuthorityBinding, expectedExecutionRevision int64) (TaskExecution, error) {
	if checkpoint.ExecutionID == "" || checkpoint.ContractVersion != NativeCheckpointContract || checkpoint.NativeCheckpointID == "" || checkpoint.NativeCheckpointVersion == "" || checkpoint.NativeCheckpointDigest == "" || checkpoint.PlanVersion == "" || checkpoint.PauseReason == nil || binding.ExecutionID != checkpoint.ExecutionID || binding.ContractVersion != NativeCheckpointContract || binding.NativeCheckpointID != checkpoint.NativeCheckpointID || binding.NativeCheckpointVersion != checkpoint.NativeCheckpointVersion || binding.NativeCheckpointDigest != checkpoint.NativeCheckpointDigest || binding.PlanVersion != checkpoint.PlanVersion || binding.PlanDigest != checkpoint.PlanDigest || binding.CapabilityDigest != checkpoint.CapabilityDigest || binding.ProjectionDigest != checkpoint.ProjectionDigest || binding.WriteScope == "" || binding.EffectNamespace == "" || expectedExecutionRevision <= 0 {
		return TaskExecution{}, fmt.Errorf("storage: invalid native authority pause")
	}
	tx, err := r.store.db.BeginTx(ctx, nil)
	if err != nil {
		return TaskExecution{}, err
	}
	defer func() { _ = tx.Rollback() }()
	lockQuery := `SELECT id,task_id,task_revision,status,revision,mode,plan_version,plan_digest,capability_digest,projection_digest,parent_run_id,correlation_id,idempotency_key,input,result,failure_class,created_at,updated_at FROM task_harness_executions WHERE id=?`
	if r.store.driver == "postgres" {
		lockQuery += ` FOR UPDATE`
	}
	var current TaskExecution
	if err := scanExecution(tx.QueryRowContext(ctx, r.store.bind(lockQuery), checkpoint.ExecutionID), &current); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return TaskExecution{}, ErrNotFound
		}
		return TaskExecution{}, err
	}
	if current.Revision != expectedExecutionRevision {
		return TaskExecution{}, ErrRevisionConflict
	}
	if current.Status != ExecutionPaused && !validExecutionTransition(current.Status, ExecutionPaused) {
		return TaskExecution{}, fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, current.Status, ExecutionPaused)
	}
	if checkpoint.CompletedEffectKeys == nil {
		checkpoint.CompletedEffectKeys = []byte("[]")
	}
	if checkpoint.Diagnostics == nil {
		checkpoint.Diagnostics = []byte("[]")
	}
	now := time.Now().UTC()
	var checkpointRevision int64
	checkpointErr := tx.QueryRowContext(ctx, r.store.bind(`SELECT revision FROM task_harness_checkpoints WHERE execution_id=?`), checkpoint.ExecutionID).Scan(&checkpointRevision)
	if errors.Is(checkpointErr, sql.ErrNoRows) {
		checkpoint.Revision = 1
		_, err = tx.ExecContext(ctx, r.store.bind(`INSERT INTO task_harness_checkpoints(execution_id,run_id,plan_version,plan_digest,capability_digest,projection_digest,current_step,pause_reason,completed_effect_keys,diagnostics,checkpoint_contract_version,task_revision,write_scope,effect_namespace,native_checkpoint_id,native_checkpoint_version,native_checkpoint_digest,source_kind,source_namespace,source_id,source_cursor,revision,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`), checkpoint.ExecutionID, checkpoint.RunID, checkpoint.PlanVersion, checkpoint.PlanDigest, checkpoint.CapabilityDigest, checkpoint.ProjectionDigest, checkpoint.CurrentStep, checkpoint.PauseReason, checkpoint.CompletedEffectKeys, checkpoint.Diagnostics, checkpoint.ContractVersion, checkpoint.TaskRevision, checkpoint.WriteScope, checkpoint.EffectNamespace, checkpoint.NativeCheckpointID, checkpoint.NativeCheckpointVersion, checkpoint.NativeCheckpointDigest, checkpoint.SourceKind, checkpoint.SourceNamespace, checkpoint.SourceID, checkpoint.SourceCursor, checkpoint.Revision, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	} else if checkpointErr != nil {
		return TaskExecution{}, checkpointErr
	} else {
		if checkpoint.Revision != 0 && checkpoint.Revision != checkpointRevision {
			return TaskExecution{}, ErrRevisionConflict
		}
		_, err = tx.ExecContext(ctx, r.store.bind(`UPDATE task_harness_checkpoints SET run_id=?,plan_version=?,plan_digest=?,capability_digest=?,projection_digest=?,current_step=?,pause_reason=?,completed_effect_keys=?,diagnostics=?,checkpoint_contract_version=?,task_revision=?,write_scope=?,effect_namespace=?,native_checkpoint_id=?,native_checkpoint_version=?,native_checkpoint_digest=?,source_kind=?,source_namespace=?,source_id=?,source_cursor=?,revision=revision+1,updated_at=? WHERE execution_id=? AND revision=?`), checkpoint.RunID, checkpoint.PlanVersion, checkpoint.PlanDigest, checkpoint.CapabilityDigest, checkpoint.ProjectionDigest, checkpoint.CurrentStep, checkpoint.PauseReason, checkpoint.CompletedEffectKeys, checkpoint.Diagnostics, checkpoint.ContractVersion, checkpoint.TaskRevision, checkpoint.WriteScope, checkpoint.EffectNamespace, checkpoint.NativeCheckpointID, checkpoint.NativeCheckpointVersion, checkpoint.NativeCheckpointDigest, checkpoint.SourceKind, checkpoint.SourceNamespace, checkpoint.SourceID, checkpoint.SourceCursor, now.Format(time.RFC3339Nano), checkpoint.ExecutionID, checkpointRevision)
	}
	if err != nil {
		return TaskExecution{}, err
	}
	if binding.CompletedEffectKeys == nil {
		binding.CompletedEffectKeys = []byte("[]")
	}
	if binding.CreatedAt.IsZero() {
		binding.CreatedAt = now
	}
	binding.UpdatedAt = now
	_, err = tx.ExecContext(ctx, r.store.bind(`INSERT INTO execution_authority_bindings(execution_id,contract_version,task_revision,schedule_revision,plan_version,plan_digest,capability_digest,projection_digest,write_scope,effect_namespace,completed_effect_keys,native_checkpoint_id,native_checkpoint_version,native_checkpoint_digest,source_kind,source_namespace,source_id,source_cursor,context_snapshot_id,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(execution_id) DO UPDATE SET contract_version=excluded.contract_version,task_revision=excluded.task_revision,schedule_revision=excluded.schedule_revision,plan_version=excluded.plan_version,plan_digest=excluded.plan_digest,capability_digest=excluded.capability_digest,projection_digest=excluded.projection_digest,write_scope=excluded.write_scope,effect_namespace=excluded.effect_namespace,completed_effect_keys=excluded.completed_effect_keys,native_checkpoint_id=excluded.native_checkpoint_id,native_checkpoint_version=excluded.native_checkpoint_version,native_checkpoint_digest=excluded.native_checkpoint_digest,source_kind=excluded.source_kind,source_namespace=excluded.source_namespace,source_id=excluded.source_id,source_cursor=excluded.source_cursor,context_snapshot_id=excluded.context_snapshot_id,updated_at=excluded.updated_at`), binding.ExecutionID, binding.ContractVersion, binding.TaskRevision, binding.ScheduleRevision, binding.PlanVersion, binding.PlanDigest, binding.CapabilityDigest, binding.ProjectionDigest, binding.WriteScope, binding.EffectNamespace, binding.CompletedEffectKeys, binding.NativeCheckpointID, binding.NativeCheckpointVersion, binding.NativeCheckpointDigest, binding.SourceKind, binding.SourceNamespace, binding.SourceID, binding.SourceCursor, binding.ContextSnapshotID, binding.CreatedAt.Format(time.RFC3339Nano), binding.UpdatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return TaskExecution{}, err
	}
	_, err = tx.ExecContext(ctx, r.store.bind(`UPDATE task_harness_executions SET status=?,revision=revision+1,updated_at=? WHERE id=? AND revision=?`), ExecutionPaused, now.Format(time.RFC3339Nano), checkpoint.ExecutionID, expectedExecutionRevision)
	if err != nil {
		return TaskExecution{}, err
	}
	if err := r.store.fail("native_checkpoint_before_commit"); err != nil {
		return TaskExecution{}, err
	}
	if err := tx.Commit(); err != nil {
		return TaskExecution{}, err
	}
	current.Status = ExecutionPaused
	current.Revision++
	current.UpdatedAt = now
	return current, nil
}

func (r *executionRepository) GetCheckpoint(ctx context.Context, executionID string) (ExecutionCheckpoint, error) {
	var c ExecutionCheckpoint
	var created, updated string
	err := r.store.db.QueryRowContext(ctx, r.store.bind(`SELECT execution_id,run_id,plan_version,plan_digest,capability_digest,projection_digest,current_step,pause_reason,completed_effect_keys,diagnostics,checkpoint_contract_version,task_revision,write_scope,effect_namespace,native_checkpoint_id,native_checkpoint_version,native_checkpoint_digest,source_kind,source_namespace,source_id,source_cursor,revision,created_at,updated_at FROM task_harness_checkpoints WHERE execution_id=?`), executionID).Scan(&c.ExecutionID, &c.RunID, &c.PlanVersion, &c.PlanDigest, &c.CapabilityDigest, &c.ProjectionDigest, &c.CurrentStep, &c.PauseReason, &c.CompletedEffectKeys, &c.Diagnostics, &c.ContractVersion, &c.TaskRevision, &c.WriteScope, &c.EffectNamespace, &c.NativeCheckpointID, &c.NativeCheckpointVersion, &c.NativeCheckpointDigest, &c.SourceKind, &c.SourceNamespace, &c.SourceID, &c.SourceCursor, &c.Revision, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return ExecutionCheckpoint{}, ErrNotFound
	}
	c.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	c.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return c, err
}

func (r *executionRepository) RecordReplan(ctx context.Context, replan ExecutionReplan) error {
	if replan.ExecutionID == "" || replan.NewPlanVersion == "" || replan.PreviousPlanVersion == "" {
		return fmt.Errorf("storage: invalid replan")
	}
	if replan.CreatedAt.IsZero() {
		replan.CreatedAt = time.Now().UTC()
	}
	_, err := r.store.db.ExecContext(ctx, r.store.bind(`INSERT INTO task_harness_replans(id,execution_id,previous_plan_version,new_plan_version,reason,previous_profile_digest,new_profile_digest,authority_expanded,created_at) VALUES(?,?,?,?,?,?,?,?,?)`), uuid.NewString(), replan.ExecutionID, replan.PreviousPlanVersion, replan.NewPlanVersion, replan.Reason, replan.PreviousProfileDigest, replan.NewProfileDigest, boolInt(replan.AuthorityExpanded), replan.CreatedAt.Format(time.RFC3339Nano))
	return err
}

func (r *executionRepository) Replans(ctx context.Context, executionID string) ([]ExecutionReplan, error) {
	rows, err := r.store.db.QueryContext(ctx, r.store.bind(`SELECT execution_id,previous_plan_version,new_plan_version,reason,previous_profile_digest,new_profile_digest,authority_expanded,created_at FROM task_harness_replans WHERE execution_id=? ORDER BY created_at`), executionID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var result []ExecutionReplan
	for rows.Next() {
		var item ExecutionReplan
		var expanded int
		var created string
		if err := rows.Scan(&item.ExecutionID, &item.PreviousPlanVersion, &item.NewPlanVersion, &item.Reason, &item.PreviousProfileDigest, &item.NewProfileDigest, &expanded, &created); err != nil {
			return nil, err
		}
		item.AuthorityExpanded = expanded != 0
		item.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *executionRepository) RecordEvidence(ctx context.Context, evidence ExecutionEvidenceRecord) (ExecutionEvidenceRecord, error) {
	if evidence.ExecutionID == "" || evidence.EffectKey == "" {
		return ExecutionEvidenceRecord{}, fmt.Errorf("storage: invalid evidence")
	}
	var existing ExecutionEvidenceRecord
	if err := scanEvidence(r.store.db.QueryRowContext(ctx, r.store.bind(`SELECT id,execution_id,effect_key,parent_run_id,child_run_id,plan_version,capability_digest,projection_digest,status,failure_class,diagnostic,result,created_at,native_source_kind,native_source_namespace,native_source_id,native_event_id,native_version,native_sequence,observation_position,evidence_contract_version,observation_scope FROM task_harness_evidence WHERE execution_id=? AND effect_key=?`), evidence.ExecutionID, evidence.EffectKey), &existing); err == nil {
		return existing, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return ExecutionEvidenceRecord{}, err
	}
	if evidence.ID == "" {
		evidence.ID = uuid.NewString()
	}
	if evidence.Status == nil {
		evidence.Status = []byte{}
	}
	if evidence.FailureClass == nil {
		evidence.FailureClass = []byte{}
	}
	if evidence.Diagnostic == nil {
		evidence.Diagnostic = []byte{}
	}
	if evidence.Result == nil {
		evidence.Result = []byte{}
	}
	if evidence.CreatedAt.IsZero() {
		evidence.CreatedAt = time.Now().UTC()
	}
	_, err := r.store.db.ExecContext(ctx, r.store.bind(`INSERT INTO task_harness_evidence(id,execution_id,effect_key,parent_run_id,child_run_id,plan_version,capability_digest,projection_digest,status,failure_class,diagnostic,result,created_at,native_source_kind,native_source_namespace,native_source_id,native_event_id,native_version,native_sequence,observation_position,evidence_contract_version,observation_scope) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`), evidence.ID, evidence.ExecutionID, evidence.EffectKey, evidence.ParentRunID, evidence.ChildRunID, evidence.PlanVersion, evidence.CapabilityDigest, evidence.ProjectionDigest, evidence.Status, evidence.FailureClass, evidence.Diagnostic, evidence.Result, evidence.CreatedAt.Format(time.RFC3339Nano), evidence.NativeSourceKind, evidence.NativeSourceNamespace, evidence.NativeSourceID, evidence.NativeEventID, evidence.NativeVersion, evidence.NativeSequence, evidence.ObservationPosition, evidence.ContractVersion, evidence.ObservationScope)
	if err != nil {
		if replay, replayErr := r.RecordEvidence(ctx, evidence); replayErr == nil {
			return replay, nil
		}
	}
	return evidence, err
}

func (r *executionRepository) ProjectNativeEvidence(ctx context.Context, evidence ExecutionEvidenceRecord, cursor ExecutionSourceCursor) (ExecutionEvidenceRecord, error) {
	if evidence.ExecutionID == "" || evidence.ObservationScope == "" || evidence.ContractVersion != NativeEvidenceContractV1 || evidence.NativeSourceKind == "" || evidence.NativeSourceNamespace == "" || evidence.NativeSourceID == "" || evidence.NativeEventID == "" || evidence.NativeVersion == "" || cursor.ExecutionID != evidence.ExecutionID || cursor.SourceKind != evidence.NativeSourceKind || cursor.SourceNamespace != evidence.NativeSourceNamespace || cursor.SourceID != evidence.NativeSourceID || cursor.ContractVersion != NativeEvidenceContractV1 || cursor.CursorVersion == "" || cursor.CursorValue == "" {
		return ExecutionEvidenceRecord{}, fmt.Errorf("storage: invalid native evidence projection")
	}
	tx, err := r.store.db.BeginTx(ctx, nil)
	if err != nil {
		return ExecutionEvidenceRecord{}, err
	}
	defer func() { _ = tx.Rollback() }()
	lockQuery := `SELECT id FROM task_harness_executions WHERE id=?`
	if r.store.driver == "postgres" {
		lockQuery += ` FOR UPDATE`
	}
	var executionID string
	if err := tx.QueryRowContext(ctx, r.store.bind(lockQuery), evidence.ExecutionID).Scan(&executionID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ExecutionEvidenceRecord{}, ErrNotFound
		}
		return ExecutionEvidenceRecord{}, err
	}
	identityQuery := `SELECT id,execution_id,effect_key,parent_run_id,child_run_id,plan_version,capability_digest,projection_digest,status,failure_class,diagnostic,result,created_at,native_source_kind,native_source_namespace,native_source_id,native_event_id,native_version,native_sequence,observation_position,evidence_contract_version,observation_scope FROM task_harness_evidence WHERE execution_id=? AND native_source_kind=? AND native_source_namespace=? AND native_source_id=? AND native_event_id=?`
	var existing ExecutionEvidenceRecord
	var created string
	err = tx.QueryRowContext(ctx, r.store.bind(identityQuery), evidence.ExecutionID, evidence.NativeSourceKind, evidence.NativeSourceNamespace, evidence.NativeSourceID, evidence.NativeEventID).Scan(&existing.ID, &existing.ExecutionID, &existing.EffectKey, &existing.ParentRunID, &existing.ChildRunID, &existing.PlanVersion, &existing.CapabilityDigest, &existing.ProjectionDigest, &existing.Status, &existing.FailureClass, &existing.Diagnostic, &existing.Result, &created, &existing.NativeSourceKind, &existing.NativeSourceNamespace, &existing.NativeSourceID, &existing.NativeEventID, &existing.NativeVersion, &existing.NativeSequence, &existing.ObservationPosition, &existing.ContractVersion, &existing.ObservationScope)
	if err == nil {
		existing.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		if !equivalentNativeEvidence(existing, evidence) {
			return ExecutionEvidenceRecord{}, ErrEvidenceConflict
		}
		return existing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return ExecutionEvidenceRecord{}, err
	}
	var priorSequence sql.NullInt64
	if err := tx.QueryRowContext(ctx, r.store.bind(`SELECT native_sequence FROM task_harness_evidence WHERE execution_id=? AND native_source_kind=? AND native_source_namespace=? AND native_source_id=? AND native_sequence IS NOT NULL ORDER BY native_sequence DESC LIMIT 1`), evidence.ExecutionID, evidence.NativeSourceKind, evidence.NativeSourceNamespace, evidence.NativeSourceID).Scan(&priorSequence); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return ExecutionEvidenceRecord{}, err
	}
	if priorSequence.Valid && evidence.NativeSequence != nil && *evidence.NativeSequence < priorSequence.Int64 {
		return ExecutionEvidenceRecord{}, ErrSourceOrderConflict
	}
	var position int64
	if err := tx.QueryRowContext(ctx, r.store.bind(`SELECT COALESCE(MAX(observation_position),0)+1 FROM task_harness_evidence WHERE execution_id=?`), evidence.ExecutionID).Scan(&position); err != nil {
		return ExecutionEvidenceRecord{}, err
	}
	if evidence.ID == "" {
		evidence.ID = uuid.NewString()
	}
	if evidence.EffectKey == "" {
		evidence.EffectKey = "native:" + evidence.NativeSourceKind + ":" + evidence.NativeSourceNamespace + ":" + evidence.NativeSourceID + ":" + evidence.NativeEventID
	}
	if evidence.Status == nil {
		evidence.Status = []byte{}
	}
	if evidence.FailureClass == nil {
		evidence.FailureClass = []byte{}
	}
	if evidence.Diagnostic == nil {
		evidence.Diagnostic = []byte{}
	}
	if evidence.Result == nil {
		evidence.Result = []byte{}
	}
	if evidence.CreatedAt.IsZero() {
		evidence.CreatedAt = time.Now().UTC()
	}
	evidence.ObservationPosition = position
	insert := `INSERT INTO task_harness_evidence(id,execution_id,effect_key,parent_run_id,child_run_id,plan_version,capability_digest,projection_digest,status,failure_class,diagnostic,result,created_at,native_source_kind,native_source_namespace,native_source_id,native_event_id,native_sequence,native_version,observation_position,evidence_contract_version,observation_scope) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`
	if _, err := tx.ExecContext(ctx, r.store.bind(insert), evidence.ID, evidence.ExecutionID, evidence.EffectKey, evidence.ParentRunID, evidence.ChildRunID, evidence.PlanVersion, evidence.CapabilityDigest, evidence.ProjectionDigest, evidence.Status, evidence.FailureClass, evidence.Diagnostic, evidence.Result, evidence.CreatedAt.Format(time.RFC3339Nano), evidence.NativeSourceKind, evidence.NativeSourceNamespace, evidence.NativeSourceID, evidence.NativeEventID, evidence.NativeSequence, evidence.NativeVersion, evidence.ObservationPosition, evidence.ContractVersion, evidence.ObservationScope); err != nil {
		return ExecutionEvidenceRecord{}, err
	}
	if cursor.UpdatedAt.IsZero() {
		cursor.UpdatedAt = time.Now().UTC()
	}
	var priorCursorSequence sql.NullInt64
	if err := tx.QueryRowContext(ctx, r.store.bind(`SELECT source_sequence FROM execution_source_cursors WHERE execution_id=? AND source_kind=? AND source_namespace=? AND source_id=?`), cursor.ExecutionID, cursor.SourceKind, cursor.SourceNamespace, cursor.SourceID).Scan(&priorCursorSequence); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return ExecutionEvidenceRecord{}, err
	}
	if priorCursorSequence.Valid && cursor.SourceSequence < priorCursorSequence.Int64 {
		return ExecutionEvidenceRecord{}, ErrSourceOrderConflict
	}
	upsert := `INSERT INTO execution_source_cursors(execution_id,source_kind,source_namespace,source_id,cursor_version,cursor_value,retention_floor,source_sequence,observation_position,contract_version,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(execution_id,source_kind,source_namespace,source_id) DO UPDATE SET cursor_version=excluded.cursor_version,cursor_value=excluded.cursor_value,retention_floor=excluded.retention_floor,source_sequence=excluded.source_sequence,observation_position=excluded.observation_position,contract_version=excluded.contract_version,updated_at=excluded.updated_at`
	if _, err := tx.ExecContext(ctx, r.store.bind(upsert), cursor.ExecutionID, cursor.SourceKind, cursor.SourceNamespace, cursor.SourceID, cursor.CursorVersion, cursor.CursorValue, cursor.RetentionFloor, cursor.SourceSequence, evidence.ObservationPosition, cursor.ContractVersion, cursor.UpdatedAt.Format(time.RFC3339Nano)); err != nil {
		return ExecutionEvidenceRecord{}, err
	}
	if err := r.store.fail("native_evidence_before_commit"); err != nil {
		return ExecutionEvidenceRecord{}, err
	}
	if err := tx.Commit(); err != nil {
		return ExecutionEvidenceRecord{}, err
	}
	if err := r.store.fail("native_evidence_after_commit"); err != nil {
		return ExecutionEvidenceRecord{}, err
	}
	return evidence, nil
}

func equivalentNativeEvidence(a, b ExecutionEvidenceRecord) bool {
	return a.ExecutionID == b.ExecutionID && a.ObservationScope == b.ObservationScope && a.ParentRunID == b.ParentRunID && a.ChildRunID == b.ChildRunID && a.PlanVersion == b.PlanVersion && a.CapabilityDigest == b.CapabilityDigest && a.ProjectionDigest == b.ProjectionDigest && a.ContractVersion == b.ContractVersion && a.NativeSourceKind == b.NativeSourceKind && a.NativeSourceNamespace == b.NativeSourceNamespace && a.NativeSourceID == b.NativeSourceID && a.NativeEventID == b.NativeEventID && a.NativeVersion == b.NativeVersion && equalInt64(a.NativeSequence, b.NativeSequence) && bytes.Equal(a.Status, b.Status) && bytes.Equal(a.FailureClass, b.FailureClass) && bytes.Equal(a.Diagnostic, b.Diagnostic) && bytes.Equal(a.Result, b.Result)
}

func equalInt64(a, b *int64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func (r *executionRepository) Evidence(ctx context.Context, executionID string) ([]ExecutionEvidenceRecord, error) {
	rows, err := r.store.db.QueryContext(ctx, r.store.bind(`SELECT id,execution_id,effect_key,parent_run_id,child_run_id,plan_version,capability_digest,projection_digest,status,failure_class,diagnostic,result,created_at,native_source_kind,native_source_namespace,native_source_id,native_event_id,native_version,native_sequence,observation_position,evidence_contract_version,observation_scope FROM task_harness_evidence WHERE execution_id=? ORDER BY observation_position,created_at,id`), executionID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var result []ExecutionEvidenceRecord
	for rows.Next() {
		var item ExecutionEvidenceRecord
		if err := scanEvidence(rows, &item); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *executionRepository) ReplayEvidence(ctx context.Context, request ExecutionEvidenceReplayRequest) (ExecutionEvidenceReplayPage, error) {
	cursor := request.Cursor
	if cursor.ContractVersion != ExecutionEvidenceReplayContractV1 || cursor.ExecutionID == "" || cursor.Scope == "" || cursor.Position < 0 || request.Limit < 1 || request.Limit > 100 {
		return ExecutionEvidenceReplayPage{}, ErrInvalidReplayCursor
	}
	var executionID string
	if err := r.store.db.QueryRowContext(ctx, r.store.bind(`SELECT id FROM task_harness_executions WHERE id=?`), cursor.ExecutionID).Scan(&executionID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ExecutionEvidenceReplayPage{}, ErrNotFound
		}
		return ExecutionEvidenceReplayPage{}, err
	}
	var scopedCount, otherScopeCount int
	if err := r.store.db.QueryRowContext(ctx, r.store.bind(`SELECT COUNT(*), COALESCE(SUM(CASE WHEN observation_scope<>? AND observation_scope<>'' THEN 1 ELSE 0 END),0) FROM task_harness_evidence WHERE execution_id=? AND observation_scope=?`), cursor.Scope, cursor.ExecutionID, cursor.Scope).Scan(&scopedCount, &otherScopeCount); err != nil {
		return ExecutionEvidenceReplayPage{}, err
	}
	if scopedCount == 0 {
		var evidenceCount int
		if err := r.store.db.QueryRowContext(ctx, r.store.bind(`SELECT COUNT(*) FROM task_harness_evidence WHERE execution_id=? AND observation_scope<>''`), cursor.ExecutionID).Scan(&evidenceCount); err != nil {
			return ExecutionEvidenceReplayPage{}, err
		}
		if evidenceCount > 0 {
			return ExecutionEvidenceReplayPage{}, ErrReplayScope
		}
		return ExecutionEvidenceReplayPage{Evidence: []ExecutionEvidenceRecord{}}, nil
	}
	var floor, ceiling int64
	if err := r.store.db.QueryRowContext(ctx, r.store.bind(`SELECT MIN(observation_position), MAX(observation_position) FROM task_harness_evidence WHERE execution_id=? AND observation_scope=?`), cursor.ExecutionID, cursor.Scope).Scan(&floor, &ceiling); err != nil {
		return ExecutionEvidenceReplayPage{}, err
	}
	if cursor.Position < floor-1 {
		return ExecutionEvidenceReplayPage{}, ErrReplayExpired
	}
	if cursor.Position > ceiling {
		return ExecutionEvidenceReplayPage{}, ErrReplayGap
	}
	rows, err := r.store.db.QueryContext(ctx, r.store.bind(`SELECT id,execution_id,effect_key,parent_run_id,child_run_id,plan_version,capability_digest,projection_digest,status,failure_class,diagnostic,result,created_at,native_source_kind,native_source_namespace,native_source_id,native_event_id,native_version,native_sequence,observation_position,evidence_contract_version,observation_scope FROM task_harness_evidence WHERE execution_id=? AND observation_scope=? AND observation_position>? ORDER BY observation_position LIMIT ?`), cursor.ExecutionID, cursor.Scope, cursor.Position, request.Limit+1)
	if err != nil {
		return ExecutionEvidenceReplayPage{}, err
	}
	defer func() { _ = rows.Close() }()
	page := ExecutionEvidenceReplayPage{Evidence: make([]ExecutionEvidenceRecord, 0, request.Limit), RetentionFloor: floor}
	for rows.Next() {
		var evidence ExecutionEvidenceRecord
		if err := scanEvidence(rows, &evidence); err != nil {
			return ExecutionEvidenceReplayPage{}, err
		}
		if len(page.Evidence) == request.Limit {
			next := cursor
			next.Position = page.Evidence[len(page.Evidence)-1].ObservationPosition
			page.NextCursor = &next
			break
		}
		page.Evidence = append(page.Evidence, evidence)
	}
	if err := rows.Err(); err != nil {
		return ExecutionEvidenceReplayPage{}, err
	}
	return page, nil
}

type scanner interface{ Scan(...any) error }

func scanExecution(row scanner, e *TaskExecution) error {
	var created, updated string
	err := row.Scan(&e.ID, &e.TaskID, &e.TaskRevision, &e.Status, &e.Revision, &e.Mode, &e.PlanVersion, &e.PlanDigest, &e.CapabilityDigest, &e.ProjectionDigest, &e.ParentRunID, &e.CorrelationID, &e.IdempotencyKey, &e.Input, &e.Result, &e.FailureClass, &created, &updated)
	if err == nil {
		e.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		e.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	}
	return err
}

func scanEvidence(row scanner, e *ExecutionEvidenceRecord) error {
	var created string
	err := row.Scan(&e.ID, &e.ExecutionID, &e.EffectKey, &e.ParentRunID, &e.ChildRunID, &e.PlanVersion, &e.CapabilityDigest, &e.ProjectionDigest, &e.Status, &e.FailureClass, &e.Diagnostic, &e.Result, &created, &e.NativeSourceKind, &e.NativeSourceNamespace, &e.NativeSourceID, &e.NativeEventID, &e.NativeVersion, &e.NativeSequence, &e.ObservationPosition, &e.ContractVersion, &e.ObservationScope)
	if err == nil {
		e.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	}
	return err
}

func insertPlan(ctx context.Context, tx interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, store *Store, plan ExecutionPlanRecord) error {
	if plan.CreatedAt.IsZero() {
		plan.CreatedAt = time.Now().UTC()
	}
	if plan.CompletedEffectKeys == nil {
		plan.CompletedEffectKeys = []byte("[]")
	}
	if plan.Diagnostics == nil {
		plan.Diagnostics = []byte("[]")
	}
	_, err := tx.ExecContext(ctx, store.bind(`INSERT INTO task_harness_plans(execution_id,version,parent_version,mode,profile_digest,plan_digest,capability_digest,projection_digest,rationale,replan_reason,budget,write_scope,child_limit,per_child_budget,aggregation_limit,authority_expanded,completed_effect_keys,diagnostics,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`), plan.ExecutionID, plan.Version, plan.ParentVersion, plan.Mode, plan.ProfileDigest, plan.PlanDigest, plan.CapabilityDigest, plan.ProjectionDigest, plan.Rationale, plan.ReplanReason, plan.Budget, plan.WriteScope, plan.ChildLimit, plan.PerChildBudget, plan.AggregationLimit, boolInt(plan.AuthorityExpanded), plan.CompletedEffectKeys, plan.Diagnostics, plan.CreatedAt.Format(time.RFC3339Nano))
	return err
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
