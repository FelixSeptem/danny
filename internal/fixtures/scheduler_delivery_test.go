package fixtures

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/FelixSeptem/danny/internal/storage"
)

func TestSQLiteScheduleCrashWindowConvergesByOccurrenceAndEffectKeys(t *testing.T) {
	ctx := context.Background()
	store, err := storage.OpenSQLite(ctx, "file:e2e-schedule-crash-window?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	schedule, err := store.Schedules().CreateSchedule(ctx, storage.Schedule{ID: "e2e-schedule", Cron: "0 9 * * *", Timezone: "UTC", ActionType: storage.ScheduleActionAgent, MisfirePolicy: storage.MisfireSkip})
	if err != nil {
		t.Fatal(err)
	}
	occurrence := storage.ScheduleOccurrence{ID: "e2e-occurrence", ScheduleID: schedule.ID, ScheduleRevision: schedule.Revision, OccurrenceKey: "e2e-occurrence-key", Timezone: "UTC", TzdataVersion: "test", DSTDecision: storage.DSTNormal, CorrelationID: "e2e-correlation", IdempotencyKey: "e2e-schedule:e2e-occurrence-key", PolicyDigest: "policy", CapabilityDigest: "capability", PlanVersion: "plan.v1", ProjectionDigest: "projection", EffectNamespace: "e2e-schedule:e2e-occurrence-key"}
	if _, err := store.Schedules().AdmitOccurrence(ctx, occurrence, storage.OutboxMessage{Kind: storage.OutboxKindSchedule, IdempotencyKey: occurrence.IdempotencyKey, Payload: []byte("command")}); err != nil {
		t.Fatal(err)
	}
	store.SetFailureInjector(func(point string) error {
		if point == "state_outbox_commit" {
			return errors.New("crash before publish")
		}
		return nil
	})
	if _, err := store.Schedules().AdmitOccurrence(ctx, occurrence, storage.OutboxMessage{Kind: storage.OutboxKindSchedule, IdempotencyKey: occurrence.IdempotencyKey, Payload: []byte("command")}); err != nil {
		t.Fatal(err)
	}
	store.SetFailureInjector(nil)
	intent, err := store.Outbox().Get(ctx, storage.OutboxKindSchedule, occurrence.IdempotencyKey)
	if err != nil || intent.IdempotencyKey != occurrence.IdempotencyKey {
		t.Fatalf("intent=%#v err=%v", intent, err)
	}
	effect, err := store.Effects().Begin(ctx, storage.EffectKey{Namespace: occurrence.EffectNamespace, Subject: occurrence.OccurrenceKey, Key: "memory"}, []byte("memory"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Effects().Complete(ctx, effect.ID, []byte("stele-result")); err != nil {
		t.Fatal(err)
	}
	replay, err := store.Effects().Begin(ctx, effect.Key, []byte("memory"))
	if err != nil || string(replay.Result) != "stele-result" {
		t.Fatalf("effect replay=%#v err=%v", replay, err)
	}
	delivery, err := store.Webhooks().CreateDelivery(ctx, storage.WebhookDelivery{OccurrenceID: occurrence.ID, EffectKey: occurrence.IdempotencyKey + ":webhook", URL: "http://127.0.0.1/webhook", Payload: []byte("terminal")})
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
	occurrence, err = store.Schedules().GetOccurrence(ctx, occurrence.ID)
	if err != nil || occurrence.OccurrenceKey != "e2e-occurrence-key" {
		t.Fatalf("occurrence=%#v err=%v", occurrence, err)
	}
}
