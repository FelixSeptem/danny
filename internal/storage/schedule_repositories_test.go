package storage

import (
	"context"
	"errors"
	"testing"
	"time"
)

func scheduleTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := OpenSQLite(context.Background(), "file:schedule-contract-"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestScheduleValidationAndRevisionConflict(t *testing.T) {
	store := scheduleTestStore(t)
	ctx := context.Background()
	_, err := store.Schedules().CreateSchedule(ctx, Schedule{ID: "invalid", Cron: "not cron", Timezone: "UTC"})
	if !errors.Is(err, ErrInvalidSchedule) {
		t.Fatalf("invalid cron error = %v, want ErrInvalidSchedule", err)
	}
	created, err := store.Schedules().CreateSchedule(ctx, Schedule{ID: "schedule-1", Cron: "*/5 * * * *", Timezone: "UTC", ActionType: ScheduleActionAgent, MisfirePolicy: MisfireSkip, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	name := "updated"
	updated, err := store.Schedules().UpdateSchedule(ctx, created.ID, created.Revision, SchedulePatch{Instruction: &name})
	if err != nil || updated.Revision != 2 {
		t.Fatalf("update = %#v, err=%v", updated, err)
	}
	_, err = store.Schedules().UpdateSchedule(ctx, created.ID, created.Revision, SchedulePatch{Instruction: &name})
	if !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale update error = %v, want ErrRevisionConflict", err)
	}
}

func TestAdmitOccurrenceCommitsOutboxAndReplaysIdentity(t *testing.T) {
	store := scheduleTestStore(t)
	ctx := context.Background()
	o := ScheduleOccurrence{ID: "occ-1", ScheduleID: "schedule-1", ScheduleRevision: 1, OccurrenceKey: "schedule-1:2026-10-07T09:00:00Z", Timezone: "UTC", TzdataVersion: "test", DSTDecision: DSTNormal, Status: OccurrenceAdmitted, IdempotencyKey: "occ-idempotency"}
	first, err := store.Schedules().AdmitOccurrence(ctx, o, OutboxMessage{Kind: OutboxKindSchedule, IdempotencyKey: o.IdempotencyKey, Payload: []byte(`{"occurrence":"occ-1"}`)})
	if err != nil {
		t.Fatal(err)
	}
	replay, err := store.Schedules().AdmitOccurrence(ctx, o, OutboxMessage{Kind: OutboxKindSchedule, IdempotencyKey: o.IdempotencyKey, Payload: []byte(`{"occurrence":"occ-1"}`)})
	if err != nil || replay.ID != first.ID {
		t.Fatalf("replay = %#v, err=%v", replay, err)
	}
	if _, err := store.Outbox().Claim(ctx, OutboxKindSchedule, "worker", time.Minute); err != nil {
		t.Fatalf("schedule outbox missing: %v", err)
	}
	conflict := o
	conflict.IdempotencyKey = "other"
	if _, err := store.Schedules().AdmitOccurrence(ctx, conflict, OutboxMessage{Kind: OutboxKindSchedule, IdempotencyKey: "other"}); !errors.Is(err, ErrOccurrenceConflict) {
		t.Fatalf("conflicting replay error = %v, want ErrOccurrenceConflict", err)
	}
}

func TestAdmitOccurrenceRollbackLeavesNoRows(t *testing.T) {
	store := scheduleTestStore(t)
	ctx := context.Background()
	store.SetFailureInjector(func(point string) error {
		if point == "state_outbox_commit" {
			return errors.New("crash")
		}
		return nil
	})
	o := ScheduleOccurrence{ID: "occ-rollback", ScheduleID: "schedule-1", ScheduleRevision: 1, OccurrenceKey: "key", Timezone: "UTC", TzdataVersion: "test", DSTDecision: DSTNormal, IdempotencyKey: "rollback"}
	if _, err := store.Schedules().AdmitOccurrence(ctx, o, OutboxMessage{Kind: OutboxKindSchedule, IdempotencyKey: o.IdempotencyKey}); err == nil {
		t.Fatal("expected transaction failure")
	}
	if _, err := store.Schedules().GetOccurrence(ctx, o.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("occurrence after rollback = %v", err)
	}
	if _, err := store.Outbox().Claim(ctx, OutboxKindSchedule, "worker", time.Minute); !errors.Is(err, ErrNotFound) {
		t.Fatalf("outbox after rollback = %v", err)
	}
}

func TestOccurrenceTransitionsAndConditionalRevision(t *testing.T) {
	store := scheduleTestStore(t)
	ctx := context.Background()
	o, err := store.Schedules().AdmitOccurrence(ctx, ScheduleOccurrence{ID: "occ-transition", ScheduleID: "s", ScheduleRevision: 1, OccurrenceKey: "k", Timezone: "UTC", TzdataVersion: "test", DSTDecision: DSTNormal, IdempotencyKey: "transition"}, OutboxMessage{Kind: OutboxKindSchedule, IdempotencyKey: "transition"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Schedules().UpdateOccurrence(ctx, o.ID, o.Revision, OccurrencePatch{Status: OccurrenceSucceeded}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("invalid transition error = %v", err)
	}
	updated, err := store.Schedules().UpdateOccurrence(ctx, o.ID, o.Revision, OccurrencePatch{Status: OccurrenceDispatching})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Schedules().UpdateOccurrence(ctx, o.ID, o.Revision, OccurrencePatch{Status: OccurrenceDispatched}); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale occurrence error = %v", err)
	}
	if updated.Status != OccurrenceDispatching {
		t.Fatalf("status = %s", updated.Status)
	}
}

func TestWebhookDeliveryLeaseFencingAndIndependentTerminalStatus(t *testing.T) {
	store := scheduleTestStore(t)
	ctx := context.Background()
	execution, err := store.Executions().Admit(ctx, ExecutionAdmission{Execution: TaskExecution{ID: "execution-webhook", TaskID: "task", Status: ExecutionAdmitted, Mode: "direct", PlanVersion: "plan", PlanDigest: "digest", CapabilityDigest: "cap", ProjectionDigest: "projection", CorrelationID: "correlation", IdempotencyKey: "execution-webhook"}, Plan: ExecutionPlanRecord{ExecutionID: "execution-webhook", Version: "plan", Mode: "direct", PlanDigest: "digest", CapabilityDigest: "cap", ProjectionDigest: "projection"}})
	if err != nil {
		t.Fatal(err)
	}
	delivery, err := store.Webhooks().CreateDelivery(ctx, WebhookDelivery{ExecutionID: execution.ID, EffectKey: "execution-webhook:webhook", URL: "http://127.0.0.1/webhook", Payload: []byte("payload")})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := store.Webhooks().ClaimDelivery(ctx, delivery.ID, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Webhooks().MarkDelivered(ctx, claimed.ID, claimed.Owner+"-stale", claimed.Revision); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("stale delivery commit = %v", err)
	}
	if err := store.Webhooks().MarkDelivered(ctx, claimed.ID, claimed.Owner, claimed.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Webhooks().ClaimDelivery(ctx, delivery.ID, time.Minute); !errors.Is(err, ErrDeliveryConflict) {
		t.Fatalf("completed delivery claim = %v", err)
	}
}

func TestTerminalResultAndWebhookIntentAreAtomic(t *testing.T) {
	store := scheduleTestStore(t)
	ctx := context.Background()
	execution, err := store.Executions().Admit(ctx, ExecutionAdmission{Execution: TaskExecution{ID: "execution-atomic", TaskID: "task", Status: ExecutionAdmitted, Mode: "direct", PlanVersion: "plan", PlanDigest: "digest", CapabilityDigest: "cap", ProjectionDigest: "projection", CorrelationID: "correlation", IdempotencyKey: "execution-atomic"}, Plan: ExecutionPlanRecord{ExecutionID: "execution-atomic", Version: "plan", Mode: "direct", PlanDigest: "digest", CapabilityDigest: "cap", ProjectionDigest: "projection"}})
	if err != nil {
		t.Fatal(err)
	}
	store.SetFailureInjector(func(point string) error {
		if point == "state_outbox_commit" {
			return errors.New("crash before commit")
		}
		return nil
	})
	_, _, err = store.TerminalDeliveries().CommitTerminalWithDelivery(ctx, execution.ID, execution.Revision, ExecutionSucceeded, []byte("result"), nil, WebhookDelivery{ExecutionID: execution.ID, EffectKey: "execution-atomic:webhook", URL: "http://127.0.0.1/webhook", Payload: []byte("payload")})
	if err == nil {
		t.Fatal("expected atomic commit failure")
	}
	current, err := store.Executions().GetExecution(ctx, execution.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != ExecutionAdmitted {
		t.Fatalf("execution status after rollback = %s", current.Status)
	}
	if _, err := store.Webhooks().GetDelivery(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delivery after rollback = %v", err)
	}
}
