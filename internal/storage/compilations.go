package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type SourceRecord struct {
	ID, Path, ContentHash string
	Content               []byte
}
type ArtifactRecord struct {
	ID, Kind, ContentHash string
	Content, Lineage      []byte
}
type CompilationCommit struct {
	EffectKey              string
	Sources                []SourceRecord
	Artifacts              []ArtifactRecord
	Manifest, StelePayload []byte
}
type CompilationResult struct {
	GenerationID int64
	EffectID     string
}
type CompilationRepository interface {
	Commit(context.Context, CompilationCommit) (CompilationResult, error)
}
type compilationRepository struct{ store *Store }

func (s *Store) Compilations() CompilationRepository { return &compilationRepository{store: s} }

func (r *compilationRepository) Commit(ctx context.Context, request CompilationCommit) (CompilationResult, error) {
	if request.EffectKey == "" || len(request.Manifest) == 0 {
		return CompilationResult{}, errors.New("storage: compilation effect key and manifest are required")
	}
	requestPayload, _ := json.Marshal(request)
	var existing struct{ ID, Result []byte }
	var existingID, status string
	err := r.store.db.QueryRowContext(ctx, r.store.bind(`SELECT id,status,result FROM idempotency_effects WHERE namespace=? AND subject=? AND effect_key=?`), "compilation", "knowledge", request.EffectKey).Scan(&existingID, &status, &existing.Result)
	if err == nil && status == string(EffectCompleted) {
		_ = json.Unmarshal(existing.Result, &existing)
		return CompilationResult{GenerationID: decodeGeneration(existing.Result), EffectID: existingID}, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return CompilationResult{}, err
	}
	tx, err := r.store.db.BeginTx(ctx, nil)
	if err != nil {
		return CompilationResult{}, err
	}
	defer func() { _ = tx.Rollback() }()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if existingID == "" {
		existingID = uuid.NewString()
		if _, err := tx.ExecContext(ctx, r.store.bind(`INSERT INTO idempotency_effects(id,namespace,subject,effect_key,status,request,result,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`), existingID, "compilation", "knowledge", request.EffectKey, EffectInProgress, requestPayload, []byte{}, now, now); err != nil {
			return CompilationResult{}, err
		}
	}
	var generationID int64
	if err := tx.QueryRowContext(ctx, r.store.bind(`SELECT COALESCE(MAX(id),0)+1 FROM knowledge_generations`)).Scan(&generationID); err != nil {
		return CompilationResult{}, err
	}
	if _, err := tx.ExecContext(ctx, r.store.bind(`INSERT INTO knowledge_generations(id,status,commit_marker,created_at,committed_at) VALUES(?,?,?,?,?)`), generationID, GenerationCommitted, uuid.NewString(), now, now); err != nil {
		return CompilationResult{}, err
	}
	for _, source := range request.Sources {
		if _, err := tx.ExecContext(ctx, r.store.bind(`INSERT INTO source_metadata(id,path,content_hash,content,created_at,updated_at) VALUES(?,?,?,?,?,?)`), source.ID, source.Path, source.ContentHash, source.Content, now, now); err != nil {
			return CompilationResult{}, err
		}
	}
	for _, artifact := range request.Artifacts {
		if _, err := tx.ExecContext(ctx, r.store.bind(`INSERT INTO artifacts(id,generation_id,kind,content_hash,content,lineage,created_at) VALUES(?,?,?,?,?,?,?)`), artifact.ID, generationID, artifact.Kind, artifact.ContentHash, artifact.Content, artifact.Lineage, now); err != nil {
			return CompilationResult{}, err
		}
	}
	if _, err := tx.ExecContext(ctx, r.store.bind(`INSERT INTO manifests(generation_id,payload,created_at) VALUES(?,?,?)`), generationID, request.Manifest, now); err != nil {
		return CompilationResult{}, err
	}
	steleKey := request.EffectKey + ":stele"
	if _, err := tx.ExecContext(ctx, r.store.bind(`INSERT INTO outboxes(id,kind,idempotency_key,payload_version,payload,status,attempts,owner,lease_until,available_at,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`), uuid.NewString(), OutboxKindStele, steleKey, 1, request.StelePayload, "pending", 0, "", nil, now, now, now); err != nil {
		return CompilationResult{}, err
	}
	resultJSON, _ := json.Marshal(CompilationResult{GenerationID: generationID, EffectID: existingID})
	if _, err := tx.ExecContext(ctx, r.store.bind(`UPDATE idempotency_effects SET status=?,result=?,updated_at=? WHERE id=?`), EffectCompleted, resultJSON, now, existingID); err != nil {
		return CompilationResult{}, err
	}
	if err := r.store.fail("state_outbox_commit"); err != nil {
		return CompilationResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return CompilationResult{}, err
	}
	return CompilationResult{GenerationID: generationID, EffectID: existingID}, nil
}

func decodeGeneration(raw []byte) int64 {
	var result CompilationResult
	_ = json.Unmarshal(raw, &result)
	return result.GenerationID
}

func (r *compilationRepository) String() string {
	return fmt.Sprintf("compilation repository for %s", r.store.driver)
}
