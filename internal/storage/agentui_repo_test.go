package storage

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/FelixSeptem/danny/internal/agentui"
)

func newAgentUIStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(context.Background(), "sqlite", "file:agentui-"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	return store
}

func agentUIEvent(executionID, kind string, terminal bool, source, eventID string, payload string) AgentUIEvent {
	return AgentUIEvent{
		ExecutionID:   executionID,
		Kind:          kind,
		Terminal:      terminal,
		NativeSource:  source,
		NativeEventID: eventID,
		Contract:      agentui.ProjectionContractV1,
		Payload:       []byte(payload),
		CreatedAt:     time.Now().UTC(),
	}
}

func TestAgentUIAppendIsIdempotentByNativeIdentity(t *testing.T) {
	store := newAgentUIStore(t)
	ctx := context.Background()
	repo := store.AgentUI()

	seq, err := repo.AppendEvent(ctx, agentUIEvent("exec-1", "run_started", false, "", "", `{"mode":"direct"}`))
	if err != nil || seq != 1 {
		t.Fatalf("first append: seq=%d err=%v", seq, err)
	}
	dup := agentUIEvent("exec-1", "step_started", false, "danny.checkpoint", "checkpoint-1", `{"step":"plan"}`)
	if seq, err = repo.AppendEvent(ctx, dup); err != nil || seq != 2 {
		t.Fatalf("second append: seq=%d err=%v", seq, err)
	}
	if seq, err = repo.AppendEvent(ctx, dup); err != nil || seq != 2 {
		t.Fatalf("replayed identity must be idempotent: seq=%d err=%v", seq, err)
	}
	conflict := dup
	conflict.Payload = []byte(`{"step":"different"}`)
	if _, err := repo.AppendEvent(ctx, conflict); !errors.Is(err, ErrAgentUIConflict) {
		t.Fatalf("conflicting content must be typed conflict, got %v", err)
	}
}

func TestAgentUILogClosesOnTerminalButAllowsInterruptResume(t *testing.T) {
	store := newAgentUIStore(t)
	ctx := context.Background()
	repo := store.AgentUI()

	if _, err := repo.AppendEvent(ctx, agentUIEvent("exec-2", "run_started", false, "", "", `{}`)); err != nil {
		t.Fatalf("append start: %v", err)
	}
	if _, err := repo.AppendEvent(ctx, agentUIEvent("exec-2", "run_error", true, "", "", `{"message":"boom"}`)); err != nil {
		t.Fatalf("append terminal: %v", err)
	}
	if _, err := repo.AppendEvent(ctx, agentUIEvent("exec-2", "step_started", false, "", "", `{}`)); !errors.Is(err, ErrAgentUIClosed) {
		t.Fatalf("append after terminal must be closed, got %v", err)
	}

	if _, err := repo.AppendEvent(ctx, agentUIEvent("exec-3", "run_interrupted", true, "danny.checkpoint", "cp-1", `{"pause_reason":"input_required"}`)); err != nil {
		t.Fatalf("interrupt append: %v", err)
	}
	if _, err := repo.AppendEvent(ctx, agentUIEvent("exec-3", "step_started", false, "", "", `{"step":"resumed"}`)); err != nil {
		t.Fatalf("resume append after interrupt must be allowed: %v", err)
	}
	events, closed, err := repo.EventsAfter(ctx, "exec-3", 0, 100)
	if err != nil || closed || len(events) != 2 || events[0].Seq != 1 || events[1].Seq != 2 {
		t.Fatalf("EventsAfter: events=%d closed=%v err=%v", len(events), closed, err)
	}
}

func TestAgentUIReplayIsBoundedAndOrdered(t *testing.T) {
	store := newAgentUIStore(t)
	ctx := context.Background()
	repo := store.AgentUI()
	for i := 0; i < 5; i++ {
		if _, err := repo.AppendEvent(ctx, agentUIEvent("exec-4", "step_started", false, "", "", `{"step":"s"}`)); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	events, closed, err := repo.EventsAfter(ctx, "exec-4", 2, 2)
	if err != nil || closed || len(events) != 2 || events[0].Seq != 3 || events[1].Seq != 4 {
		t.Fatalf("page after cursor 2: events=%d closed=%v err=%v", len(events), closed, err)
	}
	if _, closed, err = repo.EventsAfter(ctx, "exec-4", 0, 100); err != nil || closed {
		t.Fatalf("no terminal yet: closed=%v err=%v", closed, err)
	}
	if _, _, err := repo.EventsAfter(ctx, "", 0, 100); err == nil {
		t.Fatal("foreign empty execution id must fail typed")
	}
}

func TestAgentUISurfaceRevisionAdvancesMonotonically(t *testing.T) {
	store := newAgentUIStore(t)
	ctx := context.Background()
	repo := store.AgentUI()
	catalog := &agentui.Catalog{
		Version:    agentui.CatalogVersion,
		Components: []agentui.Component{{Name: "Card"}},
		Actions:    []agentui.Action{{ID: "confirm", PolicyRef: "danny.policy.read"}},
	}
	surface := agentui.DynamicSurface{
		ContractVersion: agentui.SurfaceContractV1,
		ID:              "surface-1",
		ExecutionID:     "exec-5",
		Revision:        1,
		CatalogVersion:  agentui.CatalogVersion,
		Tree:            surfaceTree(t, "Card"),
		Actions:         []string{"confirm"},
	}
	saved, err := repo.SaveSurface(ctx, surface, 3)
	if err != nil {
		t.Fatalf("SaveSurface: %v", err)
	}
	if saved.Revision != 1 {
		t.Fatalf("revision = %d, want 1", saved.Revision)
	}
	again, err := repo.SaveSurface(ctx, surface, 3)
	if err != nil || again.Revision != 1 {
		t.Fatalf("same-content replay: revision=%d err=%v", again.Revision, err)
	}
	surface.Revision = 2
	surface.Tree = surfaceTree(t, "Card")
	if _, err = repo.SaveSurface(ctx, surface, 3); err != nil {
		t.Fatalf("advance revision: %v", err)
	}
	surface.Revision = 1
	if _, err = repo.SaveSurface(ctx, surface, 3); !errors.Is(err, ErrAgentUIStaleSurface) {
		t.Fatalf("stale write must be typed conflict, got %v", err)
	}
	got, taskRevision, err := repo.GetSurface(ctx, "surface-1")
	if err != nil || got.Revision != 2 || taskRevision != 3 {
		t.Fatalf("GetSurface: revision=%d task=%d err=%v", got.Revision, taskRevision, err)
	}
	if _, _, err = repo.GetSurface(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing surface must be ErrNotFound, got %v", err)
	}
	_ = catalog
}

func surfaceTree(t *testing.T, component string) []agentui.SurfaceNode {
	t.Helper()
	tree, err := agentui.NewTree([]byte(`[{"component":"` + component + `"}]`))
	if err != nil {
		t.Fatalf("NewTree: %v", err)
	}
	return tree
}
