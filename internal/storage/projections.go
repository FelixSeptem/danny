package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ProjectionListLimit bounds every workbench list query. Projections are
// point-in-time reads of committed rows; live execution state comes from the
// Milestone 9 AG-UI stream, not from these bounded lists.
const ProjectionListLimit = 200

// The projection types below are explicit Danny-owned field allowlists. No
// projection carries raw payload bytes, Baymax native identity, a Stele
// envelope, or an internal event structure. JSON names are stable wire
// contracts consumed by the workbench.
type TaskProjection struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Status      string `json:"status"`
	Revision    int64  `json:"revision"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type ScheduleProjection struct {
	ID            string `json:"id"`
	Cron          string `json:"cron"`
	Timezone      string `json:"timezone"`
	ActionType    string `json:"action_type"`
	Instruction   string `json:"instruction"`
	Enabled       bool   `json:"enabled"`
	RetryLimit    int    `json:"retry_limit"`
	MisfirePolicy string `json:"misfire_policy"`
	BackfillLimit int    `json:"backfill_limit"`
	Revision      int64  `json:"revision"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

type OccurrenceProjection struct {
	ID            string `json:"id"`
	ScheduleID    string `json:"schedule_id"`
	OccurrenceKey string `json:"occurrence_key"`
	LocalTime     string `json:"local_time"`
	ResolvedAt    string `json:"resolved_at"`
	Timezone      string `json:"timezone"`
	TzdataVersion string `json:"tzdata_version"`
	DSTDecision   string `json:"dst_decision"`
	Status        string `json:"status"`
	AttemptCount  int    `json:"attempt_count"`
	ExecutionID   string `json:"execution_id"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

type DeliveryProjection struct {
	ID            string `json:"id"`
	ExecutionID   string `json:"execution_id"`
	OccurrenceID  string `json:"occurrence_id"`
	Status        string `json:"status"`
	AttemptCount  int64  `json:"attempt_count"`
	NextAttemptAt string `json:"next_attempt_at"`
	LastError     string `json:"last_error"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

type NotificationProjection struct {
	ID          string `json:"id"`
	ExecutionID string `json:"execution_id"`
	Channel     string `json:"channel"`
	Status      string `json:"status"`
	Revision    int64  `json:"revision"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type GenerationProjection struct {
	ID           int64  `json:"id"`
	Status       string `json:"status"`
	CommitMarker string `json:"commit_marker"`
	CreatedAt    string `json:"created_at"`
	CommittedAt  string `json:"committed_at"`
}

type ArtifactProjection struct {
	ID           string `json:"id"`
	GenerationID int64  `json:"generation_id"`
	Kind         string `json:"kind"`
	ContentHash  string `json:"content_hash"`
	CreatedAt    string `json:"created_at"`
}

type SourceProjection struct {
	ID              string `json:"id"`
	Path            string `json:"path"`
	ContentHash     string `json:"content_hash"`
	Format          string `json:"format"`
	ParserVersion   string `json:"parser_version"`
	ProviderMode    string `json:"provider_mode"`
	UntrustedSource bool   `json:"untrusted_source"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
}

type ExecutionProjection struct {
	ID            string `json:"id"`
	TaskID        string `json:"task_id"`
	Status        string `json:"status"`
	Revision      int64  `json:"revision"`
	Mode          string `json:"mode"`
	PlanVersion   string `json:"plan_version"`
	ParentRunID   string `json:"parent_run_id"`
	CorrelationID string `json:"correlation_id"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

type PlanVersionProjection struct {
	Version           string `json:"version"`
	ParentVersion     string `json:"parent_version"`
	Mode              string `json:"mode"`
	Rationale         string `json:"rationale"`
	ReplanReason      string `json:"replan_reason"`
	AuthorityExpanded bool   `json:"authority_expanded"`
	CreatedAt         string `json:"created_at"`
}

type CheckpointProjection struct {
	CurrentStep  string `json:"current_step"`
	PauseReason  string `json:"pause_reason"`
	PlanVersion  string `json:"plan_version"`
	TaskRevision int64  `json:"task_revision"`
}

type ContextProjectionRef struct {
	Digest          string `json:"digest"`
	SourceDigest    string `json:"source_digest"`
	ContinuityScope string `json:"continuity_scope"`
	CreatedAt       string `json:"created_at"`
}

// RunInspectorProjection assembles only Danny-owned summary fields. Native
// checkpoint IDs, native event IDs, effect keys, raw results, and Stele
// envelopes are intentionally absent.
type RunInspectorProjection struct {
	Execution          ExecutionProjection     `json:"execution"`
	Plans              []PlanVersionProjection `json:"plans"`
	Checkpoint         *CheckpointProjection   `json:"checkpoint,omitempty"`
	ContextProjections []ContextProjectionRef  `json:"context_projections"`
	EvidenceCount      int                     `json:"evidence_count"`
	EventCount         int                     `json:"event_count"`
}

type IndexStatusProjection struct {
	GenerationID     int64  `json:"generation_id"`
	GenerationStatus string `json:"generation_status"`
	CommitMarker     string `json:"commit_marker"`
	ArtifactCount    int    `json:"artifact_count"`
	SourceCount      int    `json:"source_count"`
	EventContract    string `json:"event_contract"`
}

// ProjectionRepository is read-only. The only write path in the workbench is
// the revision-aware task update, which uses TaskRepository.Update directly.
type ProjectionRepository interface {
	ListTasks(ctx context.Context, limit int) ([]TaskProjection, error)
	GetTask(ctx context.Context, id string) (TaskProjection, error)
	ListSchedules(ctx context.Context, limit int) ([]ScheduleProjection, error)
	GetSchedule(ctx context.Context, id string) (ScheduleProjection, error)
	ListOccurrences(ctx context.Context, scheduleID string, limit int) ([]OccurrenceProjection, error)
	ListDeliveries(ctx context.Context, limit int) ([]DeliveryProjection, error)
	GetDelivery(ctx context.Context, id string) (DeliveryProjection, error)
	ListNotifications(ctx context.Context, limit int) ([]NotificationProjection, error)
	ListGenerations(ctx context.Context, limit int) ([]GenerationProjection, error)
	ListArtifacts(ctx context.Context, generationID int64, limit int) ([]ArtifactProjection, error)
	ListSources(ctx context.Context, limit int) ([]SourceProjection, error)
	ListExecutions(ctx context.Context, limit int) ([]ExecutionProjection, error)
	GetExecution(ctx context.Context, id string) (ExecutionProjection, error)
	RunInspector(ctx context.Context, id string) (RunInspectorProjection, error)
	IndexStatus(ctx context.Context) (IndexStatusProjection, error)
}

type projectionRepository struct{ store *Store }

func (s *Store) Projections() ProjectionRepository { return &projectionRepository{store: s} }

func boundedLimit(limit int) int {
	if limit <= 0 || limit > ProjectionListLimit {
		return ProjectionListLimit
	}
	return limit
}

func notFound(err error, what string) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	return nil
}

func (r *projectionRepository) ListTasks(ctx context.Context, limit int) ([]TaskProjection, error) {
	rows, err := r.store.db.QueryContext(ctx, r.store.bind(`SELECT id,title,description,status,revision,created_at,updated_at FROM tasks ORDER BY updated_at DESC, id LIMIT ?`), boundedLimit(limit))
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []TaskProjection{}
	for rows.Next() {
		var p TaskProjection
		if err := rows.Scan(&p.ID, &p.Title, &p.Description, &p.Status, &p.Revision, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan task: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *projectionRepository) GetTask(ctx context.Context, id string) (TaskProjection, error) {
	var p TaskProjection
	err := r.store.db.QueryRowContext(ctx, r.store.bind(`SELECT id,title,description,status,revision,created_at,updated_at FROM tasks WHERE id=?`), id).
		Scan(&p.ID, &p.Title, &p.Description, &p.Status, &p.Revision, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return TaskProjection{}, notFound(err, "get task projection")
	}
	return p, nil
}

func (r *projectionRepository) ListSchedules(ctx context.Context, limit int) ([]ScheduleProjection, error) {
	rows, err := r.store.db.QueryContext(ctx, r.store.bind(`SELECT id,cron,timezone,action_type,instruction,enabled,retry_limit,misfire_policy,backfill_limit,revision,created_at,updated_at FROM schedule_definitions ORDER BY updated_at DESC, id LIMIT ?`), boundedLimit(limit))
	if err != nil {
		return nil, fmt.Errorf("list schedules: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []ScheduleProjection{}
	for rows.Next() {
		var p ScheduleProjection
		var enabled int64
		if err := rows.Scan(&p.ID, &p.Cron, &p.Timezone, &p.ActionType, &p.Instruction, &enabled, &p.RetryLimit, &p.MisfirePolicy, &p.BackfillLimit, &p.Revision, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan schedule: %w", err)
		}
		p.Enabled = enabled != 0
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *projectionRepository) GetSchedule(ctx context.Context, id string) (ScheduleProjection, error) {
	var p ScheduleProjection
	var enabled int64
	err := r.store.db.QueryRowContext(ctx, r.store.bind(`SELECT id,cron,timezone,action_type,instruction,enabled,retry_limit,misfire_policy,backfill_limit,revision,created_at,updated_at FROM schedule_definitions WHERE id=?`), id).
		Scan(&p.ID, &p.Cron, &p.Timezone, &p.ActionType, &p.Instruction, &enabled, &p.RetryLimit, &p.MisfirePolicy, &p.BackfillLimit, &p.Revision, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return ScheduleProjection{}, notFound(err, "get schedule projection")
	}
	p.Enabled = enabled != 0
	return p, nil
}

func (r *projectionRepository) ListOccurrences(ctx context.Context, scheduleID string, limit int) ([]OccurrenceProjection, error) {
	rows, err := r.store.db.QueryContext(ctx, r.store.bind(`SELECT id,schedule_id,occurrence_key,local_time,resolved_at,timezone,tzdata_version,dst_decision,status,attempt_count,execution_id,created_at,updated_at FROM schedule_occurrences WHERE schedule_id=? ORDER BY local_time DESC, id LIMIT ?`), scheduleID, boundedLimit(limit))
	if err != nil {
		return nil, fmt.Errorf("list occurrences: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []OccurrenceProjection{}
	for rows.Next() {
		var p OccurrenceProjection
		if err := rows.Scan(&p.ID, &p.ScheduleID, &p.OccurrenceKey, &p.LocalTime, &p.ResolvedAt, &p.Timezone, &p.TzdataVersion, &p.DSTDecision, &p.Status, &p.AttemptCount, &p.ExecutionID, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan occurrence: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *projectionRepository) ListDeliveries(ctx context.Context, limit int) ([]DeliveryProjection, error) {
	rows, err := r.store.db.QueryContext(ctx, r.store.bind(`SELECT id,execution_id,occurrence_id,status,attempt_count,next_attempt_at,last_error,created_at,updated_at FROM webhook_deliveries ORDER BY updated_at DESC, id LIMIT ?`), boundedLimit(limit))
	if err != nil {
		return nil, fmt.Errorf("list deliveries: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []DeliveryProjection{}
	for rows.Next() {
		var p DeliveryProjection
		if err := rows.Scan(&p.ID, &p.ExecutionID, &p.OccurrenceID, &p.Status, &p.AttemptCount, &p.NextAttemptAt, &p.LastError, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan delivery: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *projectionRepository) GetDelivery(ctx context.Context, id string) (DeliveryProjection, error) {
	var p DeliveryProjection
	err := r.store.db.QueryRowContext(ctx, r.store.bind(`SELECT id,execution_id,occurrence_id,status,attempt_count,next_attempt_at,last_error,created_at,updated_at FROM webhook_deliveries WHERE id=?`), id).
		Scan(&p.ID, &p.ExecutionID, &p.OccurrenceID, &p.Status, &p.AttemptCount, &p.NextAttemptAt, &p.LastError, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return DeliveryProjection{}, notFound(err, "get delivery projection")
	}
	return p, nil
}

func (r *projectionRepository) ListNotifications(ctx context.Context, limit int) ([]NotificationProjection, error) {
	rows, err := r.store.db.QueryContext(ctx, r.store.bind(`SELECT id,execution_id,channel,status,revision,created_at,updated_at FROM notifications ORDER BY updated_at DESC, id LIMIT ?`), boundedLimit(limit))
	if err != nil {
		return nil, fmt.Errorf("list notifications: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []NotificationProjection{}
	for rows.Next() {
		var p NotificationProjection
		var execution sql.NullString
		if err := rows.Scan(&p.ID, &execution, &p.Channel, &p.Status, &p.Revision, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan notification: %w", err)
		}
		p.ExecutionID = execution.String
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *projectionRepository) ListGenerations(ctx context.Context, limit int) ([]GenerationProjection, error) {
	rows, err := r.store.db.QueryContext(ctx, r.store.bind(`SELECT id,status,COALESCE(commit_marker,''),created_at,COALESCE(committed_at,'') FROM knowledge_generations ORDER BY id DESC LIMIT ?`), boundedLimit(limit))
	if err != nil {
		return nil, fmt.Errorf("list generations: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []GenerationProjection{}
	for rows.Next() {
		var p GenerationProjection
		if err := rows.Scan(&p.ID, &p.Status, &p.CommitMarker, &p.CreatedAt, &p.CommittedAt); err != nil {
			return nil, fmt.Errorf("scan generation: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *projectionRepository) ListArtifacts(ctx context.Context, generationID int64, limit int) ([]ArtifactProjection, error) {
	rows, err := r.store.db.QueryContext(ctx, r.store.bind(`SELECT id,generation_id,kind,content_hash,created_at FROM artifacts WHERE generation_id=? ORDER BY kind,id LIMIT ?`), generationID, boundedLimit(limit))
	if err != nil {
		return nil, fmt.Errorf("list artifacts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []ArtifactProjection{}
	for rows.Next() {
		var p ArtifactProjection
		if err := rows.Scan(&p.ID, &p.GenerationID, &p.Kind, &p.ContentHash, &p.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan artifact: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *projectionRepository) ListSources(ctx context.Context, limit int) ([]SourceProjection, error) {
	rows, err := r.store.db.QueryContext(ctx, r.store.bind(`SELECT id,path,content_hash,format,parser_version,provider_mode,untrusted_source,created_at,updated_at FROM source_metadata ORDER BY updated_at DESC, id LIMIT ?`), boundedLimit(limit))
	if err != nil {
		return nil, fmt.Errorf("list sources: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []SourceProjection{}
	for rows.Next() {
		var p SourceProjection
		var untrusted int64
		if err := rows.Scan(&p.ID, &p.Path, &p.ContentHash, &p.Format, &p.ParserVersion, &p.ProviderMode, &untrusted, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan source: %w", err)
		}
		p.UntrustedSource = untrusted != 0
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *projectionRepository) ListExecutions(ctx context.Context, limit int) ([]ExecutionProjection, error) {
	rows, err := r.store.db.QueryContext(ctx, r.store.bind(`SELECT id,task_id,status,revision,mode,plan_version,parent_run_id,correlation_id,created_at,updated_at FROM task_harness_executions ORDER BY updated_at DESC, id LIMIT ?`), boundedLimit(limit))
	if err != nil {
		return nil, fmt.Errorf("list executions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []ExecutionProjection{}
	for rows.Next() {
		var p ExecutionProjection
		if err := rows.Scan(&p.ID, &p.TaskID, &p.Status, &p.Revision, &p.Mode, &p.PlanVersion, &p.ParentRunID, &p.CorrelationID, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan execution: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *projectionRepository) GetExecution(ctx context.Context, id string) (ExecutionProjection, error) {
	var p ExecutionProjection
	err := r.store.db.QueryRowContext(ctx, r.store.bind(`SELECT id,task_id,status,revision,mode,plan_version,parent_run_id,correlation_id,created_at,updated_at FROM task_harness_executions WHERE id=?`), id).
		Scan(&p.ID, &p.TaskID, &p.Status, &p.Revision, &p.Mode, &p.PlanVersion, &p.ParentRunID, &p.CorrelationID, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return ExecutionProjection{}, notFound(err, "get execution projection")
	}
	return p, nil
}

func (r *projectionRepository) RunInspector(ctx context.Context, id string) (RunInspectorProjection, error) {
	execution, err := r.GetExecution(ctx, id)
	if err != nil {
		return RunInspectorProjection{}, err
	}
	out := RunInspectorProjection{
		Execution:          execution,
		Plans:              []PlanVersionProjection{},
		ContextProjections: []ContextProjectionRef{},
	}
	plans, err := r.store.db.QueryContext(ctx, r.store.bind(`SELECT version,parent_version,mode,rationale,replan_reason,authority_expanded,created_at FROM task_harness_plans WHERE execution_id=? ORDER BY created_at,version LIMIT ?`), id, ProjectionListLimit)
	if err != nil {
		return RunInspectorProjection{}, fmt.Errorf("inspector plans: %w", err)
	}
	for plans.Next() {
		var p PlanVersionProjection
		var expanded int64
		if err := plans.Scan(&p.Version, &p.ParentVersion, &p.Mode, &p.Rationale, &p.ReplanReason, &expanded, &p.CreatedAt); err != nil {
			_ = plans.Close()
			return RunInspectorProjection{}, fmt.Errorf("scan inspector plan: %w", err)
		}
		p.AuthorityExpanded = expanded != 0
		out.Plans = append(out.Plans, p)
	}
	if err := plans.Err(); err != nil {
		_ = plans.Close()
		return RunInspectorProjection{}, fmt.Errorf("inspector plans rows: %w", err)
	}
	_ = plans.Close()

	var checkpoint CheckpointProjection
	var currentStep sql.NullString
	err = r.store.db.QueryRowContext(ctx, r.store.bind(`SELECT current_step,pause_reason,plan_version,task_revision FROM task_harness_checkpoints WHERE execution_id=?`), id).
		Scan(&currentStep, &checkpoint.PauseReason, &checkpoint.PlanVersion, &checkpoint.TaskRevision)
	switch {
	case err == nil:
		checkpoint.CurrentStep = currentStep.String
		out.Checkpoint = &checkpoint
	case errors.Is(err, sql.ErrNoRows):
		// A run without a checkpoint is normal; keep the pointer nil.
	default:
		return RunInspectorProjection{}, fmt.Errorf("inspector checkpoint: %w", err)
	}

	refs, err := r.store.db.QueryContext(ctx, r.store.bind(`SELECT digest,source_digest,scope,created_at FROM context_projection_snapshots WHERE run_id=? ORDER BY created_at,id LIMIT ?`), id, ProjectionListLimit)
	if err != nil {
		return RunInspectorProjection{}, fmt.Errorf("inspector context refs: %w", err)
	}
	for refs.Next() {
		var p ContextProjectionRef
		if err := refs.Scan(&p.Digest, &p.SourceDigest, &p.ContinuityScope, &p.CreatedAt); err != nil {
			_ = refs.Close()
			return RunInspectorProjection{}, fmt.Errorf("scan context ref: %w", err)
		}
		out.ContextProjections = append(out.ContextProjections, p)
	}
	if err := refs.Err(); err != nil {
		_ = refs.Close()
		return RunInspectorProjection{}, fmt.Errorf("inspector context rows: %w", err)
	}
	_ = refs.Close()

	if err := r.store.db.QueryRowContext(ctx, r.store.bind(`SELECT COUNT(*) FROM task_harness_evidence WHERE execution_id=?`), id).Scan(&out.EvidenceCount); err != nil {
		return RunInspectorProjection{}, fmt.Errorf("inspector evidence count: %w", err)
	}
	if err := r.store.db.QueryRowContext(ctx, r.store.bind(`SELECT COUNT(*) FROM agent_ui_events WHERE execution_id=?`), id).Scan(&out.EventCount); err != nil {
		return RunInspectorProjection{}, fmt.Errorf("inspector event count: %w", err)
	}
	return out, nil
}

func (r *projectionRepository) IndexStatus(ctx context.Context) (IndexStatusProjection, error) {
	out := IndexStatusProjection{EventContract: "danny.knowledge.generation"}
	err := r.store.db.QueryRowContext(ctx, r.store.bind(`SELECT id,status,COALESCE(commit_marker,'') FROM knowledge_generations WHERE status=? AND commit_marker IS NOT NULL ORDER BY id DESC LIMIT 1`), GenerationCommitted).
		Scan(&out.GenerationID, &out.GenerationStatus, &out.CommitMarker)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return IndexStatusProjection{}, fmt.Errorf("index status generation: %w", err)
	}
	if out.GenerationID > 0 {
		if err := r.store.db.QueryRowContext(ctx, r.store.bind(`SELECT COUNT(*) FROM artifacts WHERE generation_id=?`), out.GenerationID).Scan(&out.ArtifactCount); err != nil {
			return IndexStatusProjection{}, fmt.Errorf("index status artifacts: %w", err)
		}
	}
	if err := r.store.db.QueryRowContext(ctx, r.store.bind(`SELECT COUNT(*) FROM source_metadata`)).Scan(&out.SourceCount); err != nil {
		return IndexStatusProjection{}, fmt.Errorf("index status sources: %w", err)
	}
	return out, nil
}
