package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/FelixSeptem/danny/internal/agentui"
)

// Typed agent-UI repository errors.
var (
	// ErrAgentUIConflict marks conflicting content for a known native identity.
	ErrAgentUIConflict = errors.New("storage: conflicting agent UI event content")
	// ErrAgentUIClosed marks an append after a closed terminal event.
	ErrAgentUIClosed = errors.New("storage: agent UI event log is closed")
	// ErrAgentUIStaleSurface marks a surface write behind the current revision.
	ErrAgentUIStaleSurface = errors.New("storage: stale agent UI surface revision")
)

// AgentUIEvent is the persisted projection row. Kind is the string form of
// agentui.EventKind; the envelope contract is validated by the agentui
// package before persistence.
type AgentUIEvent struct {
	ExecutionID   string
	Seq           int64
	Kind          string
	Terminal      bool
	NativeSource  string
	NativeEventID string
	Contract      string
	Payload       []byte
	CreatedAt     time.Time
}

// AgentUIRepository persists the browser-facing projection log and dynamic
// surfaces. Appends are idempotent by native identity and rejected after a
// closed terminal; interrupts (pauses) allow later resume appends.
type AgentUIRepository interface {
	AppendEvent(context.Context, AgentUIEvent) (int64, error)
	EventsAfter(ctx context.Context, executionID string, afterSeq int64, limit int) ([]AgentUIEvent, bool, error)
	SaveSurface(ctx context.Context, surface agentui.DynamicSurface, taskRevision int64) (agentui.DynamicSurface, error)
	GetSurface(ctx context.Context, surfaceID string) (agentui.DynamicSurface, int64, error)
}

func (s *Store) AgentUI() AgentUIRepository { return &agentUIRepository{store: s} }

type agentUIRepository struct{ store *Store }

func (r *agentUIRepository) AppendEvent(ctx context.Context, event AgentUIEvent) (int64, error) {
	if event.ExecutionID == "" || event.Kind == "" || event.Contract == "" {
		return 0, fmt.Errorf("storage: agent UI event requires execution, kind and contract")
	}
	if event.NativeSource == "" && event.NativeEventID != "" {
		return 0, fmt.Errorf("storage: native event id requires native source")
	}
	tx, err := r.store.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	var closed bool
	if err := tx.QueryRowContext(ctx, r.store.bind(`SELECT COUNT(*) FROM agent_ui_events WHERE execution_id=? AND terminal=1 AND kind IN ('run_finished','run_error')`), event.ExecutionID).Scan(&closed); err != nil {
		return 0, err
	}
	if closed {
		return 0, fmt.Errorf("%w: execution %s", ErrAgentUIClosed, event.ExecutionID)
	}
	if event.NativeEventID != "" {
		var seq int64
		var kind, payload string
		err := tx.QueryRowContext(ctx, r.store.bind(`SELECT seq, kind, payload FROM agent_ui_events WHERE execution_id=? AND native_source=? AND native_event_id=?`), event.ExecutionID, event.NativeSource, event.NativeEventID).Scan(&seq, &kind, &payload)
		if err == nil {
			if kind != event.Kind || string(payload) != string(event.Payload) {
				return 0, fmt.Errorf("%w: native event %s/%s", ErrAgentUIConflict, event.NativeSource, event.NativeEventID)
			}
			return seq, nil // idempotent replay of the same identity
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return 0, err
		}
	}
	var maxSeq int64
	if err := tx.QueryRowContext(ctx, r.store.bind(`SELECT COALESCE(MAX(seq),0) FROM agent_ui_events WHERE execution_id=?`), event.ExecutionID).Scan(&maxSeq); err != nil {
		return 0, err
	}
	seq := maxSeq + 1
	createdAt := event.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	if _, err := tx.ExecContext(ctx, r.store.bind(`INSERT INTO agent_ui_events(execution_id,seq,kind,terminal,native_source,native_event_id,contract_version,payload,created_at) VALUES(?,?,?,?,?,?,?,?,?)`), event.ExecutionID, seq, event.Kind, event.Terminal, event.NativeSource, event.NativeEventID, event.Contract, event.Payload, createdAt.Format(time.RFC3339Nano)); err != nil {
		return 0, fmt.Errorf("append agent UI event: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit agent UI event: %w", err)
	}
	return seq, nil
}

