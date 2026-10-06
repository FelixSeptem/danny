package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type taskRepository struct{ store *Store }

func (r *taskRepository) Create(ctx context.Context, task Task) (Task, error) {
	if task.ID == "" {
		task.ID = uuid.NewString()
	}
	if task.Status == "" {
		task.Status = "open"
	}
	task.Revision = 1
	now := time.Now().UTC()
	task.CreatedAt, task.UpdatedAt = now, now
	_, err := r.store.db.ExecContext(ctx, r.store.bind(`INSERT INTO tasks(id,title,description,status,revision,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`), task.ID, task.Title, task.Description, task.Status, task.Revision, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		return Task{}, fmt.Errorf("create task: %w", err)
	}
	return task, nil
}

func (r *taskRepository) Get(ctx context.Context, id string) (Task, error) {
	var task Task
	var created, updated string
	err := r.store.db.QueryRowContext(ctx, r.store.bind(`SELECT id,title,description,status,revision,created_at,updated_at FROM tasks WHERE id=?`), id).Scan(&task.ID, &task.Title, &task.Description, &task.Status, &task.Revision, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return Task{}, ErrNotFound
	}
	if err != nil {
		return Task{}, fmt.Errorf("get task: %w", err)
	}
	task.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	task.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return task, nil
}

func (r *taskRepository) Update(ctx context.Context, id string, expectedRevision int64, patch TaskPatch) (Task, error) {
	current, err := r.Get(ctx, id)
	if err != nil {
		return Task{}, err
	}
	title, description, status := current.Title, current.Description, current.Status
	if patch.Title != nil {
		title = *patch.Title
	}
	if patch.Description != nil {
		description = *patch.Description
	}
	if patch.Status != "" {
		status = patch.Status
	}
	now := time.Now().UTC()
	result, err := r.store.db.ExecContext(ctx, r.store.bind(`UPDATE tasks SET title=?,description=?,status=?,revision=revision+1,updated_at=? WHERE id=? AND revision=?`), title, description, status, now.Format(time.RFC3339Nano), id, expectedRevision)
	if err != nil {
		return Task{}, fmt.Errorf("update task: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows != 1 {
		return Task{}, ErrRevisionConflict
	}
	return r.Get(ctx, id)
}

type effectRepository struct{ store *Store }

func (r *effectRepository) Begin(ctx context.Context, key EffectKey, request []byte) (EffectRecord, error) {
	var existing EffectRecord
	var created, updated string
	err := r.store.db.QueryRowContext(ctx, r.store.bind(`SELECT id,namespace,subject,effect_key,status,request,result,created_at,updated_at FROM idempotency_effects WHERE namespace=? AND subject=? AND effect_key=?`), key.Namespace, key.Subject, key.Key).Scan(&existing.ID, &existing.Key.Namespace, &existing.Key.Subject, &existing.Key.Key, &existing.Status, &existing.Request, &existing.Result, &created, &updated)
	if err == nil {
		existing.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		existing.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
		return existing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return EffectRecord{}, err
	}
	now := time.Now().UTC()
	record := EffectRecord{ID: uuid.NewString(), Key: key, Status: EffectInProgress, Request: append([]byte(nil), request...), CreatedAt: now, UpdatedAt: now}
	_, err = r.store.db.ExecContext(ctx, r.store.bind(`INSERT INTO idempotency_effects(id,namespace,subject,effect_key,status,request,result,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`), record.ID, key.Namespace, key.Subject, key.Key, record.Status, request, []byte{}, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		if replay, replayErr := r.Begin(ctx, key, request); replayErr == nil {
			return replay, nil
		}
		return EffectRecord{}, fmt.Errorf("begin effect: %w", err)
	}
	return record, nil
}

func (r *effectRepository) Complete(ctx context.Context, id string, result []byte) error {
	return r.finish(ctx, id, EffectCompleted, result)
}
func (r *effectRepository) Fail(ctx context.Context, id string, result []byte) error {
	return r.finish(ctx, id, EffectFailed, result)
}
func (r *effectRepository) finish(ctx context.Context, id string, status EffectStatus, result []byte) error {
	if err := r.store.fail("effect_commit"); err != nil {
		return err
	}
	res, err := r.store.db.ExecContext(ctx, r.store.bind(`UPDATE idempotency_effects SET status=?,result=?,updated_at=? WHERE id=? AND status=?`), status, result, time.Now().UTC().Format(time.RFC3339Nano), id, EffectInProgress)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ErrEffectInProgress
	}
	return nil
}

