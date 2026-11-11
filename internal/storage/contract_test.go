package storage

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/FelixSeptem/danny/internal/parser"
	"github.com/google/uuid"
)

func TestPostgresRepositoryContract(t *testing.T) {
	dsn := os.Getenv("DANNY_TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("DANNY_TEST_POSTGRES_URL is not configured")
	}
	ctx := context.Background()
	store, err := OpenPostgres(ctx, dsn)
	if err != nil {
		t.Fatalf("OpenPostgres() error = %v", err)
	}
	defer func() { _ = store.Close() }()
	schema := "danny_contract_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := store.DB().ExecContext(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatalf("create contract schema: %v", err)
	}
	defer func() { _, _ = store.DB().ExecContext(ctx, `DROP SCHEMA `+schema+` CASCADE`) }()
	if _, err := store.DB().ExecContext(ctx, `SET search_path TO `+schema); err != nil {
		t.Fatalf("set contract schema: %v", err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	task, err := store.Tasks().Create(ctx, Task{ID: "contract-" + uuid.NewString(), Title: "postgres contract"})
	if err != nil {
		t.Fatalf("Create task: %v", err)
	}
	updated, err := store.Tasks().Update(ctx, task.ID, task.Revision, TaskPatch{Status: "done"})
	if err != nil || updated.Revision != task.Revision+1 {
		t.Fatalf("Update task = %#v, err=%v", updated, err)
	}
	if _, err := store.Tasks().Update(ctx, task.ID, task.Revision, TaskPatch{Status: "stale"}); err != ErrRevisionConflict {
		t.Fatalf("stale Update error = %v, want ErrRevisionConflict", err)
	}

	effect, err := store.Effects().Begin(ctx, EffectKey{Namespace: "contract", Subject: task.ID, Key: "once"}, []byte("request"))
	if err != nil {
		t.Fatalf("Begin effect: %v", err)
	}
	if err := store.Effects().Complete(ctx, effect.ID, []byte("result")); err != nil {
		t.Fatalf("Complete effect: %v", err)
	}
	replay, err := store.Effects().Begin(ctx, effect.Key, []byte("request"))
	if err != nil || replay.Status != EffectCompleted || string(replay.Result) != "result" {
		t.Fatalf("Replay effect = %#v, err=%v", replay, err)
	}

	message := OutboxMessage{Kind: OutboxKindWebhook, IdempotencyKey: "contract:" + uuid.NewString(), Payload: []byte("payload")}
	if err := store.Outbox().Enqueue(ctx, message); err != nil {
		t.Fatalf("Enqueue outbox: %v", err)
	}
	claimed, err := store.Outbox().Claim(ctx, OutboxKindWebhook, "contract-worker", time.Minute)
	if err != nil || claimed.IdempotencyKey != message.IdempotencyKey {
		t.Fatalf("Claim outbox = %#v, err=%v", claimed, err)
	}
	if err := store.Outbox().Ack(ctx, claimed.ID, "contract-worker"); err != nil {
		t.Fatalf("Ack outbox: %v", err)
	}
	admitted, err := store.Executions().Admit(ctx, ExecutionAdmission{Execution: TaskExecution{ID: "execution-" + uuid.NewString(), TaskID: task.ID, TaskRevision: updated.Revision, CorrelationID: "corr-" + uuid.NewString(), IdempotencyKey: "execution-" + uuid.NewString(), Mode: "direct", PlanVersion: "planner.v1", PlanDigest: "plan", CapabilityDigest: "cap", ProjectionDigest: "projection", Input: "contract"}, Plan: ExecutionPlanRecord{Version: "planner.v1", Mode: "direct", PlanDigest: "plan", CapabilityDigest: "cap", ProjectionDigest: "projection"}})
	if err != nil {
		t.Fatalf("execution admission: %v", err)
	}
	if admitted.Status != ExecutionAdmitted {
		t.Fatalf("execution status = %s", admitted.Status)
	}
	dispatching, err := store.Executions().UpdateExecution(ctx, admitted.ID, admitted.Revision, ExecutionDispatching, nil, nil)
	if err != nil {
		t.Fatalf("execution dispatch transition: %v", err)
	}
	running, err := store.Executions().UpdateExecution(ctx, dispatching.ID, dispatching.Revision, ExecutionRunning, nil, nil)
	if err != nil {
		t.Fatalf("execution running transition: %v", err)
	}
	if _, err := store.Executions().UpdateExecution(ctx, running.ID, running.Revision, ExecutionSucceeded, []byte("ok"), nil); err != nil {
		t.Fatalf("execution terminal update: %v", err)
	}
	lease, err := store.Leases().Acquire(ctx, "contract", task.ID, "worker-1", time.Minute)
	if err != nil {
		t.Fatalf("Acquire lease: %v", err)
	}
	if err := store.Leases().Release(ctx, "contract", task.ID, "stale-worker", lease.Fencing); err != ErrLeaseLost {
		t.Fatalf("stale Release error = %v, want ErrLeaseLost", err)
	}
	parserJob, err := store.ParserJobs().Create(ctx, ParserJob{ID: "parser-" + uuid.NewString(), IdempotencyKey: "parser-key-" + uuid.NewString(), StagingObjectID: "object-1", SourceFingerprint: "source-1", ContentHash: "hash-1", Filename: "note.md"})
	if err != nil {
		t.Fatalf("parser job create: %v", err)
	}
	claimedParser, err := store.ParserJobs().Claim(ctx, "parser-worker", time.Minute)
	if err != nil || claimedParser.ID != parserJob.ID || claimedParser.Status != string(parser.StatusRunning) {
		t.Fatalf("parser job claim = %#v, err=%v", claimedParser, err)
	}
	if _, err := store.ParserJobs().SaveResult(ctx, parserJob.ID, "parser-worker", []byte(`{"ok":true}`), nil, string(parser.StatusSucceeded), "local"); err != nil {
		t.Fatalf("parser job result: %v", err)
	}
	cancelJob, err := store.ParserJobs().Create(ctx, ParserJob{ID: "parser-cancel-" + uuid.NewString(), IdempotencyKey: "parser-cancel-key-" + uuid.NewString(), StagingObjectID: "object-cancel", SourceFingerprint: "source-cancel", ContentHash: "hash-cancel", Filename: "cancel.md"})
	if err != nil {
		t.Fatalf("parser cancellation job create: %v", err)
	}
	claimedCancel, err := store.ParserJobs().Claim(ctx, "parser-cancel-worker", time.Minute)
	if err != nil || claimedCancel.ID != cancelJob.ID {
		t.Fatalf("parser cancellation claim = %#v, err=%v", claimedCancel, err)
	}
	if err := store.ParserJobs().RequestCancel(ctx, cancelJob.ID); err != nil {
		t.Fatalf("parser cancellation request: %v", err)
	}
	if _, err := store.ParserJobs().SaveResult(ctx, cancelJob.ID, "parser-cancel-worker", []byte(`{"stale":true}`), nil, string(parser.StatusSucceeded), "local"); err != ErrLeaseLost {
		t.Fatalf("cancelled parser result error = %v, want ErrLeaseLost", err)
	}
	recoveryJob, err := store.ParserJobs().Create(ctx, ParserJob{ID: "parser-recovery-" + uuid.NewString(), IdempotencyKey: "parser-recovery-key-" + uuid.NewString(), StagingObjectID: "object-recovery", SourceFingerprint: "source-recovery", ContentHash: "hash-recovery", Filename: "recovery.md"})
	if err != nil {
		t.Fatalf("parser recovery job create: %v", err)
	}
	firstRecovery, err := store.ParserJobs().Claim(ctx, "parser-recovery-worker-1", -time.Millisecond)
	if err != nil || firstRecovery.ID != recoveryJob.ID {
		t.Fatalf("first parser recovery claim = %#v, err=%v", firstRecovery, err)
	}
	secondRecovery, err := store.ParserJobs().Claim(ctx, "parser-recovery-worker-2", time.Minute)
	if err != nil || secondRecovery.ID != recoveryJob.ID || secondRecovery.AttemptCount != firstRecovery.AttemptCount+1 {
		t.Fatalf("second parser recovery claim = %#v, err=%v", secondRecovery, err)
	}
	if _, err := store.ParserJobs().SaveResult(ctx, recoveryJob.ID, "parser-recovery-worker-1", []byte(`{"stale":true}`), nil, string(parser.StatusSucceeded), "local"); err != ErrLeaseLost {
		t.Fatalf("stale parser recovery result error = %v, want ErrLeaseLost", err)
	}
	if _, err := store.ParserJobs().SaveResult(ctx, recoveryJob.ID, "parser-recovery-worker-2", []byte(`{"ok":true}`), nil, string(parser.StatusSucceeded), "local"); err != nil {
		t.Fatalf("parser recovery result: %v", err)
	}
	sourceID := "parser-source-" + uuid.NewString()
	transactionJobID := "parser-transaction-" + uuid.NewString()
	if err := store.WithTransaction(ctx, func(tx *Transaction) error {
		return tx.CreateParserJobWithSource(ctx, ParserJob{ID: transactionJobID, ContractVersion: parser.ContractVersion, IdempotencyKey: "parser-transaction-key-" + uuid.NewString(), StagingObjectID: "object-transaction", SourceFingerprint: "source-transaction", ContentHash: "hash-transaction", Filename: "transaction.md"}, ParserSourceRecord{ID: sourceID, Path: "transaction.md", ContentHash: "hash-transaction", Content: []byte("# Transaction\n"), Format: "md", ParserVersion: "native", ProviderMode: "local"})
	}); err != nil {
		t.Fatalf("parser source transaction commit: %v", err)
	}
	if _, err := store.ParserJobs().Get(ctx, transactionJobID); err != nil {
		t.Fatalf("parser job after transaction commit: %v", err)
	}
	var sourceCount int
	if err := store.DB().QueryRowContext(ctx, store.bind(`SELECT COUNT(*) FROM source_metadata WHERE id=?`), sourceID).Scan(&sourceCount); err != nil || sourceCount != 1 {
		t.Fatalf("parser source after transaction commit count=%d err=%v", sourceCount, err)
	}
	store.SetFailureInjector(func(point string) error {
		if point == "state_outbox_commit" {
			return os.ErrClosed
		}
		return nil
	})
	rolledBackJobID := "parser-rollback-" + uuid.NewString()
	rolledBackSourceID := "parser-rollback-source-" + uuid.NewString()
	err = store.WithTransaction(ctx, func(tx *Transaction) error {
		return tx.CreateParserJobWithSource(ctx, ParserJob{ID: rolledBackJobID, ContractVersion: parser.ContractVersion, IdempotencyKey: "parser-rollback-key-" + uuid.NewString(), StagingObjectID: "object-rollback", SourceFingerprint: "source-rollback", ContentHash: "hash-rollback", Filename: "rollback.md"}, ParserSourceRecord{ID: rolledBackSourceID, Path: "rollback.md", ContentHash: "hash-rollback", Content: []byte("# Rollback\n"), Format: "md", ParserVersion: "native", ProviderMode: "local"})
	})
	store.SetFailureInjector(nil)
	if err == nil {
		t.Fatal("parser crash-boundary transaction unexpectedly committed")
	}
	if _, err := store.ParserJobs().Get(ctx, rolledBackJobID); err != ErrNotFound {
		t.Fatalf("parser job after crash-boundary rollback error = %v, want ErrNotFound", err)
	}
	if err := store.DB().QueryRowContext(ctx, store.bind(`SELECT COUNT(*) FROM source_metadata WHERE id=?`), rolledBackSourceID).Scan(&sourceCount); err != nil || sourceCount != 0 {
		t.Fatalf("parser source after crash-boundary rollback count=%d err=%v", sourceCount, err)
	}
	generation, err := store.Generations().Begin(ctx)
	if err != nil {
		t.Fatalf("materialization generation begin: %v", err)
	}
	if err := store.Generations().AddArtifact(ctx, generation.ID, Artifact{ID: "parser-materialized-artifact", Kind: "summary", ContentHash: "parser-materialized-hash", Content: []byte("parser materialized content")}); err != nil {
		t.Fatalf("materialization artifact add: %v", err)
	}
	if err := store.Generations().Commit(ctx, generation.ID, []byte(`{"artifacts":["parser-materialized-artifact"]}`)); err != nil {
		t.Fatalf("materialization generation commit: %v", err)
	}
	materializationDir := t.TempDir()
	if err := store.Generations().Materialize(ctx, generation.ID, materializationDir); err != nil {
		t.Fatalf("materialization: %v", err)
	}
	materializedPath := filepath.Join(materializationDir, "summary", "parser-materialized-artifact")
	if err := os.Remove(materializedPath); err != nil {
		t.Fatalf("remove materialized artifact: %v", err)
	}
	if err := store.Reconcile(ctx, materializationDir); err != nil {
		t.Fatalf("materialization recovery reconcile: %v", err)
	}
	if content, err := os.ReadFile(materializedPath); err != nil || string(content) != "parser materialized content" {
		t.Fatalf("recovered materialized artifact content=%q err=%v", content, err)
	}
}

func TestPostgresV7MigrationContract(t *testing.T) {
	dsn := os.Getenv("DANNY_TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("DANNY_TEST_POSTGRES_URL is not configured")
	}
	ctx := context.Background()
	store, err := OpenPostgres(ctx, dsn)
	if err != nil {
		t.Fatalf("OpenPostgres() error = %v", err)
	}
	defer func() { _ = store.Close() }()
	store.DB().SetMaxOpenConns(1)

	newSchema := "danny_v7_fresh_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := store.DB().ExecContext(ctx, `CREATE SCHEMA `+newSchema); err != nil {
		t.Fatalf("create fresh schema: %v", err)
	}
	defer func() { _, _ = store.DB().ExecContext(ctx, `DROP SCHEMA `+newSchema+` CASCADE`) }()
	if _, err := store.DB().ExecContext(ctx, `SET search_path TO `+newSchema); err != nil {
		t.Fatalf("set fresh schema: %v", err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("fresh Migrate(): %v", err)
	}
	version, err := store.SchemaVersion(ctx)
	if err != nil || version != CurrentSchemaVersion {
		t.Fatalf("fresh schema version=%d err=%v", version, err)
	}
	for _, table := range []string{"execution_source_cursors", "execution_authority_bindings"} {
		var name string
		if err := store.DB().QueryRowContext(ctx, `SELECT table_name FROM information_schema.tables WHERE table_schema=current_schema() AND table_name=$1`, table).Scan(&name); err != nil || name != table {
			t.Fatalf("fresh migration table %q: name=%q err=%v", table, name, err)
		}
	}

	legacySchema := "danny_v7_legacy_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := store.DB().ExecContext(ctx, `CREATE SCHEMA `+legacySchema); err != nil {
		t.Fatalf("create legacy schema: %v", err)
	}
	defer func() { _, _ = store.DB().ExecContext(ctx, `DROP SCHEMA `+legacySchema+` CASCADE`) }()
	if _, err := store.DB().ExecContext(ctx, `SET search_path TO `+legacySchema); err != nil {
		t.Fatalf("set legacy schema: %v", err)
	}
	legacySetup := []string{
		`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`,
		`INSERT INTO schema_migrations(version, applied_at) VALUES (6, '2026-10-20T00:00:00Z')`,
		`CREATE TABLE task_harness_executions (id TEXT PRIMARY KEY, task_id TEXT NOT NULL, task_revision BIGINT NOT NULL DEFAULT 0, status TEXT NOT NULL, revision BIGINT NOT NULL DEFAULT 1, mode TEXT NOT NULL, plan_version TEXT NOT NULL, plan_digest TEXT NOT NULL, capability_digest TEXT NOT NULL, projection_digest TEXT NOT NULL, parent_run_id TEXT NOT NULL DEFAULT '', correlation_id TEXT NOT NULL, idempotency_key TEXT NOT NULL, input TEXT NOT NULL DEFAULT '', result BYTEA NOT NULL DEFAULT '', failure_class BYTEA NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL, UNIQUE(idempotency_key))`,
		`CREATE TABLE task_harness_evidence (id TEXT PRIMARY KEY, execution_id TEXT NOT NULL, effect_key TEXT NOT NULL, parent_run_id TEXT NOT NULL DEFAULT '', child_run_id TEXT NOT NULL DEFAULT '', plan_version TEXT NOT NULL, capability_digest TEXT NOT NULL, projection_digest TEXT NOT NULL, status BYTEA NOT NULL, failure_class BYTEA NOT NULL DEFAULT '', diagnostic BYTEA NOT NULL DEFAULT '', result BYTEA NOT NULL DEFAULT '', created_at TEXT NOT NULL, UNIQUE(execution_id, effect_key))`,
		`CREATE TABLE task_harness_checkpoints (execution_id TEXT PRIMARY KEY, run_id TEXT NOT NULL, plan_version TEXT NOT NULL, plan_digest TEXT NOT NULL, capability_digest TEXT NOT NULL, projection_digest TEXT NOT NULL, current_step TEXT NOT NULL DEFAULT '', pause_reason TEXT NOT NULL, completed_effect_keys BYTEA NOT NULL DEFAULT '', diagnostics BYTEA NOT NULL DEFAULT '', revision BIGINT NOT NULL DEFAULT 1, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
		`INSERT INTO task_harness_executions(id, task_id, status, mode, plan_version, plan_digest, capability_digest, projection_digest, correlation_id, idempotency_key, created_at, updated_at) VALUES ('legacy-execution', 'task-1', 'paused', 'direct', 'plan-v1', 'plan-digest', 'cap-digest', 'projection-digest', 'corr-1', 'legacy-key', '2026-10-20T00:00:00Z', '2026-10-20T00:00:00Z')`,
		`INSERT INTO task_harness_evidence(id, execution_id, effect_key, plan_version, capability_digest, projection_digest, status, created_at) VALUES ('legacy-event', 'legacy-execution', 'parent', 'plan-v1', 'cap-digest', 'projection-digest', 'succeeded', '2026-10-20T00:00:00Z')`,
		`INSERT INTO task_harness_checkpoints(execution_id, run_id, plan_version, plan_digest, capability_digest, projection_digest, pause_reason, created_at, updated_at) VALUES ('legacy-execution', 'legacy-run', 'plan-v1', 'plan-digest', 'cap-digest', 'projection-digest', 'approval_required', '2026-10-20T00:00:00Z', '2026-10-20T00:00:00Z')`,
	}
	for _, statement := range legacySetup {
		if _, err := store.DB().ExecContext(ctx, statement); err != nil {
			t.Fatalf("prepare v6 schema: %v\nSQL: %s", err, statement)
		}
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("v6 Migrate(): %v", err)
	}
	version, err = store.SchemaVersion(ctx)
	if err != nil || version != CurrentSchemaVersion {
		t.Fatalf("migrated schema version=%d err=%v", version, err)
	}
	var executionStatus, evidenceContract, nativeEventID, checkpointContract, nativeCheckpointID string
	var observationPosition int64
	if err := store.DB().QueryRowContext(ctx, `SELECT status FROM task_harness_executions WHERE id='legacy-execution'`).Scan(&executionStatus); err != nil || executionStatus != "paused" {
		t.Fatalf("legacy execution status=%q err=%v", executionStatus, err)
	}
	if err := store.DB().QueryRowContext(ctx, `SELECT evidence_contract_version, native_event_id, observation_position FROM task_harness_evidence WHERE id='legacy-event'`).Scan(&evidenceContract, &nativeEventID, &observationPosition); err != nil {
		t.Fatal(err)
	}
	if evidenceContract != LegacyEvidenceProvenance || nativeEventID != "" || observationPosition != 0 {
		t.Fatalf("legacy evidence provenance=%q native_id=%q position=%d", evidenceContract, nativeEventID, observationPosition)
	}
	if err := store.DB().QueryRowContext(ctx, `SELECT checkpoint_contract_version, native_checkpoint_id FROM task_harness_checkpoints WHERE execution_id='legacy-execution'`).Scan(&checkpointContract, &nativeCheckpointID); err != nil {
		t.Fatal(err)
	}
	if checkpointContract != LegacyCheckpointContract || nativeCheckpointID != "" {
		t.Fatalf("legacy checkpoint contract=%q native_id=%q", checkpointContract, nativeCheckpointID)
	}

	newerSchema := "danny_v7_newer_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := store.DB().ExecContext(ctx, `CREATE SCHEMA `+newerSchema); err != nil {
		t.Fatalf("create newer schema: %v", err)
	}
	defer func() { _, _ = store.DB().ExecContext(ctx, `DROP SCHEMA `+newerSchema+` CASCADE`) }()
	if _, err := store.DB().ExecContext(ctx, `SET search_path TO `+newerSchema); err != nil {
		t.Fatalf("set newer schema: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO schema_migrations(version, applied_at) VALUES ($1, '2026-10-22T00:00:00Z')`, CurrentSchemaVersion+1); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx); err == nil {
		t.Fatal("migration accepted unsupported newer schema contract")
	}
}

