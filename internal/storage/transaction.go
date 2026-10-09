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

func (t *Transaction) CreateParserJobWithSource(ctx context.Context, job ParserJob, source ParserSourceRecord) error {
	if source.ID == "" || source.Path == "" || source.ContentHash == "" {
		return ErrNotFound
	}
	if source.Content == nil {
		source.Content = []byte{}
	}
	if source.Warnings == nil {
		source.Warnings = []byte{}
	}
	if !source.UntrustedSource {
		source.UntrustedSource = true
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	untrustedSource := 0
	if source.UntrustedSource {
		untrustedSource = 1
	}
	if _, err := t.tx.ExecContext(ctx, t.store.bind(`INSERT INTO source_metadata(id,path,content_hash,content,format,parser_version,provider_mode,source_fingerprint,warnings,untrusted_source,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`), source.ID, source.Path, source.ContentHash, source.Content, source.Format, source.ParserVersion, source.ProviderMode, source.SourceFingerprint, source.Warnings, untrustedSource, now, now); err != nil {
		return fmt.Errorf("create parser source: %w", err)
	}
	if job.Policy == nil {
		job.Policy = []byte{}
	}
	if job.Result == nil {
		job.Result = []byte{}
	}
	if job.Failure == nil {
		job.Failure = []byte{}
	}
	if job.Status == "" {
		job.Status = "accepted"
	}
	if job.CleanupState == "" {
		job.CleanupState = "pending"
	}
	if _, err := t.tx.ExecContext(ctx, t.store.bind(`INSERT INTO parser_jobs(id,contract_version,idempotency_key,staging_object_id,source_fingerprint,content_hash,filename,format_hint,policy,status,attempt_count,result,failure,parser_version,provider_mode,cleanup_state,owner,lease_until,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`), job.ID, job.ContractVersion, job.IdempotencyKey, job.StagingObjectID, job.SourceFingerprint, job.ContentHash, job.Filename, job.FormatHint, job.Policy, job.Status, job.AttemptCount, job.Result, job.Failure, job.ParserVersion, job.ProviderMode, job.CleanupState, job.Owner, nullableTime(job.LeaseUntil), now, now); err != nil {
		return fmt.Errorf("create parser job: %w", err)
	}
	return nil
}
