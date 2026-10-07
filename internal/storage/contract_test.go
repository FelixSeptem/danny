package storage

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

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
	lease, err := store.Leases().Acquire(ctx, "contract", task.ID, "worker-1", time.Minute)
	if err != nil {
		t.Fatalf("Acquire lease: %v", err)
	}
	if err := store.Leases().Release(ctx, "contract", task.ID, "stale-worker", lease.Fencing); err != ErrLeaseLost {
		t.Fatalf("stale Release error = %v, want ErrLeaseLost", err)
	}
}
