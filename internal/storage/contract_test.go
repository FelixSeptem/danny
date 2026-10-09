package storage

import (
	"context"
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
