package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type contextProjectionRepository struct{ store *Store }

func (r *contextProjectionRepository) Save(ctx context.Context, p ContextProjectionSnapshot) (ContextProjectionSnapshot, error) {
	if p.ID == "" || p.Scope == "" || p.SessionID == "" || p.TurnID == "" || p.BindingID == "" || p.RunID == "" || p.ContractVersion == "" || p.Digest == "" || p.SourceDigest == "" || len(p.Envelope) == 0 {
		return ContextProjectionSnapshot{}, fmt.Errorf("storage: invalid context projection")
	}
	var existing ContextProjectionSnapshot
	err := r.scan(r.store.db.QueryRowContext(ctx, r.store.bind(`SELECT id,scope,session_id,turn_id,binding_id,run_id,contract_version,digest,source_digest,continuity_contract_version,continuity_reference,continuity_expires_at,envelope,created_at,updated_at FROM context_projection_snapshots WHERE scope=? AND id=?`), p.Scope, p.ID), &existing)
	if err == nil {
		if existing.Digest != p.Digest || string(existing.Envelope) != string(p.Envelope) || existing.SessionID != p.SessionID || existing.TurnID != p.TurnID || existing.ContinuityContractVersion != p.ContinuityContractVersion || existing.ContinuityReference != p.ContinuityReference || existing.ContinuityExpiresAt != p.ContinuityExpiresAt {
			return ContextProjectionSnapshot{}, ErrRevisionConflict
		}
		return existing, nil
	}
	if err != sql.ErrNoRows {
		return ContextProjectionSnapshot{}, err
	}
	now := time.Now().UTC()
	_, err = r.store.db.ExecContext(ctx, r.store.bind(`INSERT INTO context_projection_snapshots(id,scope,session_id,turn_id,binding_id,run_id,contract_version,digest,source_digest,continuity_contract_version,continuity_reference,continuity_expires_at,envelope,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`), p.ID, p.Scope, p.SessionID, p.TurnID, p.BindingID, p.RunID, p.ContractVersion, p.Digest, p.SourceDigest, p.ContinuityContractVersion, p.ContinuityReference, p.ContinuityExpiresAt, p.Envelope, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		return ContextProjectionSnapshot{}, err
	}
	p.CreatedAt, p.UpdatedAt = now, now
	return p, nil
}

func (r *contextProjectionRepository) Get(ctx context.Context, scope, id string) (ContextProjectionSnapshot, error) {
	if scope == "" || id == "" {
		return ContextProjectionSnapshot{}, ErrNotFound
	}
	var p ContextProjectionSnapshot
	err := r.scan(r.store.db.QueryRowContext(ctx, r.store.bind(`SELECT id,scope,session_id,turn_id,binding_id,run_id,contract_version,digest,source_digest,continuity_contract_version,continuity_reference,continuity_expires_at,envelope,created_at,updated_at FROM context_projection_snapshots WHERE scope=? AND id=?`), scope, id), &p)
	if err == sql.ErrNoRows {
		return ContextProjectionSnapshot{}, ErrNotFound
	}
	return p, err
}

func (r *contextProjectionRepository) scan(row scanner, p *ContextProjectionSnapshot) error {
	var created, updated string
	err := row.Scan(&p.ID, &p.Scope, &p.SessionID, &p.TurnID, &p.BindingID, &p.RunID, &p.ContractVersion, &p.Digest, &p.SourceDigest, &p.ContinuityContractVersion, &p.ContinuityReference, &p.ContinuityExpiresAt, &p.Envelope, &created, &updated)
	if err == nil {
		p.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		p.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	}
	return err
}
