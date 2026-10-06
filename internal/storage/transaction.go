package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type Transaction struct {
	tx    *sql.Tx
	store *Store
}

func (s *Store) WithTransaction(ctx context.Context, fn func(*Transaction) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin storage transaction: %w", err)
	}
	transaction := &Transaction{tx: tx, store: s}
	if err := fn(transaction); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := s.fail("state_outbox_commit"); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit storage transaction: %w", err)
	}
	return nil
}

func (t *Transaction) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return t.tx.ExecContext(ctx, t.store.bind(query), args...)
}

func (t *Transaction) EnqueueOutbox(ctx context.Context, message OutboxMessage) error {
	if message.ID == "" {
		message.ID = uuid.NewString()
	}
	if message.Status == "" {
		message.Status = "pending"
	}
	if message.PayloadVersion == 0 {
		message.PayloadVersion = 1
	}
	now := time.Now().UTC()
	if message.AvailableAt.IsZero() {
		message.AvailableAt = now
	}
	_, err := t.tx.ExecContext(ctx, t.store.bind(`INSERT INTO outboxes(id,kind,idempotency_key,payload_version,payload,status,attempts,owner,lease_until,available_at,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`), message.ID, message.Kind, message.IdempotencyKey, message.PayloadVersion, message.Payload, message.Status, message.Attempts, message.Owner, nullableTime(message.LeaseUntil), message.AvailableAt.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	return err
}
