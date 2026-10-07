package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSQLiteStoreInitializesAndReportsSchemaVersion(t *testing.T) {
	ctx := context.Background()
	store, err := OpenSQLite(ctx, "file:storage-contract?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("OpenSQLite() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	version, err := store.SchemaVersion(ctx)
	if err != nil {
		t.Fatalf("SchemaVersion() error = %v", err)
	}
	if version != CurrentSchemaVersion {
		t.Fatalf("schema version = %d, want %d", version, CurrentSchemaVersion)
	}
}

func TestTaskRevisionRejectsStaleUpdate(t *testing.T) {
	ctx := context.Background()
	store, err := OpenSQLite(ctx, "file:revision-contract?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("OpenSQLite() error = %v", err)
	}
	defer func() { _ = store.Close() }()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	task, err := store.Tasks().Create(ctx, Task{Title: "write tests"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	updated, err := store.Tasks().Update(ctx, task.ID, task.Revision, TaskPatch{Status: "done"})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if updated.Revision != task.Revision+1 {
		t.Fatalf("revision = %d, want %d", updated.Revision, task.Revision+1)
	}
	_, err = store.Tasks().Update(ctx, task.ID, task.Revision, TaskPatch{Status: "open"})
	if !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale update error = %v, want ErrRevisionConflict", err)
	}
}

func TestEffectKeyReturnsOriginalResultOnReplay(t *testing.T) {
	ctx := context.Background()
	store, err := OpenSQLite(ctx, "file:effect-contract?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("OpenSQLite() error = %v", err)
	}
	defer func() { _ = store.Close() }()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	first, err := store.Effects().Begin(ctx, EffectKey{Namespace: "task", Subject: "task-1", Key: "occurrence-1"}, []byte(`{"task_id":"task-1"}`))
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	if err := store.Effects().Complete(ctx, first.ID, []byte(`{"status":"done"}`)); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	replay, err := store.Effects().Begin(ctx, EffectKey{Namespace: "task", Subject: "task-1", Key: "occurrence-1"}, []byte(`{"task_id":"task-1"}`))
	if err != nil {
		t.Fatalf("replay Begin() error = %v", err)
	}
	if string(replay.Result) != `{"status":"done"}` || replay.Status != EffectCompleted {
		t.Fatalf("replay = %#v, want completed original result", replay)
	}
}

func TestOutboxClaimUsesStableIdempotencyKey(t *testing.T) {
	ctx := context.Background()
	store, err := OpenSQLite(ctx, "file:outbox-contract?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("OpenSQLite() error = %v", err)
	}
	defer func() { _ = store.Close() }()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	want := OutboxMessage{Kind: OutboxKindWebhook, IdempotencyKey: "execution-1:webhook", PayloadVersion: 1, Payload: []byte(`{"ok":true}`)}
	if err := store.Outbox().Enqueue(ctx, want); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	claimed, err := store.Outbox().Claim(ctx, OutboxKindWebhook, "worker-1", time.Minute)
	if err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if claimed.IdempotencyKey != want.IdempotencyKey || claimed.Owner != "worker-1" {
		t.Fatalf("claimed = %#v, want stable key and owner", claimed)
	}
}

func TestCommittedGenerationCanRebuildDerivedMaterialization(t *testing.T) {
	ctx := context.Background()
	store, err := OpenSQLite(ctx, "file:generation-contract?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("OpenSQLite() error = %v", err)
	}
	defer func() { _ = store.Close() }()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	generation, err := store.Generations().Begin(ctx)
	if err != nil {
		t.Fatalf("Begin generation: %v", err)
	}
	artifact := Artifact{ID: "artifact-1", Kind: "summary", Content: []byte("hello"), ContentHash: "hash-1"}
	if err := store.Generations().AddArtifact(ctx, generation.ID, artifact); err != nil {
		t.Fatalf("AddArtifact: %v", err)
	}
	if err := store.Generations().Commit(ctx, generation.ID, []byte(`{"artifacts":["artifact-1"]}`)); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	committed, err := store.Generations().Committed(ctx)
	if err != nil {
		t.Fatalf("Committed: %v", err)
	}
	if committed.ID != generation.ID || committed.Status != GenerationCommitted {
		t.Fatalf("committed = %#v", committed)
	}

	dir := t.TempDir()
	if err := store.Generations().Materialize(ctx, committed.ID, dir); err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	path := filepath.Join(dir, "summary", "artifact-1")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read materialized artifact: %v", err)
	}
	if string(content) != "hello" {
		t.Fatalf("materialized content = %q", content)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove materialized artifact: %v", err)
	}
	if err := store.Reconcile(ctx, dir); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if err := store.Reconcile(ctx, dir); err != nil {
		t.Fatalf("repeat Reconcile: %v", err)
	}
	content, err = os.ReadFile(path)
	if err != nil {
		t.Fatalf("read rebuilt artifact: %v", err)
	}
	if string(content) != "hello" {
		t.Fatalf("rebuilt content = %q", content)
	}
	extra := filepath.Join(dir, "summary", "stale-artifact")
	if err := os.WriteFile(extra, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := store.Generations().Materialize(ctx, committed.ID, dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(extra); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale artifact still exists: %v", err)
	}
}

func TestLeaseFencingRejectsExpiredOwner(t *testing.T) {
	ctx := context.Background()
	store, err := OpenSQLite(ctx, "file:lease-contract?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("OpenSQLite() error = %v", err)
	}
	defer func() { _ = store.Close() }()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	first, err := store.Leases().Acquire(ctx, "outbox", "row-1", "worker-1", time.Millisecond)
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	time.Sleep(5 * time.Millisecond)
	second, err := store.Leases().Acquire(ctx, "outbox", "row-1", "worker-2", time.Minute)
	if err != nil {
		t.Fatalf("second Acquire: %v", err)
	}
	if second.Fencing <= first.Fencing {
		t.Fatalf("fencing did not increase: first=%d second=%d", first.Fencing, second.Fencing)
	}
	if err := store.Leases().Release(ctx, "outbox", "row-1", "worker-1", first.Fencing); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("stale Release error = %v, want ErrLeaseLost", err)
	}
}

func TestTransactionCommitsStateAndOutboxTogether(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, "sqlite", "file:transaction-contract?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	if err := store.WithTransaction(ctx, func(tx *Transaction) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO tasks(id,title,description,status,revision,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`, "task-tx", "transaction", "", "open", 1, time.Now().UTC().Format(time.RFC3339Nano), time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
		return tx.EnqueueOutbox(ctx, OutboxMessage{Kind: OutboxKindStele, IdempotencyKey: "task-tx:projection", Payload: []byte(`{"task":"task-tx"}`)})
	}); err != nil {
		t.Fatalf("WithTransaction() error = %v", err)
	}
	if _, err := store.Tasks().Get(ctx, "task-tx"); err != nil {
		t.Fatalf("task not committed: %v", err)
	}
	if _, err := store.Outbox().Claim(ctx, OutboxKindStele, "worker", time.Minute); err != nil {
		t.Fatalf("outbox not committed: %v", err)
	}
}