func TestPostgresNativeEvidenceProjectionContract(t *testing.T) {
	dsn := os.Getenv("DANNY_TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("DANNY_TEST_POSTGRES_URL is not configured")
	}
	ctx := context.Background()
	store, err := OpenPostgres(ctx, dsn)
	if err != nil {
		t.Fatalf("OpenPostgres() error = %v", err)
	}
	defer func() { _ = store.Close() }()
	store.DB().SetMaxOpenConns(1)
	schema := "danny_native_projection_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := store.DB().ExecContext(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = store.DB().ExecContext(ctx, `DROP SCHEMA `+schema+` CASCADE`) }()
	if _, err := store.DB().ExecContext(ctx, `SET search_path TO `+schema); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	repo := store.Executions()
	if _, err := repo.Admit(ctx, ExecutionAdmission{Execution: TaskExecution{ID: "pg-native", TaskID: "task", CorrelationID: "corr", IdempotencyKey: "pg-native", Mode: "direct", PlanVersion: "v1", PlanDigest: "plan", CapabilityDigest: "cap", ProjectionDigest: "projection"}, Plan: ExecutionPlanRecord{ExecutionID: "pg-native", Version: "v1", Mode: "direct", PlanDigest: "plan", CapabilityDigest: "cap", ProjectionDigest: "projection"}}); err != nil {
		t.Fatal(err)
	}
	evidence := ExecutionEvidenceRecord{ExecutionID: "pg-native", ObservationScope: "scope", PlanVersion: "v1", CapabilityDigest: "cap", ProjectionDigest: "projection", ContractVersion: NativeEvidenceContractV1, NativeSourceKind: "runner", NativeSourceNamespace: "history-v1", NativeSourceID: "run-1", NativeEventID: "event-1", NativeVersion: "event.v1", NativeSequence: ptrInt64(10), Status: []byte(`{"kind":"tool_completed"}`), Result: []byte(`{"ok":true}`)}
	cursor := ExecutionSourceCursor{ExecutionID: "pg-native", SourceKind: "runner", SourceNamespace: "history-v1", SourceID: "run-1", CursorVersion: "cursor.v1", CursorValue: "cursor-10", SourceSequence: 10, ContractVersion: NativeEvidenceContractV1}
	first, err := repo.ProjectNativeEvidence(ctx, evidence, cursor)
	if err != nil {
		t.Fatalf("first projection: %v", err)
	}
	duplicate, err := repo.ProjectNativeEvidence(ctx, evidence, cursor)
	if err != nil || duplicate.ObservationPosition != first.ObservationPosition {
		t.Fatalf("duplicate projection=%#v err=%v", duplicate, err)
	}
	conflicting := evidence
	conflicting.Result = []byte(`{"ok":false}`)
	if _, err := repo.ProjectNativeEvidence(ctx, conflicting, cursor); !errors.Is(err, ErrEvidenceConflict) {
		t.Fatalf("identity conflict=%v", err)
	}
	store.SetFailureInjector(func(point string) error {
		if point == "native_evidence_before_commit" {
			return errors.New("injected rollback")
		}
		return nil
	})
	failing := evidence
	failing.NativeEventID = "event-fail"
	failing.NativeSequence = ptrInt64(11)
	failingCursor := cursor
	failingCursor.CursorValue = "cursor-11"
	failingCursor.SourceSequence = 11
	if _, err := repo.ProjectNativeEvidence(ctx, failing, failingCursor); err == nil {
		t.Fatal("failure injection unexpectedly committed")
	}
	store.SetFailureInjector(nil)
	var count int
	if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM task_harness_evidence WHERE execution_id='pg-native'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("evidence count=%d err=%v", count, err)
	}
	var cursorValue string
	if err := store.DB().QueryRowContext(ctx, `SELECT cursor_value FROM execution_source_cursors WHERE execution_id='pg-native' AND source_id='run-1'`).Scan(&cursorValue); err != nil || cursorValue != "cursor-10" {
		t.Fatalf("cursor=%q err=%v", cursorValue, err)
	}
	store.SetFailureInjector(func(point string) error {
		if point == "native_evidence_after_commit" {
			return errors.New("injected lost acknowledgement")
		}
		return nil
	})
	committed := evidence
	committed.NativeEventID = "event-committed"
	committed.NativeSequence = ptrInt64(11)
	committedCursor := cursor
	committedCursor.CursorValue = "cursor-11"
	committedCursor.SourceSequence = 11
	if _, err := repo.ProjectNativeEvidence(ctx, committed, committedCursor); err == nil {
		t.Fatal("post-commit failure injection did not fire")
	}
	store.SetFailureInjector(nil)
	replayed, err := repo.ProjectNativeEvidence(ctx, committed, committedCursor)
	if err != nil || replayed.ObservationPosition != 2 {
		t.Fatalf("post-commit replay=%#v err=%v", replayed, err)
	}
	page, err := repo.ReplayEvidence(ctx, ExecutionEvidenceReplayRequest{Cursor: ExecutionEvidenceReplayCursor{ContractVersion: ExecutionEvidenceReplayContractV1, ExecutionID: "pg-native", Scope: "scope", Position: 0}, Limit: 1})
	if err != nil || len(page.Evidence) != 1 || page.Evidence[0].ObservationPosition != 1 || page.NextCursor == nil || page.RetentionFloor != 1 {
		t.Fatalf("replay page=%#v err=%v", page, err)
	}
	page, err = repo.ReplayEvidence(ctx, ExecutionEvidenceReplayRequest{Cursor: *page.NextCursor, Limit: 2})
	if err != nil || len(page.Evidence) != 1 || page.Evidence[0].ObservationPosition != 2 || page.NextCursor != nil {
		t.Fatalf("replay tail=%#v err=%v", page, err)
	}
	regression := evidence
	regression.NativeEventID = "event-regression"
	regression.NativeSequence = nil
	regressionCursor := cursor
	regressionCursor.CursorValue = "cursor-9"
	regressionCursor.SourceSequence = 9
	if _, err := repo.ProjectNativeEvidence(ctx, regression, regressionCursor); !errors.Is(err, ErrSourceOrderConflict) {
		t.Fatalf("source cursor regression=%v", err)
	}
	concurrent := make(chan error, 2)
	for _, eventID := range []string{"event-concurrent-a", "event-concurrent-b"} {
		go func(eventID string) {
			_, err := repo.ProjectNativeEvidence(ctx, ExecutionEvidenceRecord{ExecutionID: "pg-native", ObservationScope: "scope", PlanVersion: "v1", ContractVersion: NativeEvidenceContractV1, NativeSourceKind: "runner", NativeSourceNamespace: "history-v1", NativeSourceID: "run-2", NativeEventID: eventID, NativeVersion: "event.v1", Status: []byte(eventID)}, ExecutionSourceCursor{ExecutionID: "pg-native", SourceKind: "runner", SourceNamespace: "history-v1", SourceID: "run-2", CursorVersion: "cursor.v1", CursorValue: eventID, ContractVersion: NativeEvidenceContractV1})
			concurrent <- err
		}(eventID)
	}
	for range 2 {
		if err := <-concurrent; err != nil {
			t.Fatalf("concurrent projection: %v", err)
		}
	}
}

