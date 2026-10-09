package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/FelixSeptem/danny/internal/parser"
	"github.com/google/uuid"
)

type parserJobRepository struct{ store *Store }

func (r *parserJobRepository) Create(ctx context.Context, job ParserJob) (ParserJob, error) {
	if job.ID == "" {
		job.ID = uuid.NewString()
	}
	if job.ContractVersion == "" {
		job.ContractVersion = parser.ContractVersion
	}
	if job.Status == "" {
		job.Status = string(parser.StatusAccepted)
	}
	if job.CleanupState == "" {
		job.CleanupState = "pending"
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
	now := time.Now().UTC()
	job.CreatedAt, job.UpdatedAt = now, now
	_, err := r.store.db.ExecContext(ctx, r.store.bind(`INSERT INTO parser_jobs(id,contract_version,idempotency_key,staging_object_id,source_fingerprint,content_hash,filename,format_hint,policy,status,attempt_count,result,failure,parser_version,provider_mode,cleanup_state,owner,lease_until,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`), job.ID, job.ContractVersion, job.IdempotencyKey, job.StagingObjectID, job.SourceFingerprint, job.ContentHash, job.Filename, job.FormatHint, job.Policy, job.Status, job.AttemptCount, job.Result, job.Failure, job.ParserVersion, job.ProviderMode, job.CleanupState, job.Owner, nullableTime(job.LeaseUntil), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		if existing, lookupErr := r.GetByIdempotencyKey(ctx, job.IdempotencyKey); lookupErr == nil {
			if existing.SourceFingerprint != job.SourceFingerprint || existing.ContentHash != job.ContentHash {
				return ParserJob{}, fmt.Errorf("%w: idempotency key belongs to another source", parser.ErrConflict)
			}
			return existing, nil
		}
		return ParserJob{}, fmt.Errorf("create parser job: %w", err)
	}
	return job, nil
}

func (r *parserJobRepository) Get(ctx context.Context, id string) (ParserJob, error) {
	return r.get(ctx, `WHERE id=?`, id)
}

func (r *parserJobRepository) GetByIdempotencyKey(ctx context.Context, key string) (ParserJob, error) {
	return r.get(ctx, `WHERE idempotency_key=?`, key)
}

func (r *parserJobRepository) get(ctx context.Context, predicate string, arg string) (ParserJob, error) {
	var job ParserJob
	var lease sql.NullString
	var created, updated string
	err := r.store.db.QueryRowContext(ctx, r.store.bind(`SELECT id,contract_version,idempotency_key,staging_object_id,source_fingerprint,content_hash,filename,format_hint,policy,status,attempt_count,result,failure,parser_version,provider_mode,cleanup_state,owner,lease_until,created_at,updated_at FROM parser_jobs `+predicate), arg).Scan(&job.ID, &job.ContractVersion, &job.IdempotencyKey, &job.StagingObjectID, &job.SourceFingerprint, &job.ContentHash, &job.Filename, &job.FormatHint, &job.Policy, &job.Status, &job.AttemptCount, &job.Result, &job.Failure, &job.ParserVersion, &job.ProviderMode, &job.CleanupState, &job.Owner, &lease, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return ParserJob{}, ErrNotFound
	}
	if err != nil {
		return ParserJob{}, fmt.Errorf("get parser job: %w", err)
	}
	job.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	job.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	if lease.Valid {
		value, parseErr := time.Parse(time.RFC3339Nano, lease.String)
		if parseErr == nil {
			job.LeaseUntil = &value
		}
	}
	return job, nil
}

func (r *parserJobRepository) Claim(ctx context.Context, owner string, lease time.Duration) (ParserJob, error) {
	tx, err := r.store.db.BeginTx(ctx, nil)
	if err != nil {
		return ParserJob{}, err
	}
	defer func() { _ = tx.Rollback() }()
	now := time.Now().UTC()
	var job ParserJob
	var leaseValue sql.NullString
	var created, updated string
	err = tx.QueryRowContext(ctx, r.store.bind(`SELECT id,contract_version,idempotency_key,staging_object_id,source_fingerprint,content_hash,filename,format_hint,policy,status,attempt_count,result,failure,parser_version,provider_mode,cleanup_state,owner,lease_until,created_at,updated_at FROM parser_jobs WHERE (status=? OR (status=? AND lease_until<=?)) ORDER BY created_at LIMIT 1`), parser.StatusAccepted, parser.StatusRunning, now.Format(time.RFC3339Nano)).Scan(&job.ID, &job.ContractVersion, &job.IdempotencyKey, &job.StagingObjectID, &job.SourceFingerprint, &job.ContentHash, &job.Filename, &job.FormatHint, &job.Policy, &job.Status, &job.AttemptCount, &job.Result, &job.Failure, &job.ParserVersion, &job.ProviderMode, &job.CleanupState, &job.Owner, &leaseValue, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return ParserJob{}, ErrNotFound
	}
	if err != nil {
		return ParserJob{}, err
	}
	until := now.Add(lease)
	result, err := tx.ExecContext(ctx, r.store.bind(`UPDATE parser_jobs SET status=?,owner=?,lease_until=?,attempt_count=attempt_count+1,updated_at=? WHERE id=? AND (status=? OR (status=? AND lease_until<=?))`), parser.StatusRunning, owner, until.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), job.ID, parser.StatusAccepted, parser.StatusRunning, now.Format(time.RFC3339Nano))
	if err != nil {
		return ParserJob{}, err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return ParserJob{}, ErrLeaseLost
	}
	if err := tx.Commit(); err != nil {
		return ParserJob{}, err
	}
	job.Status = string(parser.StatusRunning)
	job.Owner = owner
	job.LeaseUntil = &until
	job.AttemptCount++
	job.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	job.UpdatedAt = now
	return job, nil
}

func (r *parserJobRepository) SaveResult(ctx context.Context, id, owner string, result, failure []byte, status, providerMode string) (ParserJob, error) {
	if status == "" {
		return ParserJob{}, fmt.Errorf("save parser result: empty status")
	}
	if result == nil {
		result = []byte{}
	}
	if failure == nil {
		failure = []byte{}
	}
	now := time.Now().UTC()
	res, err := r.store.db.ExecContext(ctx, r.store.bind(`UPDATE parser_jobs SET status=?,result=?,failure=?,provider_mode=?,owner='',lease_until=NULL,cleanup_state='complete',updated_at=? WHERE id=? AND status=? AND owner=?`), status, result, failure, providerMode, now.Format(time.RFC3339Nano), id, parser.StatusRunning, owner)
	if err != nil {
		return ParserJob{}, err
	}
	if rows, _ := res.RowsAffected(); rows != 1 {
		return ParserJob{}, ErrLeaseLost
	}
	return r.Get(ctx, id)
}

func (r *parserJobRepository) RequestCancel(ctx context.Context, id string) error {
	result, err := r.store.db.ExecContext(ctx, r.store.bind(`UPDATE parser_jobs SET status=?,updated_at=? WHERE id=? AND status IN (?,?)`), parser.StatusCancelled, time.Now().UTC().Format(time.RFC3339Nano), id, parser.StatusAccepted, parser.StatusRunning)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		if _, getErr := r.Get(ctx, id); getErr != nil {
			return getErr
		}
		return ErrInvalidTransition
	}
	return nil
}
