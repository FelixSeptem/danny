package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type executionRepository struct{ store *Store }

func (r *executionRepository) Admit(ctx context.Context, admission ExecutionAdmission) (TaskExecution, error) {
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

func (r *executionRepository) GetCheckpoint(ctx context.Context, executionID string) (ExecutionCheckpoint, error) {
	var c ExecutionCheckpoint
	var created, updated string
	err := r.store.db.QueryRowContext(ctx, r.store.bind(`SELECT execution_id,run_id,plan_version,plan_digest,capability_digest,projection_digest,current_step,pause_reason,completed_effect_keys,diagnostics,revision,created_at,updated_at FROM task_harness_checkpoints WHERE execution_id=?`), executionID).Scan(&c.ExecutionID, &c.RunID, &c.PlanVersion, &c.PlanDigest, &c.CapabilityDigest, &c.ProjectionDigest, &c.CurrentStep, &c.PauseReason, &c.CompletedEffectKeys, &c.Diagnostics, &c.Revision, &created, &updated)
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
	if err := scanEvidence(r.store.db.QueryRowContext(ctx, r.store.bind(`SELECT id,execution_id,effect_key,parent_run_id,child_run_id,plan_version,capability_digest,projection_digest,status,failure_class,diagnostic,result,created_at FROM task_harness_evidence WHERE execution_id=? AND effect_key=?`), evidence.ExecutionID, evidence.EffectKey), &existing); err == nil {
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
	_, err := r.store.db.ExecContext(ctx, r.store.bind(`INSERT INTO task_harness_evidence(id,execution_id,effect_key,parent_run_id,child_run_id,plan_version,capability_digest,projection_digest,status,failure_class,diagnostic,result,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`), evidence.ID, evidence.ExecutionID, evidence.EffectKey, evidence.ParentRunID, evidence.ChildRunID, evidence.PlanVersion, evidence.CapabilityDigest, evidence.ProjectionDigest, evidence.Status, evidence.FailureClass, evidence.Diagnostic, evidence.Result, evidence.CreatedAt.Format(time.RFC3339Nano))
	if err != nil {
		if replay, replayErr := r.RecordEvidence(ctx, evidence); replayErr == nil {
			return replay, nil
		}
	}
	return evidence, err
}

func (r *executionRepository) Evidence(ctx context.Context, executionID string) ([]ExecutionEvidenceRecord, error) {
	rows, err := r.store.db.QueryContext(ctx, r.store.bind(`SELECT id,execution_id,effect_key,parent_run_id,child_run_id,plan_version,capability_digest,projection_digest,status,failure_class,diagnostic,result,created_at FROM task_harness_evidence WHERE execution_id=? ORDER BY created_at,id`), executionID)
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
	err := row.Scan(&e.ID, &e.ExecutionID, &e.EffectKey, &e.ParentRunID, &e.ChildRunID, &e.PlanVersion, &e.CapabilityDigest, &e.ProjectionDigest, &e.Status, &e.FailureClass, &e.Diagnostic, &e.Result, &created)
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
