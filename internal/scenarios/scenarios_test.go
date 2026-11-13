package scenarios

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	agentknowledge "github.com/FelixSeptem/danny/internal/agent/knowledge"
	"github.com/FelixSeptem/danny/internal/deterministic"
	"github.com/FelixSeptem/danny/internal/knowledge"
	"github.com/FelixSeptem/danny/internal/storage"
)

// newScenarioStore opens a file-backed SQLite store. A file database is used
// on purpose: the crash-recovery scenario needs a store that can be closed and
// reopened over the same data.
func newScenarioStore(t *testing.T) *storage.Store {
	t.Helper()
	store, err := storage.Open(context.Background(), "sqlite", filepath.Join(t.TempDir(), "danny.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return store
}

// TestScenarioTaskRevisionAcrossConversations proves S03: a task created in
// one conversation can be updated and completed in another, and a stale
// writer can never overwrite the newer durable state.
func TestScenarioTaskRevisionAcrossConversations(t *testing.T) {
	store := newScenarioStore(t)
	ctx := context.Background()

	created, err := store.Tasks().Create(ctx, storage.Task{ID: "task-s03", Title: "draft"})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	title := "first revision"
	updated, err := store.Tasks().Update(ctx, created.ID, created.Revision, storage.TaskPatch{Title: &title})
	if err != nil {
		t.Fatalf("first update: %v", err)
	}

	// A second conversation still holds the old revision and must be refused.
	stale := "stale revision"
	if _, err := store.Tasks().Update(ctx, created.ID, created.Revision, storage.TaskPatch{Title: &stale}); !errors.Is(err, storage.ErrRevisionConflict) {
		t.Fatalf("stale update error = %v, want ErrRevisionConflict", err)
	}

	current, err := store.Tasks().Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if current.Title != "first revision" {
		t.Fatalf("stale update overwrote newer state: %q", current.Title)
	}
	if current.Revision != updated.Revision {
		t.Fatalf("revision = %d, want %d", current.Revision, updated.Revision)
	}

	completed, err := store.Tasks().Update(ctx, created.ID, current.Revision, storage.TaskPatch{Status: "completed"})
	if err != nil {
		t.Fatalf("complete task: %v", err)
	}
	if completed.Status != "completed" {
		t.Fatalf("status = %q, want completed", completed.Status)
	}
}

// TestScenarioDeterministicContractIsReproducible proves the deterministic
// half of S11: the scripted double records exactly what the contract
// requires, two identical runs agree, and a provider dial fails loudly
// instead of falling back.
func TestScenarioDeterministicContractIsReproducible(t *testing.T) {
	ctx := context.Background()
	request := agentknowledge.ExecutionRequest{
		RunID:            "run-s11",
		CorrelationID:    "corr-s11",
		PlanVersion:      "planner.v1",
		PlanDigest:       "plan-digest",
		CapabilityDigest: "cap-digest",
		ProjectionDigest: "proj-digest",
		Role:             "coordinator",
	}

	run := func() (agentknowledge.ExecutionOutcome, agentknowledge.ExecutionRequest) {
		executor := deterministic.NewExecutor()
		executor.RunOutcome = agentknowledge.ExecutionOutcome{Status: "succeeded", Result: "answer"}
		outcome, err := executor.Run(ctx, request)
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		calls := executor.CallsOfKind("run")
		if len(calls) != 1 {
			t.Fatalf("recorded %d run calls, want 1", len(calls))
		}
		return outcome, calls[0].Request
	}

	first, firstRequest := run()
	second, secondRequest := run()
	if first != second {
		t.Fatalf("two identical scripted runs disagree: %#v vs %#v", first, second)
	}
	if !reflect.DeepEqual(firstRequest, secondRequest) {
		t.Fatalf("two identical scripted runs recorded different requests: %#v vs %#v", firstRequest, secondRequest)
	}
	if firstRequest.PlanDigest != "plan-digest" || firstRequest.CapabilityDigest != "cap-digest" {
		t.Fatalf("recorded request lost its digests: %#v", firstRequest)
	}

	if err := deterministic.ProviderDial(); !errors.Is(err, deterministic.ErrProviderDial) {
		t.Fatalf("ProviderDial() error = %v, want ErrProviderDial", err)
	}

	// The compile coordinator must refuse an invalid plan before it reaches
	// the executor, so a broken plan can never produce a commit.
	executor := deterministic.NewCompileExecutor(nil)
	coordinator := knowledge.NewCompileCoordinator(executor)
	if _, err := coordinator.Run(ctx, knowledge.CompileRunRequest{RunID: "run-s11"}); !errors.Is(err, knowledge.ErrCompileBlocked) {
		t.Fatalf("invalid plan error = %v, want ErrCompileBlocked", err)
	}
	if len(executor.Calls()) != 0 {
		t.Fatalf("a rejected plan still reached the executor: %d calls", len(executor.Calls()))
	}
}