func TestPostgresNativeEvidenceProjectionConcurrentWriters(t *testing.T) {
	dsn := os.Getenv("DANNY_TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("DANNY_TEST_POSTGRES_URL is not configured")
	}
	ctx := context.Background()
	admin, err := OpenPostgres(ctx, dsn)
	if err != nil {
		t.Fatalf("OpenPostgres() error = %v", err)
	}
	defer func() { _ = admin.Close() }()
	schema := "danny_native_concurrent_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := admin.DB().ExecContext(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = admin.DB().ExecContext(ctx, `DROP SCHEMA `+schema+` CASCADE`) }()
	store, err := OpenPostgres(ctx, postgresDSNWithSearchPath(t, dsn, schema))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	store.DB().SetMaxOpenConns(4)
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	repo := store.Executions()
	if _, err := repo.Admit(ctx, ExecutionAdmission{Execution: TaskExecution{ID: "pg-concurrent", TaskID: "task", CorrelationID: "corr", IdempotencyKey: "pg-concurrent", Mode: "direct", PlanVersion: "v1", PlanDigest: "plan", CapabilityDigest: "cap", ProjectionDigest: "projection"}, Plan: ExecutionPlanRecord{ExecutionID: "pg-concurrent", Version: "v1", Mode: "direct", PlanDigest: "plan", CapabilityDigest: "cap", ProjectionDigest: "projection"}}); err != nil {
		t.Fatal(err)
	}
	positions := make(chan int64, 2)
	failures := make(chan error, 2)
	for _, eventID := range []string{"event-a", "event-b"} {
		go func(eventID string) {
			evidence, err := repo.ProjectNativeEvidence(ctx, ExecutionEvidenceRecord{ExecutionID: "pg-concurrent", ObservationScope: "scope", PlanVersion: "v1", ContractVersion: NativeEvidenceContractV1, NativeSourceKind: "runner", NativeSourceNamespace: "history-v1", NativeSourceID: "run-1", NativeEventID: eventID, NativeVersion: "event.v1", Status: []byte(eventID)}, ExecutionSourceCursor{ExecutionID: "pg-concurrent", SourceKind: "runner", SourceNamespace: "history-v1", SourceID: "run-1", CursorVersion: "cursor.v1", CursorValue: eventID, ContractVersion: NativeEvidenceContractV1})
			positions <- evidence.ObservationPosition
			failures <- err
		}(eventID)
	}
	seen := map[int64]bool{}
	for range 2 {
		if err := <-failures; err != nil {
			t.Fatalf("concurrent projection: %v", err)
		}
		position := <-positions
		if position == 0 || seen[position] {
			t.Fatalf("invalid duplicate observation position %d", position)
		}
		seen[position] = true
	}
}

