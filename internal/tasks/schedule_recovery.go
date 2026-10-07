package tasks

import (
	"context"
	"time"

	"github.com/FelixSeptem/danny/internal/storage"
)

// ReconcileScheduleDispatch confirms that the durable intent belonging to an
// occurrence still carries its original key. Pending or expired outbox leases
// are returned as publishable work by the existing transactional outbox.
func ReconcileScheduleDispatch(ctx context.Context, outbox storage.OutboxRepository, occurrence storage.ScheduleOccurrence, owner string, lease time.Duration) (storage.OutboxMessage, error) {
	intent, err := outbox.Get(ctx, storage.OutboxKindSchedule, occurrence.IdempotencyKey)
	if err != nil {
		return storage.OutboxMessage{}, err
	}
	if intent.IdempotencyKey != occurrence.IdempotencyKey {
		return storage.OutboxMessage{}, storage.ErrOccurrenceConflict
	}
	if intent.Status == "delivered" {
		return intent, nil
	}
	claimed, err := outbox.ClaimByKey(ctx, storage.OutboxKindSchedule, occurrence.IdempotencyKey, owner, lease)
	if err != nil {
		return storage.OutboxMessage{}, err
	}
	if claimed.IdempotencyKey != occurrence.IdempotencyKey {
		_ = outbox.Retry(ctx, claimed.ID, owner, time.Now().UTC())
		return storage.OutboxMessage{}, storage.ErrLeaseLost
	}
	return claimed, nil
}