func TestOutboxAdapterBoundariesRemainExplicit(t *testing.T) {
	var _ MailboxDispatcher = mailboxDispatcherFixture{}
	var _ ContextProjector = contextProjectorFixture{}
	var _ WebhookSender = webhookSenderFixture{}
	store, err := OpenSQLite(context.Background(), "file:adapter-contract?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	if err := store.ValidateOutboxKind(OutboxKindSchedule); err != nil {
		t.Fatal(err)
	}
	if err := store.ValidateOutboxKind(OutboxKind("unknown")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown outbox kind error = %v", err)
	}
}

func TestFailureInjectorRollsBackStateOutboxBoundary(t *testing.T) {
	ctx := context.Background()
	store, err := OpenSQLite(ctx, "file:failure-contract?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	store.SetFailureInjector(func(point string) error {
		if point == "state_outbox_commit" {
			return errors.New("injected crash")
		}
		return nil
	})
	err = store.WithTransaction(ctx, func(tx *Transaction) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO tasks(id,title,description,status,revision,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`, "rolled-back", "rollback", "", "open", 1, time.Now().UTC().Format(time.RFC3339Nano), time.Now().UTC().Format(time.RFC3339Nano))
		if err != nil {
			return err
		}
		return tx.EnqueueOutbox(ctx, OutboxMessage{Kind: OutboxKindWebhook, IdempotencyKey: "rolled-back", Payload: []byte("payload")})
	})
	if err == nil {
		t.Fatal("WithTransaction() error = nil")
	}
	if _, err := store.Tasks().Get(ctx, "rolled-back"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("rolled-back task error = %v", err)
	}
	if _, err := store.Outbox().Claim(ctx, OutboxKindWebhook, "worker", time.Minute); !errors.Is(err, ErrNotFound) {
		t.Fatalf("rolled-back outbox error = %v", err)
	}
}

func TestFailureInjectorCoversGenerationLeaseEffectAndMaterialization(t *testing.T) {
	ctx := context.Background()
	store, err := OpenSQLite(ctx, "file:failure-points?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	store.SetFailureInjector(func(point string) error {
		if point == "lease_claim" {
			return errors.New("lease crash")
		}
		return nil
	})
	if _, err := store.Leases().Acquire(ctx, "failure", "lease", "worker", time.Minute); err == nil {
		t.Fatal("lease claim should fail")
	}
	store.SetFailureInjector(func(point string) error {
		if point == "effect_commit" {
			return errors.New("effect crash")
		}
		return nil
	})
	effect, err := store.Effects().Begin(ctx, EffectKey{Namespace: "failure", Subject: "effect", Key: "once"}, []byte("request"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Effects().Complete(ctx, effect.ID, []byte("result")); err == nil {
		t.Fatal("effect commit should fail")
	}
	store.SetFailureInjector(func(point string) error {
		if point == "generation_commit_marker" {
			return errors.New("generation crash")
		}
		return nil
	})
	generation, err := store.Generations().Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Generations().Commit(ctx, generation.ID, []byte(`{"artifacts":[]}`)); err == nil {
		t.Fatal("generation commit should fail")
	}
	if _, err := store.Generations().Committed(ctx); !errors.Is(err, ErrNotFound) {
		t.Fatalf("committed generation after crash = %v", err)
	}
	store.SetFailureInjector(func(point string) error {
		if point == "filesystem_materialization" {
			return errors.New("materialization crash")
		}
		return nil
	})
	generation, err = store.Generations().Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Generations().AddArtifact(ctx, generation.ID, Artifact{ID: "failure-artifact", Kind: "summary", ContentHash: "hash", Content: []byte("content")}); err != nil {
		t.Fatal(err)
	}
	if err := store.Generations().Commit(ctx, generation.ID, []byte(`{"artifacts":["failure-artifact"]}`)); err != nil {
		t.Fatal(err)
	}
	if err := store.Generations().Materialize(ctx, generation.ID, t.TempDir()); err == nil {
		t.Fatal("materialization should fail")
	}
}

type mailboxDispatcherFixture struct{}

func (mailboxDispatcherFixture) PublishMailbox(context.Context, string, []byte) error { return nil }

type contextProjectorFixture struct{}

func (contextProjectorFixture) ProjectContext(context.Context, string, []byte) error { return nil }

type webhookSenderFixture struct{}

func (webhookSenderFixture) DeliverWebhook(context.Context, string, []byte) error { return nil }