func TestPostgresNativeCheckpointPauseContract(t *testing.T) {
	dsn := os.Getenv("DANNY_TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("DANNY_TEST_POSTGRES_URL is not configured")
	}
	ctx := context.Background()
	store, err := OpenPostgres(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	schema := "danny_native_pause_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := store.DB().ExecContext(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = store.DB().ExecContext(ctx, `DROP SCHEMA `+schema+` CASCADE`) }()
	if _, err := store.DB().ExecContext(ctx, `SET search_path TO `+schema); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	repo := store.Executions()
	run, err := repo.Admit(ctx, ExecutionAdmission{Execution: TaskExecution{ID: "pg-pause", TaskID: "task", TaskRevision: 4, CorrelationID: "corr", IdempotencyKey: "pg-pause", Mode: "direct", PlanVersion: "v1", PlanDigest: "plan", CapabilityDigest: "cap", ProjectionDigest: "projection"}, Plan: ExecutionPlanRecord{ExecutionID: "pg-pause", Version: "v1", Mode: "direct", PlanDigest: "plan", CapabilityDigest: "cap", ProjectionDigest: "projection"}})
	if err != nil {
		t.Fatal(err)
	}
	checkpoint := ExecutionCheckpoint{ExecutionID: "pg-pause", RunID: "native-run", PlanVersion: "v1", PlanDigest: "plan", CapabilityDigest: "cap", ProjectionDigest: "projection", PauseReason: []byte("approval_required"), ContractVersion: NativeCheckpointContract, NativeCheckpointID: "checkpoint-1", NativeCheckpointVersion: "checkpoint.v1", NativeCheckpointDigest: "digest-1", TaskRevision: 4, WriteScope: "task:4", EffectNamespace: "task:pg-pause", SourceKind: "runner", SourceNamespace: "history-v1", SourceID: "native-run", SourceCursor: "cursor-1"}
	binding := ExecutionAuthorityBinding{ExecutionID: "pg-pause", ContractVersion: NativeCheckpointContract, TaskRevision: 4, PlanVersion: "v1", PlanDigest: "plan", CapabilityDigest: "cap", ProjectionDigest: "projection", WriteScope: "task:4", EffectNamespace: "task:pg-pause", NativeCheckpointID: "checkpoint-1", NativeCheckpointVersion: "checkpoint.v1", NativeCheckpointDigest: "digest-1", SourceKind: "runner", SourceNamespace: "history-v1", SourceID: "native-run", SourceCursor: "cursor-1"}
	paused, err := repo.SaveNativeCheckpointAndPause(ctx, checkpoint, binding, run.Revision)
	if err != nil || paused.Status != ExecutionPaused || paused.Revision != 2 {
		t.Fatalf("paused=%#v err=%v", paused, err)
	}
	if _, err := repo.SaveNativeCheckpointAndPause(ctx, checkpoint, binding, run.Revision); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale worker error=%v", err)
	}
	store.SetFailureInjector(func(point string) error {
		if point == "native_checkpoint_before_commit" {
			return errors.New("injected rollback")
		}
		return nil
	})
	second := checkpoint
	second.NativeCheckpointID = "checkpoint-2"
	second.NativeCheckpointDigest = "digest-2"
	secondBinding := binding
	secondBinding.NativeCheckpointID = "checkpoint-2"
	secondBinding.NativeCheckpointDigest = "digest-2"
	if _, err := repo.SaveNativeCheckpointAndPause(ctx, second, secondBinding, paused.Revision); err == nil {
		t.Fatal("rollback injection did not fire")
	}
	store.SetFailureInjector(nil)
	latest, err := repo.GetExecution(ctx, "pg-pause")
	if err != nil || latest.Revision != paused.Revision || latest.Status != ExecutionPaused {
		t.Fatalf("execution after rollback=%#v err=%v", latest, err)
	}
	stored, err := repo.GetCheckpoint(ctx, "pg-pause")
	if err != nil || stored.NativeCheckpointID != "checkpoint-1" {
		t.Fatalf("checkpoint after rollback=%#v err=%v", stored, err)
	}
}

