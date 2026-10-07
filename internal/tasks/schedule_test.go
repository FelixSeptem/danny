package tasks

import (
	"context"
	"testing"
	"time"

	"github.com/FelixSeptem/danny/internal/storage"
)

func TestOccurrenceCalculatorStableNormalOccurrence(t *testing.T) {
	calc := NewOccurrenceCalculator("tzdata-test")
	schedule := storage.Schedule{ID: "schedule", Cron: "0 9 * * *", Timezone: "Asia/Shanghai", Revision: 3, MisfirePolicy: storage.MisfireSkip}
	after := time.Date(2026, 10, 7, 8, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	first, err := calc.Next(schedule, after)
	if err != nil {
		t.Fatal(err)
	}
	second, err := calc.Next(schedule, after)
	if err != nil || first.OccurrenceKey != second.OccurrenceKey || first.ResolvedAt != second.ResolvedAt {
		t.Fatalf("occurrence not stable: first=%#v second=%#v err=%v", first, second, err)
	}
	if first.TzdataVersion != "tzdata-test" || first.ResolvedAt.Hour() != 1 {
		t.Fatalf("resolved occurrence = %#v", first)
	}
}

func TestOccurrenceCalculatorDSTGapIsSkipped(t *testing.T) {
	calc := NewOccurrenceCalculator("tzdata-test")
	schedule := storage.Schedule{ID: "ny", Cron: "30 2 * * *", Timezone: "America/New_York", Revision: 1}
	after := time.Date(2026, 3, 8, 0, 0, 0, 0, time.UTC)
	occurrence, err := calc.Next(schedule, after)
	if err != nil {
		t.Fatal(err)
	}
	if occurrence.LocalTime.Day() == 8 && occurrence.LocalTime.Hour() == 2 {
		t.Fatalf("nonexistent local time was admitted: %#v", occurrence)
	}
}

func TestOccurrenceCalculatorDSTFoldUsesEarlierInstant(t *testing.T) {
	calc := NewOccurrenceCalculator("tzdata-test")
	schedule := storage.Schedule{ID: "ny", Cron: "30 1 * * *", Timezone: "America/New_York", Revision: 1}
	after := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	occurrence, err := calc.Next(schedule, after)
	if err != nil {
		t.Fatal(err)
	}
	if occurrence.DSTDecision != storage.DSTEarlierAmbiguous {
		t.Fatalf("DST decision = %s, want %s", occurrence.DSTDecision, storage.DSTEarlierAmbiguous)
	}
	// Earlier UTC instant for 01:30 America/New_York is 05:30Z (EDT).
	if occurrence.ResolvedAt.Hour() != 5 {
		t.Fatalf("resolved instant = %s, want 05:30Z", occurrence.ResolvedAt)
	}
}

func TestReconcileMisfiresIsBounded(t *testing.T) {
	calc := NewOccurrenceCalculator("tzdata-test")
	missed := []time.Time{time.Unix(1, 0), time.Unix(2, 0), time.Unix(3, 0)}
	schedule := storage.Schedule{ID: "s", Revision: 1, Timezone: "UTC", MisfirePolicy: storage.MisfireBackfill, BackfillLimit: 2}
	results, err := calc.ReconcileMisfires(schedule, missed)
	if err != nil || len(results) != 2 {
		t.Fatalf("backfill results=%d err=%v", len(results), err)
	}
	schedule.MisfirePolicy = storage.MisfireCoalesceLatest
	results, err = calc.ReconcileMisfires(schedule, missed)
	if err != nil || len(results) != 1 || results[0].Decision != storage.DSTCoalesced {
		t.Fatalf("coalesced results=%#v err=%v", results, err)
	}
}

func TestEffectKeyIsOccurrenceScoped(t *testing.T) {
	a := EffectKey("s", "o1", "memory")
	b := EffectKey("s", "o1", "memory")
	c := EffectKey("s", "o2", "memory")
	if a != b || a == c || a.Subject != "s:o1" || a.Key != "memory" {
		t.Fatalf("effect keys = %#v %#v %#v", a, b, c)
	}
}

func TestReconcileScheduleDispatchRetainsOriginalOutboxKey(t *testing.T) {
	ctx := context.Background()
	store, err := storage.OpenSQLite(ctx, "file:schedule-recovery?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	occurrence := storage.ScheduleOccurrence{ID: "o", ScheduleID: "s", OccurrenceKey: "key", IdempotencyKey: "stable-key"}
	if err := store.Outbox().Enqueue(ctx, storage.OutboxMessage{Kind: storage.OutboxKindSchedule, IdempotencyKey: occurrence.IdempotencyKey, Payload: []byte("payload")}); err != nil {
		t.Fatal(err)
	}
	claimed, err := ReconcileScheduleDispatch(ctx, store.Outbox(), occurrence, "reconciler", time.Minute)
	if err != nil || claimed.IdempotencyKey != occurrence.IdempotencyKey {
		t.Fatalf("reconciliation = %#v, err=%v", claimed, err)
	}
}
