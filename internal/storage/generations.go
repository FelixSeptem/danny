package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
)

type generationRepository struct{ store *Store }

func (r *generationRepository) Begin(ctx context.Context) (Generation, error) {
	tx, err := r.store.db.BeginTx(ctx, nil)
	if err != nil {
		return Generation{}, err
	}
	defer tx.Rollback()
	var id int64
	if err := tx.QueryRowContext(ctx, r.store.bind(`SELECT COALESCE(MAX(id), 0)+1 FROM knowledge_generations`)).Scan(&id); err != nil {
		return Generation{}, err
	}
	now := time.Now().UTC()
	if _, err := tx.ExecContext(ctx, r.store.bind(`INSERT INTO knowledge_generations(id,status,commit_marker,created_at,committed_at) VALUES(?,?,?,?,NULL)`), id, GenerationStaged, nil, now.Format(time.RFC3339Nano)); err != nil {
		return Generation{}, err
	}
	if err := tx.Commit(); err != nil {
		return Generation{}, err
	}
	return Generation{ID: id, Status: GenerationStaged, CreatedAt: now}, nil
}

func (r *generationRepository) AddArtifact(ctx context.Context, generationID int64, artifact Artifact) error {
	if artifact.ID == "" {
		artifact.ID = uuid.NewString()
	}
	if artifact.Kind == "" || artifact.ContentHash == "" {
		return fmt.Errorf("artifact kind and content hash are required")
	}
	if artifact.Lineage == nil {
		artifact.Lineage = []byte{}
	}
	result, err := r.store.db.ExecContext(ctx, r.store.bind(`INSERT INTO artifacts(id,generation_id,kind,content_hash,content,lineage,created_at) SELECT ?,?,?,?,?,?,? WHERE EXISTS(SELECT 1 FROM knowledge_generations WHERE id=? AND status=?)`), artifact.ID, generationID, artifact.Kind, artifact.ContentHash, artifact.Content, artifact.Lineage, time.Now().UTC().Format(time.RFC3339Nano), generationID, GenerationStaged)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return fmt.Errorf("add artifact: generation %d is not staged", generationID)
	}
	return nil
}

func (r *generationRepository) Commit(ctx context.Context, generationID int64, manifest []byte) error {
	tx, err := r.store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC()
	if _, err := tx.ExecContext(ctx, r.store.bind(`INSERT INTO manifests(generation_id,payload,created_at) VALUES(?,?,?)`), generationID, manifest, now.Format(time.RFC3339Nano)); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	marker := uuid.NewString()
	result, err := tx.ExecContext(ctx, r.store.bind(`UPDATE knowledge_generations SET status=?,commit_marker=?,committed_at=? WHERE id=? AND status=? AND commit_marker IS NULL`), GenerationCommitted, marker, now.Format(time.RFC3339Nano), generationID, GenerationStaged)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return fmt.Errorf("commit generation: %w", ErrNotFound)
	}
	if err := r.store.fail("generation_commit_marker"); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}

func (r *generationRepository) Committed(ctx context.Context) (Generation, error) {
	var result Generation
	var created string
	var committed sql.NullString
	err := r.store.db.QueryRowContext(ctx, r.store.bind(`SELECT id,status,commit_marker,created_at,committed_at FROM knowledge_generations WHERE status=? AND commit_marker IS NOT NULL ORDER BY id DESC LIMIT 1`), GenerationCommitted).Scan(&result.ID, &result.Status, &result.CommitMarker, &created, &committed)
	if errors.Is(err, sql.ErrNoRows) {
		return Generation{}, ErrNotFound
	}
	if err != nil {
		return Generation{}, err
	}
	result.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	if committed.Valid {
		value, _ := time.Parse(time.RFC3339Nano, committed.String)
		result.CommittedAt = &value
	}
	return result, nil
}

func (r *generationRepository) Materialize(ctx context.Context, generationID int64, root string) error {
	var marker string
	if err := r.store.db.QueryRowContext(ctx, r.store.bind(`SELECT commit_marker FROM knowledge_generations WHERE id=? AND status=? AND commit_marker IS NOT NULL`), generationID, GenerationCommitted).Scan(&marker); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	rows, err := r.store.db.QueryContext(ctx, r.store.bind(`SELECT id,kind,content FROM artifacts WHERE generation_id=? ORDER BY kind,id`), generationID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, kind string
		var content []byte
		if err := rows.Scan(&id, &kind, &content); err != nil {
			return err
		}
		path := filepath.Join(root, filepath.Clean(kind), id)
		if !isWithin(root, path) {
			return fmt.Errorf("invalid artifact path")
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		temp, err := os.CreateTemp(filepath.Dir(path), ".danny-materialize-*")
		if err != nil {
			return err
		}
		name := temp.Name()
		if _, err := temp.Write(content); err == nil {
			err = temp.Close()
		} else {
			temp.Close()
		}
		if err != nil {
			os.Remove(name)
			return err
		}
		if err := r.store.fail("filesystem_materialization"); err != nil {
			os.Remove(name)
			return err
		}
		if err := os.Rename(name, path); err != nil {
			os.Remove(name)
			return err
		}
	}
	return rows.Err()
}

func isWithin(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	return err == nil && rel != ".." && len(rel) > 0 && rel[0:1] != ".."+string(filepath.Separator)
}

func (s *Store) Reconcile(ctx context.Context, materializationRoot string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := s.db.ExecContext(ctx, s.bind(`UPDATE outboxes SET status='pending',owner='',lease_until=NULL,available_at=?,updated_at=? WHERE status='leased' AND lease_until<=?`), now, now, now); err != nil {
		return fmt.Errorf("reconcile expired outbox leases: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, s.bind(`SELECT id FROM knowledge_generations WHERE status=? AND commit_marker IS NOT NULL ORDER BY id`), GenerationCommitted)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		if err := s.Generations().Materialize(ctx, id, materializationRoot); err != nil {
			return err
		}
		payload := []byte(fmt.Sprintf(`{"generation_id":%d}`, id))
		if _, err := s.db.ExecContext(ctx, s.bind(`INSERT INTO outboxes(id,kind,idempotency_key,payload_version,payload,status,attempts,owner,lease_until,available_at,created_at,updated_at) SELECT ?,?,?,?,?,?,?,?,?,?,?,? WHERE NOT EXISTS (SELECT 1 FROM outboxes WHERE kind=? AND idempotency_key=?)`), uuid.NewString(), OutboxKindStele, fmt.Sprintf("generation:%d:stele", id), 1, payload, "pending", 0, "", nil, now, now, now, OutboxKindStele, fmt.Sprintf("generation:%d:stele", id)); err != nil {
			return fmt.Errorf("reconcile Stele projection intent: %w", err)
		}
	}
	return nil
}