func (r *agentUIRepository) EventsAfter(ctx context.Context, executionID string, afterSeq int64, limit int) ([]AgentUIEvent, bool, error) {
	if executionID == "" {
		return nil, false, fmt.Errorf("storage: agent UI replay requires an execution id")
	}
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	rows, err := r.store.db.QueryContext(ctx, r.store.bind(`SELECT seq,kind,terminal,native_source,native_event_id,contract_version,payload,created_at FROM agent_ui_events WHERE execution_id=? AND seq>? ORDER BY seq LIMIT ?`), executionID, afterSeq, limit)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = rows.Close() }()
	events := []AgentUIEvent{}
	for rows.Next() {
		var e AgentUIEvent
		var created string
		if err := rows.Scan(&e.Seq, &e.Kind, &e.Terminal, &e.NativeSource, &e.NativeEventID, &e.Contract, &e.Payload, &created); err != nil {
			return nil, false, err
		}
		e.ExecutionID = executionID
		e.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	var closed bool
	if err := r.store.db.QueryRowContext(ctx, r.store.bind(`SELECT EXISTS(SELECT 1 FROM agent_ui_events WHERE execution_id=? AND terminal=1 AND kind IN ('run_finished','run_error'))`), executionID).Scan(&closed); err != nil {
		return nil, false, err
	}
	return events, closed, nil
}

func (r *agentUIRepository) SaveSurface(ctx context.Context, surface agentui.DynamicSurface, taskRevision int64) (agentui.DynamicSurface, error) {
	if surface.ID == "" || surface.ExecutionID == "" {
		return agentui.DynamicSurface{}, fmt.Errorf("storage: surface requires identity")
	}
	blob, err := json.Marshal(surface)
	if err != nil {
		return agentui.DynamicSurface{}, fmt.Errorf("storage: marshal surface: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if surface.Revision <= 0 {
		surface.Revision = 1
	}
	// Insert, or advance monotonically: an existing surface only moves
	// forward. A write at or behind the current revision is a typed conflict.
	res, err := r.store.db.ExecContext(ctx, r.store.bind(`UPDATE agent_ui_surfaces SET surface_json=?, revision=?, task_revision=?, catalog_version=?, updated_at=? WHERE id=? AND revision<?`), blob, surface.Revision, taskRevision, surface.CatalogVersion, now, surface.ID, surface.Revision)
	if err != nil {
		return agentui.DynamicSurface{}, err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		if _, _, getErr := r.GetSurface(ctx, surface.ID); getErr == nil {
			var current agentui.DynamicSurface
			var currentRevision, currentTask int64
			if err := r.store.db.QueryRowContext(ctx, r.store.bind(`SELECT surface_json, revision, task_revision FROM agent_ui_surfaces WHERE id=?`), surface.ID).Scan(&blob, &currentRevision, &currentTask); err != nil {
				return agentui.DynamicSurface{}, err
			}
			_ = json.Unmarshal(blob, &current)
			// Same revision with identical content is an idempotent replay;
			// anything else at or behind the current revision is stale.
			if surface.Revision == currentRevision && sameSurface(surface, current) && taskRevision == currentTask {
				return current, nil
			}
			return agentui.DynamicSurface{}, fmt.Errorf("%w: surface %s at revision %d", ErrAgentUIStaleSurface, surface.ID, currentRevision)
		}
		if _, err := r.store.db.ExecContext(ctx, r.store.bind(`INSERT INTO agent_ui_surfaces(id,revision,task_revision,catalog_version,surface_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`), surface.ID, surface.Revision, taskRevision, surface.CatalogVersion, blob, now, now); err != nil {
			return agentui.DynamicSurface{}, fmt.Errorf("insert surface: %w", err)
		}
	}
	return surface, nil
}

func sameSurface(a, b agentui.DynamicSurface) bool {
	ab, _ := json.Marshal(a.Tree)
	bb, _ := json.Marshal(b.Tree)
	return a.ExecutionID == b.ExecutionID && a.CatalogVersion == b.CatalogVersion && string(ab) == string(bb)
}

func (r *agentUIRepository) GetSurface(ctx context.Context, surfaceID string) (agentui.DynamicSurface, int64, error) {
	var surface agentui.DynamicSurface
	var blob []byte
	var revision, taskRevision int64
	err := r.store.db.QueryRowContext(ctx, r.store.bind(`SELECT surface_json, revision, task_revision FROM agent_ui_surfaces WHERE id=?`), surfaceID).Scan(&blob, &revision, &taskRevision)
	if errors.Is(err, sql.ErrNoRows) {
		return agentui.DynamicSurface{}, 0, ErrNotFound
	}
	if err != nil {
		return agentui.DynamicSurface{}, 0, err
	}
	if err := json.Unmarshal(blob, &surface); err != nil {
		return agentui.DynamicSurface{}, 0, fmt.Errorf("storage: decode surface %s: %w", surfaceID, err)
	}
	return surface, taskRevision, nil
}
