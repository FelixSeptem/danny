package storage

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"
)

// ParserJob is the durable application record for one parser request.
// The source bytes remain in the controlled staging root; this record stores
// the opaque object identity and the canonical parser result metadata.
type ParserJob struct {
	ID                string
	ContractVersion   string
	IdempotencyKey    string
	StagingObjectID   string
	SourceFingerprint string
	ContentHash       string
	Filename          string
	FormatHint        string
	Policy            []byte
	Status            string
	AttemptCount      int
	Result            []byte
	Failure           []byte
	ParserVersion     string
	ProviderMode      string
	CleanupState      string
	Owner             string
	LeaseUntil        *time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type ParserJobRepository interface {
	Create(context.Context, ParserJob) (ParserJob, error)
	Get(context.Context, string) (ParserJob, error)
	GetByIdempotencyKey(context.Context, string) (ParserJob, error)
	Claim(context.Context, string, time.Duration) (ParserJob, error)
	SaveResult(context.Context, string, string, []byte, []byte, string, string) (ParserJob, error)
	RequestCancel(context.Context, string) error
}

type ParserSourceRecord struct {
	ID                string
	Path              string
	ContentHash       string
	Content           []byte
	Format            string
	ParserVersion     string
	ProviderMode      string
	SourceFingerprint string
	Warnings          []byte
	UntrustedSource   bool
}

var (
	ErrRevisionConflict    = errors.New("storage: revision conflict")
	ErrLeaseLost           = errors.New("storage: lease lost")
	ErrEffectInProgress    = errors.New("storage: effect in progress")
	ErrNotFound            = errors.New("storage: not found")
	ErrInvalidTransition   = errors.New("storage: invalid execution transition")
	ErrInvalidSchedule     = errors.New("storage: invalid schedule")
	ErrOccurrenceConflict  = errors.New("storage: occurrence conflict")
	ErrDeliveryConflict    = errors.New("storage: delivery conflict")
	ErrEvidenceConflict    = errors.New("storage: native evidence conflict")
	ErrSourceOrderConflict = errors.New("storage: native source order conflict")
	ErrInvalidReplayCursor = errors.New("storage: invalid replay cursor")
	ErrReplayScope         = errors.New("storage: replay scope conflict")
	ErrReplayExpired       = errors.New("storage: replay cursor expired")
	ErrReplayGap           = errors.New("storage: replay history gap")
)

const CurrentSchemaVersion = 10

const (
	NativeEvidenceContractV1 = "danny.native-evidence.v1"
	LegacyEvidenceProvenance = "legacy_application_evidence"
	NativeCheckpointContract = "danny.native-checkpoint.v1"
	LegacyCheckpointContract = "legacy_application_checkpoint"
)

type Store struct {
	db              *sql.DB
	driver          string
	failureInjector func(string) error
}

type ContextProjectionSnapshot struct {
	ID, Scope, SessionID, TurnID, BindingID, RunID                      string
	ContractVersion, Digest, SourceDigest                               string
	ContinuityContractVersion, ContinuityReference, ContinuityExpiresAt string
	Envelope                                                            []byte
	CreatedAt, UpdatedAt                                                time.Time
}

type ContextProjectionRepository interface {
	Save(context.Context, ContextProjectionSnapshot) (ContextProjectionSnapshot, error)
	Get(context.Context, string, string) (ContextProjectionSnapshot, error)
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

type ScheduleActionType string

const (
	ScheduleActionAgent       ScheduleActionType = "agent"
	ScheduleActionFixed       ScheduleActionType = "fixed"
	ScheduleActionCompilation ScheduleActionType = "compilation"
)

type MisfirePolicy string

const (
	MisfireSkip           MisfirePolicy = "skip"
	MisfireCoalesceLatest MisfirePolicy = "coalesce_latest"
	MisfireBackfill       MisfirePolicy = "backfill"
)

type Schedule struct {
	ID, Cron, Timezone, Instruction, Payload string
	ActionType                               ScheduleActionType
	Enabled                                  bool
	RetryLimit, BackfillLimit                int
	MisfirePolicy                            MisfirePolicy
	Revision                                 int64
	CreatedAt, UpdatedAt                     time.Time
}

type SchedulePatch struct {
	Cron, Timezone, Instruction, Payload *string
	ActionType                           *ScheduleActionType
	Enabled                              *bool
	RetryLimit, BackfillLimit            *int
	MisfirePolicy                        *MisfirePolicy
}

type OccurrenceStatus string

const (
	OccurrenceAdmitted         OccurrenceStatus = "admitted"
	OccurrenceDispatching      OccurrenceStatus = "dispatching"
	OccurrenceDispatched       OccurrenceStatus = "dispatched"
	OccurrenceRunning          OccurrenceStatus = "running"
	OccurrenceSucceeded        OccurrenceStatus = "succeeded"
	OccurrenceFailed           OccurrenceStatus = "failed"
	OccurrenceRetryable        OccurrenceStatus = "retryable"
	OccurrenceDeadLettered     OccurrenceStatus = "dead_lettered"
	OccurrenceSkipped          OccurrenceStatus = "skipped"
	OccurrenceCoalesced        OccurrenceStatus = "coalesced"
	OccurrenceRecoveryRequired OccurrenceStatus = "recovery_required"
)

type DSTDecision string

const (
	DSTNormal             DSTDecision = "normal"
	DSTSkippedNonexistent DSTDecision = "skipped_nonexistent"
	DSTEarlierAmbiguous   DSTDecision = "earlier_ambiguous"
	DSTCoalesced          DSTDecision = "coalesced"
	DSTBackfilled         DSTDecision = "backfilled"
)

type ScheduleOccurrence struct {
	ID, ScheduleID, OccurrenceKey                                                  string
	ScheduleRevision                                                               int64
	LocalTime, ResolvedAt                                                          time.Time
	Timezone, TzdataVersion                                                        string
	DSTDecision                                                                    DSTDecision
	Status                                                                         OccurrenceStatus
	Revision                                                                       int64
	AttemptCount                                                                   int
	ExecutionID, CorrelationID, IdempotencyKey                                     string
	PolicyDigest, CapabilityDigest, PlanVersion, ProjectionDigest, EffectNamespace string
	LastError                                                                      string
	CreatedAt, UpdatedAt                                                           time.Time
}

type OccurrencePatch struct {
	Status                     OccurrenceStatus
	ExecutionID, CorrelationID *string
	AttemptCount               *int
	LastError                  *string
}

type DispatchAttempt struct {
	ID, OccurrenceID, OutboxID string
	Status                     string
	Attempt                    int
	Owner                      string
	Fencing                    int64
	CreatedAt, UpdatedAt       time.Time
}

type WebhookDeliveryStatus string

const (
	DeliveryPending   WebhookDeliveryStatus = "pending"
	DeliveryLeased    WebhookDeliveryStatus = "leased"
	DeliveryDelivered WebhookDeliveryStatus = "delivered"
	DeliveryRetryable WebhookDeliveryStatus = "retryable"
	DeliveryFailed    WebhookDeliveryStatus = "failed"
)

type WebhookDelivery struct {
	ID, ExecutionID, OccurrenceID, EffectKey, URL string
	Payload                                       []byte
	Status                                        WebhookDeliveryStatus
	Revision, AttemptCount                        int64
	NextAttemptAt                                 time.Time
	Owner                                         string
	LeaseUntil                                    *time.Time
	LastError                                     string
	CreatedAt, UpdatedAt                          time.Time
}

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
	// Lookup returns the recorded effect for a key without creating one.
	Lookup(context.Context, EffectKey) (EffectRecord, bool, error)
	Complete(context.Context, string, []byte) error
	Fail(context.Context, string, []byte) error
}

type OutboxRepository interface {
	Enqueue(context.Context, OutboxMessage) error
	Get(context.Context, OutboxKind, string) (OutboxMessage, error)
	ClaimByKey(context.Context, OutboxKind, string, string, time.Duration) (OutboxMessage, error)
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

type ScheduleRepository interface {
	CreateSchedule(context.Context, Schedule) (Schedule, error)
	GetSchedule(context.Context, string) (Schedule, error)
	UpdateSchedule(context.Context, string, int64, SchedulePatch) (Schedule, error)
	AdmitOccurrence(context.Context, ScheduleOccurrence, OutboxMessage) (ScheduleOccurrence, error)
	GetOccurrence(context.Context, string) (ScheduleOccurrence, error)
	FindOccurrence(context.Context, string, string) (ScheduleOccurrence, error)
	UpdateOccurrence(context.Context, string, int64, OccurrencePatch) (ScheduleOccurrence, error)
	ListOccurrences(context.Context, string) ([]ScheduleOccurrence, error)
	RecordDispatchAttempt(context.Context, DispatchAttempt) (DispatchAttempt, error)
	ListDispatchAttempts(context.Context, string) ([]DispatchAttempt, error)
}

type WebhookRepository interface {
	CreateDelivery(context.Context, WebhookDelivery) (WebhookDelivery, error)
	GetDelivery(context.Context, string) (WebhookDelivery, error)
	ClaimDelivery(context.Context, string, time.Duration) (WebhookDelivery, error)
	MarkDelivered(context.Context, string, string, int64) error
	RetryDelivery(context.Context, string, string, int64, time.Time, string) error
	FailDelivery(context.Context, string, string, int64, string) error
}

// TerminalDeliveryRepository commits an execution terminal state and its
// durable webhook intent in one application transaction.
type TerminalDeliveryRepository interface {
	CommitTerminalWithDelivery(context.Context, string, int64, ExecutionStatus, []byte, []byte, WebhookDelivery) (TaskExecution, WebhookDelivery, error)
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
	ExecutionID, RunID, PlanVersion, PlanDigest         string
	CapabilityDigest, ProjectionDigest, CurrentStep     string
	PauseReason, CompletedEffectKeys, Diagnostics       []byte
	ContractVersion                                     string
	TaskRevision                                        int64
	WriteScope, EffectNamespace                         string
	NativeCheckpointID, NativeCheckpointVersion         string
	NativeCheckpointDigest                              string
	SourceKind, SourceNamespace, SourceID, SourceCursor string
	Revision                                            int64
	CreatedAt, UpdatedAt                                time.Time
}

type ExecutionSourceCursor struct {
	ExecutionID, SourceKind, SourceNamespace, SourceID string
	CursorVersion, CursorValue, RetentionFloor         string
	ContractVersion                                    string
	SourceSequence, ObservationPosition                int64
	UpdatedAt                                          time.Time
}

type ExecutionAuthorityBinding struct {
	ExecutionID, ContractVersion                string
	TaskRevision, ScheduleRevision              int64
	PlanVersion, PlanDigest                     string
	CapabilityDigest, ProjectionDigest          string
	WriteScope, EffectNamespace                 string
	CompletedEffectKeys                         []byte
	NativeCheckpointID, NativeCheckpointVersion string
	NativeCheckpointDigest                      string
	SourceKind, SourceNamespace, SourceID       string
	SourceCursor, ContextSnapshotID             string
	CreatedAt, UpdatedAt                        time.Time
}

type ExecutionReplan struct {
	ExecutionID, PreviousPlanVersion, NewPlanVersion string
	Reason, PreviousProfileDigest, NewProfileDigest  string
	AuthorityExpanded                                bool
	CreatedAt                                        time.Time
}

type ExecutionEvidenceRecord struct {
	ID, ExecutionID, EffectKey, ParentRunID, ChildRunID string
	ObservationScope                                    string
	PlanVersion, CapabilityDigest, ProjectionDigest     string
	ContractVersion                                     string
	NativeSourceKind, NativeSourceNamespace             string
	NativeSourceID, NativeEventID, NativeVersion        string
	NativeSequence                                      *int64
	ObservationPosition                                 int64
	Status, FailureClass, Diagnostic, Result            []byte
	CreatedAt                                           time.Time
}

const ExecutionEvidenceReplayContractV1 = "danny.evidence-replay.v1"

type ExecutionEvidenceReplayCursor struct {
	ContractVersion string
	ExecutionID     string
	Scope           string
	Position        int64
}

type ExecutionEvidenceReplayRequest struct {
	Cursor ExecutionEvidenceReplayCursor
	Limit  int
}

type ExecutionEvidenceReplayPage struct {
	Evidence       []ExecutionEvidenceRecord
	NextCursor     *ExecutionEvidenceReplayCursor
	RetentionFloor int64
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
	AdmitIfTaskRevisionCurrent(context.Context, ExecutionAdmission) (TaskExecution, error)
	GetExecution(context.Context, string) (TaskExecution, error)
	UpdateExecution(context.Context, string, int64, ExecutionStatus, []byte, []byte) (TaskExecution, error)
	CommitExecutionIfTaskRevisionCurrent(context.Context, string, int64, int64, ExecutionStatus, []byte, []byte) (TaskExecution, error)
	UpdateExecutionPlan(context.Context, string, int64, string, string, string, string, string, string) (TaskExecution, error)
	BindExecutionTaskRevision(context.Context, string, int64, int64) (TaskExecution, error)
	AppendPlan(context.Context, ExecutionPlanRecord) error
	Plans(context.Context, string) ([]ExecutionPlanRecord, error)
	SaveCheckpoint(context.Context, ExecutionCheckpoint) error
	SaveNativeCheckpointAndPause(context.Context, ExecutionCheckpoint, ExecutionAuthorityBinding, int64) (TaskExecution, error)
	GetCheckpoint(context.Context, string) (ExecutionCheckpoint, error)
	RecordReplan(context.Context, ExecutionReplan) error
	Replans(context.Context, string) ([]ExecutionReplan, error)
	RecordEvidence(context.Context, ExecutionEvidenceRecord) (ExecutionEvidenceRecord, error)
	ProjectNativeEvidence(context.Context, ExecutionEvidenceRecord, ExecutionSourceCursor) (ExecutionEvidenceRecord, error)
	ReplayEvidence(context.Context, ExecutionEvidenceReplayRequest) (ExecutionEvidenceReplayPage, error)
	Evidence(context.Context, string) ([]ExecutionEvidenceRecord, error)
}

func (s *Store) Tasks() TaskRepository             { return &taskRepository{store: s} }
func (s *Store) Effects() EffectRepository         { return &effectRepository{store: s} }
func (s *Store) Outbox() OutboxRepository          { return &outboxRepository{store: s} }
func (s *Store) Generations() GenerationRepository { return &generationRepository{store: s} }
func (s *Store) Leases() LeaseRepository           { return &leaseRepository{store: s} }
func (s *Store) Executions() ExecutionRepository   { return &executionRepository{store: s} }
func (s *Store) Schedules() ScheduleRepository     { return &scheduleRepository{store: s} }
func (s *Store) Webhooks() WebhookRepository       { return &webhookRepository{store: s} }
func (s *Store) ParserJobs() ParserJobRepository   { return &parserJobRepository{store: s} }
func (s *Store) ContextProjections() ContextProjectionRepository {
	return &contextProjectionRepository{store: s}
}
func (s *Store) TerminalDeliveries() TerminalDeliveryRepository {
	return &webhookRepository{store: s}
}
