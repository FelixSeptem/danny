package storage

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
)

func newHarnessTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := OpenSQLite(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestExecutionCheckpointSurvivesProcessRestart(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "restart.db")
	ctx := context.Background()
	store, err := OpenSQLite(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Executions().Admit(ctx, ExecutionAdmission{Execution: TaskExecution{ID: "restart-1", TaskID: "task", CorrelationID: "corr", IdempotencyKey: "restart", Mode: "direct", PlanVersion: "v1", PlanDigest: "d", CapabilityDigest: "c", ProjectionDigest: "p"}, Plan: ExecutionPlanRecord{ExecutionID: "restart-1", Version: "v1", Mode: "direct", PlanDigest: "d", CapabilityDigest: "c", ProjectionDigest: "p"}}); err != nil {
		t.Fatal(err)
	}
	if err := store.Executions().SaveCheckpoint(ctx, ExecutionCheckpoint{ExecutionID: "restart-1", RunID: "restart-1", PlanVersion: "v1", PlanDigest: "d", CapabilityDigest: "c", ProjectionDigest: "p", PauseReason: []byte("input_required")}); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()
	reopened, err := OpenSQLite(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reopened.Close() }()
	recovered, err := reopened.Executions().GetCheckpoint(ctx, "restart-1")
	if err != nil || recovered.PlanVersion != "v1" || string(recovered.PauseReason) != "input_required" {
		t.Fatalf("recovered=%#v err=%v", recovered, err)
	}
}

func TestExecutionRevisionUpdateAllowsExactlyOneWinner(t *testing.T) {
	store := newHarnessTestStore(t)
	ctx := context.Background()
	if _, err := store.Executions().Admit(ctx, ExecutionAdmission{Execution: TaskExecution{ID: "race-1", TaskID: "task", CorrelationID: "corr", IdempotencyKey: "race", Mode: "direct", PlanVersion: "v1", PlanDigest: "d", CapabilityDigest: "c", ProjectionDigest: "p"}, Plan: ExecutionPlanRecord{ExecutionID: "race-1", Version: "v1", Mode: "direct", PlanDigest: "d", CapabilityDigest: "c", ProjectionDigest: "p"}}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, status := range []ExecutionStatus{ExecutionDispatching, ExecutionPaused} {
		wg.Add(1)
		go func(status ExecutionStatus) {
			defer wg.Done()
			_, err := store.Executions().UpdateExecution(ctx, "race-1", 1, status, nil, nil)
			results <- err
		}(status)
	}
	wg.Wait()
	close(results)
	var success, conflicts int
	var other error
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrRevisionConflict) {
			conflicts++
		} else if err != nil {
			other = err
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatalf("success=%d conflicts=%d other=%v", success, conflicts, other)
	}
}

func TestExecutionAdmissionAndReplayAreDurable(t *testing.T) {
	store := newHarnessTestStore(t)
	repo := store.Executions()
	admission := ExecutionAdmission{Execution: TaskExecution{ID: "run-1", TaskID: "task-1", TaskRevision: 2, CorrelationID: "corr-1", IdempotencyKey: "once", Mode: "direct", PlanVersion: "planner.v1", PlanDigest: "plan", CapabilityDigest: "cap", ProjectionDigest: "projection", Input: "summarize"}, Plan: ExecutionPlanRecord{ExecutionID: "run-1", Version: "planner.v1", Mode: "direct", ProfileDigest: "profile", PlanDigest: "plan", CapabilityDigest: "cap", ProjectionDigest: "projection"}}
	one, err := repo.Admit(context.Background(), admission)
	if err != nil || one.Status != ExecutionAdmitted {
		t.Fatalf("admit=%#v err=%v", one, err)
	}
	replay, err := repo.Admit(context.Background(), admission)
	if err != nil || replay.ID != one.ID || replay.Revision != one.Revision {
		t.Fatalf("replay=%#v err=%v", replay, err)
	}
	plans, err := repo.Plans(context.Background(), "run-1")
	if err != nil || len(plans) != 1 {
		t.Fatalf("plans=%#v err=%v", plans, err)
	}
	if _, err := repo.UpdateExecution(context.Background(), "run-1", 99, ExecutionSucceeded, []byte("ok"), nil); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale update=%v", err)
	}
}

func TestCheckpointEvidenceAndReplanRetainHistory(t *testing.T) {
	store := newHarnessTestStore(t)
	repo := store.Executions()
	_, err := repo.Admit(context.Background(), ExecutionAdmission{Execution: TaskExecution{ID: "run-2", TaskID: "task-2", CorrelationID: "corr-2", IdempotencyKey: "once-2", Mode: "direct", PlanVersion: "v1", PlanDigest: "d1", CapabilityDigest: "c1", ProjectionDigest: "p1"}, Plan: ExecutionPlanRecord{ExecutionID: "run-2", Version: "v1", Mode: "direct", PlanDigest: "d1", CapabilityDigest: "c1", ProjectionDigest: "p1"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveCheckpoint(context.Background(), ExecutionCheckpoint{ExecutionID: "run-2", RunID: "run-2", PlanVersion: "v1", PlanDigest: "d1", CapabilityDigest: "c1", ProjectionDigest: "p1", PauseReason: []byte("approval_required")}); err != nil {
		t.Fatal(err)
	}
	checkpoint, err := repo.GetCheckpoint(context.Background(), "run-2")
	if err != nil || checkpoint.PlanDigest != "d1" {
		t.Fatalf("checkpoint=%#v err=%v", checkpoint, err)
	}
	if err := repo.RecordReplan(context.Background(), ExecutionReplan{ExecutionID: "run-2", PreviousPlanVersion: "v1", NewPlanVersion: "v2", Reason: "task changed", PreviousProfileDigest: "p1", NewProfileDigest: "p2"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.RecordEvidence(context.Background(), ExecutionEvidenceRecord{ExecutionID: "run-2", EffectKey: "run-2:parent", PlanVersion: "v1", Status: []byte("succeeded")}); err != nil {
		t.Fatal(err)
	}
	replay, err := repo.RecordEvidence(context.Background(), ExecutionEvidenceRecord{ExecutionID: "run-2", EffectKey: "run-2:parent", PlanVersion: "v1", Status: []byte("failed")})
	if err != nil || string(replay.Status) != "succeeded" {
		t.Fatalf("evidence replay=%#v err=%v", replay, err)
	}
}
