package tasks

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/FelixSeptem/danny/internal/storage"
)

type fakeWebhookSender struct {
	calls int
	fail  bool
}

func (s *fakeWebhookSender) DeliverWebhook(context.Context, string, []byte) error {
	s.calls++
	if s.fail {
		return errors.New("timeout")
	}
	return nil
}

func webhookStore(t *testing.T) *storage.Store {
	t.Helper()
	store, err := storage.OpenSQLite(context.Background(), "file:webhook-worker-"+t.Name()+"?mode=memory&cache=shared")
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

func TestWebhookWorkerRetriesWithStableDeliveryIdentity(t *testing.T) {
	store := webhookStore(t)
	ctx := context.Background()
	delivery, err := store.Webhooks().CreateDelivery(ctx, storage.WebhookDelivery{EffectKey: "occurrence:webhook", URL: "http://example", Payload: []byte("payload")})
	if err != nil {
		t.Fatal(err)
	}
	sender := &fakeWebhookSender{fail: true}
	worker := WebhookWorker{Repository: store.Webhooks(), Sender: sender, MaxAttempts: 2, Backoff: func(int64) time.Duration { return 0 }}
	_, _ = worker.RunOnce(ctx, delivery.ID, time.Minute)
	current, err := store.Webhooks().GetDelivery(ctx, delivery.ID)
	if err != nil || current.Status != storage.DeliveryRetryable {
		t.Fatalf("retry state=%#v err=%v", current, err)
	}
	sender.fail = false
	if _, err := worker.RunOnce(ctx, delivery.ID, time.Minute); err != nil {
		t.Fatal(err)
	}
	current, err = store.Webhooks().GetDelivery(ctx, delivery.ID)
	if err != nil || current.Status != storage.DeliveryDelivered || current.EffectKey != "occurrence:webhook" {
		t.Fatalf("delivery state=%#v err=%v", current, err)
	}
}

func TestWebhookWorkerTerminalFailureDoesNotChangeAgentStatus(t *testing.T) {
	store := webhookStore(t)
	ctx := context.Background()
	delivery, err := store.Webhooks().CreateDelivery(ctx, storage.WebhookDelivery{ExecutionID: "execution", EffectKey: "occurrence:webhook:failed", URL: "http://example", Payload: []byte("payload")})
	if err != nil {
		t.Fatal(err)
	}
	worker := WebhookWorker{Repository: store.Webhooks(), Sender: &fakeWebhookSender{fail: true}, MaxAttempts: 1, Backoff: func(int64) time.Duration { return 0 }}
	if _, err := worker.RunOnce(ctx, delivery.ID, time.Minute); err == nil {
		t.Fatal("expected delivery error")
	}
	current, err := store.Webhooks().GetDelivery(ctx, delivery.ID)
	if err != nil || current.Status != storage.DeliveryFailed {
		t.Fatalf("delivery status=%#v err=%v", current, err)
	}
}