func TestPostgresConditionalExecutionCommitChecksTaskRevision(t *testing.T) {
	dsn := os.Getenv("DANNY_TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("DANNY_TEST_POSTGRES_URL is not configured")
	}
	ctx := context.Background()
	store, err := OpenPostgres(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	store.DB().SetMaxOpenConns(1)
	schema := "danny_conditional_commit_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := store.DB().ExecContext(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = store.DB().ExecContext(ctx, `DROP SCHEMA `+schema+` CASCADE`) }()
	if _, err := store.DB().ExecContext(ctx, `SET search_path TO `+schema); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	task, err := store.Tasks().Create(ctx, Task{ID: "conditional-task", Title: "original"})
	if err != nil {
		t.Fatal(err)
	}
	repo := store.Executions()
	run, err := repo.AdmitIfTaskRevisionCurrent(ctx, ExecutionAdmission{Execution: TaskExecution{ID: "conditional-run", TaskID: task.ID, TaskRevision: task.Revision, CorrelationID: "conditional-correlation", IdempotencyKey: "conditional-once", Mode: "direct", PlanVersion: "v1", PlanDigest: "plan", CapabilityDigest: "cap", ProjectionDigest: "projection"}, Plan: ExecutionPlanRecord{ExecutionID: "conditional-run", Version: "v1", Mode: "direct", PlanDigest: "plan", CapabilityDigest: "cap", ProjectionDigest: "projection"}})
	if err != nil {
		t.Fatal(err)
	}
	title := "updated"
	updatedTask, err := store.Tasks().Update(ctx, task.ID, task.Revision, TaskPatch{Title: &title})
	if err != nil {
		t.Fatal(err)
	}
	bound, err := repo.BindExecutionTaskRevision(ctx, run.ID, run.Revision, updatedTask.Revision)
	if err != nil || bound.TaskRevision != updatedTask.Revision || bound.Revision != run.Revision+1 {
		t.Fatalf("bound execution=%#v err=%v", bound, err)
	}
	dispatching, err := repo.UpdateExecution(ctx, bound.ID, bound.Revision, ExecutionDispatching, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	running, err := repo.UpdateExecution(ctx, dispatching.ID, dispatching.Revision, ExecutionRunning, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	latestTitle := "advanced again"
	if _, err := store.Tasks().Update(ctx, task.ID, updatedTask.Revision, TaskPatch{Title: &latestTitle}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CommitExecutionIfTaskRevisionCurrent(ctx, running.ID, running.Revision, updatedTask.Revision, ExecutionSucceeded, []byte("native result"), nil); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("conditional terminal commit error=%v", err)
	}
	current, err := repo.GetExecution(ctx, running.ID)
	if err != nil || current.Status != ExecutionRunning || current.Revision != running.Revision {
		t.Fatalf("execution after rejected terminal commit=%#v err=%v", current, err)
	}
}

func postgresDSNWithSearchPath(t *testing.T, dsn, schema string) string {
	t.Helper()
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse postgres dsn: %v", err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func TestPostgresScheduleAndDeliveryContract(t *testing.T) {
	dsn := os.Getenv("DANNY_TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("DANNY_TEST_POSTGRES_URL is not configured")
	}
	ctx := context.Background()
	store, err := OpenPostgres(ctx, dsn)
	if err != nil {
		t.Fatalf("OpenPostgres() error = %v", err)
	}
	defer func() { _ = store.Close() }()
	schema := "danny_schedule_contract_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := store.DB().ExecContext(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatalf("create contract schema: %v", err)
	}
	defer func() { _, _ = store.DB().ExecContext(ctx, `DROP SCHEMA `+schema+` CASCADE`) }()
	if _, err := store.DB().ExecContext(ctx, `SET search_path TO `+schema); err != nil {
		t.Fatalf("set contract schema: %v", err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	schedule, err := store.Schedules().CreateSchedule(ctx, Schedule{ID: "schedule-" + uuid.NewString(), Cron: "*/5 * * * *", Timezone: "UTC", ActionType: ScheduleActionAgent, MisfirePolicy: MisfireSkip, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	occurrence, err := store.Schedules().AdmitOccurrence(ctx, ScheduleOccurrence{ScheduleID: schedule.ID, ScheduleRevision: schedule.Revision, OccurrenceKey: "occurrence-1", Timezone: "UTC", TzdataVersion: "test", DSTDecision: DSTNormal, IdempotencyKey: schedule.ID + ":occurrence-1"}, OutboxMessage{Kind: OutboxKindSchedule, IdempotencyKey: schedule.ID + ":occurrence-1", Payload: []byte("payload")})
	if err != nil {
		t.Fatal(err)
	}
	replay, err := store.Schedules().AdmitOccurrence(ctx, ScheduleOccurrence{ScheduleID: schedule.ID, ScheduleRevision: schedule.Revision, OccurrenceKey: "occurrence-1", IdempotencyKey: occurrence.IdempotencyKey}, OutboxMessage{Kind: OutboxKindSchedule, IdempotencyKey: occurrence.IdempotencyKey})
	if err != nil || replay.ID != occurrence.ID {
		t.Fatalf("occurrence replay = %#v, err=%v", replay, err)
	}
	delivery, err := store.Webhooks().CreateDelivery(ctx, WebhookDelivery{EffectKey: schedule.ID + ":webhook", URL: "http://127.0.0.1/webhook", Payload: []byte("payload")})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := store.Webhooks().ClaimDelivery(ctx, delivery.ID, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Webhooks().MarkDelivered(ctx, claimed.ID, claimed.Owner, claimed.Revision); err != nil {
		t.Fatal(err)
	}
}