type outboxRepository struct{ store *Store }

func (r *outboxRepository) Enqueue(ctx context.Context, message OutboxMessage) error {
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
	_, err := r.store.db.ExecContext(ctx, r.store.bind(`INSERT INTO outboxes(id,kind,idempotency_key,payload_version,payload,status,attempts,owner,lease_until,available_at,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`), message.ID, message.Kind, message.IdempotencyKey, message.PayloadVersion, message.Payload, message.Status, message.Attempts, message.Owner, nullableTime(message.LeaseUntil), message.AvailableAt.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "unique") {
		return nil
	}
	if err != nil {
		return fmt.Errorf("enqueue outbox: %w", err)
	}
	return nil
}

func (r *outboxRepository) Claim(ctx context.Context, kind OutboxKind, owner string, lease time.Duration) (OutboxMessage, error) {
	tx, err := r.store.db.BeginTx(ctx, nil)
	if err != nil {
		return OutboxMessage{}, err
	}
	defer tx.Rollback()
	now := time.Now().UTC()
	var msg OutboxMessage
	var leaseUntil sql.NullString
	var available, created, updated string
	err = tx.QueryRowContext(ctx, r.store.bind(`SELECT id,kind,idempotency_key,payload_version,payload,status,attempts,owner,lease_until,available_at,created_at,updated_at FROM outboxes WHERE kind=? AND ((status='pending' AND available_at<=?) OR (status='leased' AND lease_until<=?)) ORDER BY created_at LIMIT 1`), kind, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)).Scan(&msg.ID, &msg.Kind, &msg.IdempotencyKey, &msg.PayloadVersion, &msg.Payload, &msg.Status, &msg.Attempts, &msg.Owner, &leaseUntil, &available, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return OutboxMessage{}, ErrNotFound
	}
	if err != nil {
		return OutboxMessage{}, err
	}
	until := now.Add(lease)
	res, err := tx.ExecContext(ctx, r.store.bind(`UPDATE outboxes SET status='leased',owner=?,lease_until=?,attempts=attempts+1,updated_at=? WHERE id=? AND (status='pending' OR (status='leased' AND lease_until<=?))`), owner, until.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), msg.ID, now.Format(time.RFC3339Nano))
	if err != nil {
		return OutboxMessage{}, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return OutboxMessage{}, ErrLeaseLost
	}
	if err := tx.Commit(); err != nil {
		return OutboxMessage{}, err
	}
	msg.Owner, msg.Status, msg.LeaseUntil = owner, "leased", &until
	msg.AvailableAt, _ = time.Parse(time.RFC3339Nano, available)
	msg.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	msg.UpdatedAt = now
	return msg, nil
}

func (r *outboxRepository) Ack(ctx context.Context, id, owner string) error {
	res, err := r.store.db.ExecContext(ctx, r.store.bind(`UPDATE outboxes SET status='delivered',owner='',lease_until=NULL,updated_at=? WHERE id=? AND status='leased' AND owner=?`), time.Now().UTC().Format(time.RFC3339Nano), id, owner)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ErrLeaseLost
	}
	return nil
}

func (r *outboxRepository) Retry(ctx context.Context, id, owner string, availableAt time.Time) error {
	res, err := r.store.db.ExecContext(ctx, r.store.bind(`UPDATE outboxes SET status='pending',owner='',lease_until=NULL,available_at=?,updated_at=? WHERE id=? AND status='leased' AND owner=?`), availableAt.UTC().Format(time.RFC3339Nano), time.Now().UTC().Format(time.RFC3339Nano), id, owner)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ErrLeaseLost
	}
	return nil
}

func nullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC().Format(time.RFC3339Nano)
}
