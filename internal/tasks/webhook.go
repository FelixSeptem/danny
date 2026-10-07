package tasks

import (
	"context"
	"fmt"
	"time"

	"github.com/FelixSeptem/danny/internal/storage"
)

type WebhookSender interface {
	DeliverWebhook(context.Context, string, []byte) error
}

type WebhookWorker struct {
	Repository  storage.WebhookRepository
	Sender      WebhookSender
	MaxAttempts int64
	Backoff     func(int64) time.Duration
}

func (w WebhookWorker) RunOnce(ctx context.Context, deliveryID string, lease time.Duration) (storage.WebhookDelivery, error) {
	if w.Repository == nil || w.Sender == nil || deliveryID == "" {
		return storage.WebhookDelivery{}, fmt.Errorf("tasks: webhook worker is not configured")
	}
	delivery, err := w.Repository.ClaimDelivery(ctx, deliveryID, lease)
	if err != nil {
		return storage.WebhookDelivery{}, err
	}
	if err := w.Sender.DeliverWebhook(ctx, delivery.URL, delivery.Payload); err != nil {
		maxAttempts := w.MaxAttempts
		if maxAttempts <= 0 {
			maxAttempts = 3
		}
		if delivery.AttemptCount >= maxAttempts {
			if failErr := w.Repository.FailDelivery(ctx, delivery.ID, delivery.Owner, delivery.Revision, err.Error()); failErr != nil {
				return storage.WebhookDelivery{}, failErr
			}
			return delivery, err
		}
		backoff := time.Duration(delivery.AttemptCount) * time.Second
		if w.Backoff != nil {
			backoff = w.Backoff(delivery.AttemptCount)
		}
		if retryErr := w.Repository.RetryDelivery(ctx, delivery.ID, delivery.Owner, delivery.Revision, time.Now().UTC().Add(backoff), err.Error()); retryErr != nil {
			return storage.WebhookDelivery{}, retryErr
		}
		return delivery, err
	}
	if err := w.Repository.MarkDelivered(ctx, delivery.ID, delivery.Owner, delivery.Revision); err != nil {
		return storage.WebhookDelivery{}, err
	}
	delivery.Status = storage.DeliveryDelivered
	return delivery, nil
}
