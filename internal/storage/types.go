package storage

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"
)

var (
	ErrRevisionConflict  = errors.New("storage: revision conflict")
	ErrLeaseLost         = errors.New("storage: lease lost")
	ErrEffectInProgress  = errors.New("storage: effect in progress")
	ErrNotFound          = errors.New("storage: not found")
	ErrInvalidTransition = errors.New("storage: invalid execution transition")
)

const CurrentSchemaVersion = 2

type Store struct {
	db              *sql.DB
	driver          string
	failureInjector func(string) error
}

func (s *Store) Close() error { return s.db.Close() }
func (s *Store) DB() *sql.DB  { return s.db }

func (s *Store) SetFailureInjector(injector func(string) error) { s.failureInjector = injector }

func (s *Store) fail(point string) error {
	if s.failureInjector == nil {
		return nil
	}
	return s.failureInjector(point)
}

func (s *Store) bind(query string) string {
	if s.driver != "postgres" {
		return query
	}
	var builder strings.Builder
	position := 1
	for _, char := range query {
		if char == '?' {
			builder.WriteByte('$')
			builder.WriteString(strconv.Itoa(position))
			position++
			continue
		}
		builder.WriteRune(char)
	}
	return builder.String()
}

type Task struct {
	ID          string
	Title       string
	Description string
	Status      string
	Revision    int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type TaskPatch struct {
	Title       *string
	Description *string
	Status      string
}

type EffectKey struct {
	Namespace string
	Subject   string
	Key       string
}

type EffectStatus string

const (
	EffectInProgress EffectStatus = "in_progress"
	EffectCompleted  EffectStatus = "completed"
	EffectFailed     EffectStatus = "failed"
)

type EffectRecord struct {
	ID        string
	Key       EffectKey
	Status    EffectStatus
	Request   []byte
	Result    []byte
	CreatedAt time.Time
	UpdatedAt time.Time
}

type OutboxKind string

const (
	OutboxKindSchedule OutboxKind = "schedule_dispatch"
	OutboxKindStele    OutboxKind = "stele_projection"
	OutboxKindWebhook  OutboxKind = "webhook_delivery"
)

type OutboxMessage struct {
	ID             string
	Kind           OutboxKind
	IdempotencyKey string
	PayloadVersion int
	Payload        []byte
	Status         string
	Attempts       int
	Owner          string
	LeaseUntil     *time.Time
	AvailableAt    time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type GenerationStatus string

const (
	GenerationStaged    GenerationStatus = "staged"
	GenerationCommitted GenerationStatus = "committed"
)

type Generation struct {
	ID           int64
	Status       GenerationStatus
	CommitMarker string
	CreatedAt    time.Time
	CommittedAt  *time.Time
}

type Artifact struct {
	ID           string
	GenerationID int64
	Kind         string
	ContentHash  string
	Content      []byte
	Lineage      []byte
}

type Lease struct {
	ResourceType string
	ResourceID   string
	Owner        string
	Fencing      int64
	LeaseUntil   time.Time
	HeartbeatAt  time.Time
}

type TaskRepository interface {
	Create(context.Context, Task) (Task, error)
	Get(context.Context, string) (Task, error)
	Update(context.Context, string, int64, TaskPatch) (Task, error)
}

type EffectRepository interface {
	Begin(context.Context, EffectKey, []byte) (EffectRecord, error)
	Complete(context.Context, string, []byte) error
	Fail(context.Context, string, []byte) error
}

type OutboxRepository interface {
	Enqueue(context.Context, OutboxMessage) error
	Claim(context.Context, OutboxKind, string, time.Duration) (OutboxMessage, error)
	Ack(context.Context, string, string) error
	Retry(context.Context, string, string, time.Time) error
}

type GenerationRepository interface {
	Begin(context.Context) (Generation, error)
	AddArtifact(context.Context, int64, Artifact) error
	Commit(context.Context, int64, []byte) error
	Committed(context.Context) (Generation, error)
	Materialize(context.Context, int64, string) error
}

type LeaseRepository interface {
	Acquire(context.Context, string, string, string, time.Duration) (Lease, error)
	Release(context.Context, string, string, string, int64) error
}

// ExecutionStatus is the durable state of a task execution harness run.
type ExecutionStatus string

const (
	ExecutionAdmitted         ExecutionStatus = "admitted"
	ExecutionDispatching      ExecutionStatus = "dispatching"
	ExecutionRunning          ExecutionStatus = "running"
	ExecutionPaused           ExecutionStatus = "paused"
	ExecutionRetryable        ExecutionStatus = "retryable"
	ExecutionReplanning       ExecutionStatus = "replanning"
	ExecutionSucceeded        ExecutionStatus = "succeeded"
	ExecutionFailed           ExecutionStatus = "failed"
	ExecutionBlocked          ExecutionStatus = "blocked"
	ExecutionRecoveryRequired ExecutionStatus = "recovery_required"
)

// TaskExecution is the application-owned execution record. Baymax run state
// is correlated through ParentRunID and CorrelationID and is never the source
// of truth for this record.
type TaskExecution struct {
	ID, TaskID, ParentRunID, CorrelationID, IdempotencyKey, Input string
	TaskRevision                                                  int64
	Status                                                        ExecutionStatus
	Revision                                                      int64
	Mode, PlanVersion, PlanDigest                                 string
	CapabilityDigest, ProjectionDigest                            string
	Result, FailureClass                                          []byte
	CreatedAt, UpdatedAt                                          time.Time
}

// ExecutionPlanRecord is an immutable plan notebook entry.
type ExecutionPlanRecord struct {
	ExecutionID, Version, ParentVersion, Mode    string
	ProfileDigest, PlanDigest, CapabilityDigest  string
	ProjectionDigest, Rationale, ReplanReason    string
	Budget, WriteScope                           int
	ChildLimit, PerChildBudget, AggregationLimit int
	CompletedEffectKeys, Diagnostics             []byte
	AuthorityExpanded                            bool
	CreatedAt                                    time.Time
}

type ExecutionCheckpoint struct {
	ExecutionID, RunID, PlanVersion, PlanDigest     string
	CapabilityDigest, ProjectionDigest, CurrentStep string
	PauseReason, CompletedEffectKeys, Diagnostics   []byte
	Revision                                        int64
	CreatedAt, UpdatedAt                            time.Time
}

type ExecutionReplan struct {
	ExecutionID, PreviousPlanVersion, NewPlanVersion string
	Reason, PreviousProfileDigest, NewProfileDigest  string
	AuthorityExpanded                                bool
	CreatedAt                                        time.Time
}

type ExecutionEvidenceRecord struct {
	ID, ExecutionID, EffectKey, ParentRunID, ChildRunID string
	PlanVersion, CapabilityDigest, ProjectionDigest     string
	Status, FailureClass, Diagnostic, Result            []byte
	CreatedAt                                           time.Time
}

type ExecutionAdmission struct {
	Execution TaskExecution
	Plan      ExecutionPlanRecord
}

// ExecutionRepository is shared by SQLite and PostgreSQL implementations.
// Mutations use the execution revision and stable effect keys for replay
// safety.
type ExecutionRepository interface {
	Admit(context.Context, ExecutionAdmission) (TaskExecution, error)
	GetExecution(context.Context, string) (TaskExecution, error)
	UpdateExecution(context.Context, string, int64, ExecutionStatus, []byte, []byte) (TaskExecution, error)
	UpdateExecutionPlan(context.Context, string, int64, string, string, string, string, string, string) (TaskExecution, error)
	AppendPlan(context.Context, ExecutionPlanRecord) error
	Plans(context.Context, string) ([]ExecutionPlanRecord, error)
	SaveCheckpoint(context.Context, ExecutionCheckpoint) error
	GetCheckpoint(context.Context, string) (ExecutionCheckpoint, error)
	RecordReplan(context.Context, ExecutionReplan) error
	Replans(context.Context, string) ([]ExecutionReplan, error)
	RecordEvidence(context.Context, ExecutionEvidenceRecord) (ExecutionEvidenceRecord, error)
	Evidence(context.Context, string) ([]ExecutionEvidenceRecord, error)
}

func (s *Store) Tasks() TaskRepository             { return &taskRepository{store: s} }
func (s *Store) Effects() EffectRepository         { return &effectRepository{store: s} }
func (s *Store) Outbox() OutboxRepository          { return &outboxRepository{store: s} }
func (s *Store) Generations() GenerationRepository { return &generationRepository{store: s} }
func (s *Store) Leases() LeaseRepository           { return &leaseRepository{store: s} }
func (s *Store) Executions() ExecutionRepository   { return &executionRepository{store: s} }
