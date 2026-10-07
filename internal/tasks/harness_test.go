package tasks

import (
	"context"
	"errors"
	"testing"

	"github.com/FelixSeptem/danny/internal/agent/knowledge"
	"github.com/FelixSeptem/danny/internal/storage"
)

type fakeExecutor struct {
	parent   knowledge.ExecutionOutcome
	child    knowledge.ExecutionOutcome
	children int
	err      error
}

func (f *fakeExecutor) Run(context.Context, knowledge.ExecutionRequest) (knowledge.ExecutionOutcome, error) {
	if f.err != nil {
		return knowledge.ExecutionOutcome{}, f.err
	}
	return f.parent, nil
}
func (f *fakeExecutor) RunChild(context.Context, knowledge.ExecutionRequest) (knowledge.ExecutionOutcome, error) {
	f.children++
	return f.child, nil
}

func newHarness(t *testing.T, executor *fakeExecutor) (*Harness, *storage.Store) {
	t.Helper()
	store, err := storage.OpenSQLite(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return &Harness{Repository: store.Executions(), Planner: knowledge.NewPlanner("planner.v1"), Executor: executor}, store
}

func TestHarnessDirectExecutionCommitsOnce(t *testing.T) {
	harness, store := newHarness(t, &fakeExecutor{parent: knowledge.ExecutionOutcome{Status: "succeeded", Result: "answer"}})
	run, err := harness.Admit(context.Background(), AdmissionRequest{ExecutionID: "direct-1", TaskID: "task-1", CorrelationID: "corr-1", IdempotencyKey: "direct-once", Policy: knowledge.DefaultPolicy(knowledge.PolicyRead), Profile: knowledge.TaskProfile{Objective: "answer", Complexity: 1, Parallelism: 1, Budget: 1000}, ProjectionDigest: "projection-1"})
	if err != nil {
		t.Fatal(err)
	}
	completed, err := harness.Execute(context.Background(), run.Execution.ID)
	if err != nil || completed.Execution.Status != storage.ExecutionSucceeded {
		t.Fatalf("completed=%#v err=%v", completed, err)
	}
	replay, err := harness.Execute(context.Background(), run.Execution.ID)
	if err != nil || replay.Execution.Status != storage.ExecutionSucceeded {
		t.Fatalf("replay=%#v err=%v", replay, err)
	}
	evidence, err := store.Executions().Evidence(context.Background(), run.Execution.ID)
	if err != nil || len(evidence) != 1 {
		t.Fatalf("evidence=%#v err=%v", evidence, err)
	}
}

func TestHarnessFanoutBlocksCommitOnChildFailure(t *testing.T) {
	executor := &fakeExecutor{parent: knowledge.ExecutionOutcome{Status: "succeeded"}, child: knowledge.ExecutionOutcome{Status: "failed", FailureClass: "child failure"}}
	harness, _ := newHarness(t, executor)
	run, err := harness.Admit(context.Background(), AdmissionRequest{ExecutionID: "fanout-1", TaskID: "task-2", CorrelationID: "corr-2", IdempotencyKey: "fanout-once", Policy: knowledge.DefaultPolicy(knowledge.PolicyRead), Profile: knowledge.TaskProfile{Objective: "split", Complexity: 6, Parallelism: 3, Budget: 3000}, ProjectionDigest: "projection-2"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := harness.Execute(context.Background(), run.Execution.ID); !errors.Is(err, ErrCommitBlocked) {
		t.Fatalf("error=%v", err)
	}
	execution, err := harness.Repository.GetExecution(context.Background(), run.Execution.ID)
	if err != nil || execution.Status != storage.ExecutionBlocked || executor.children != 1 {
		t.Fatalf("execution=%#v children=%d err=%v", execution, executor.children, err)
	}
}

func TestHarnessRejectsUnsupportedModeWithoutDispatch(t *testing.T) {
	executor := &fakeExecutor{parent: knowledge.ExecutionOutcome{Status: "succeeded"}}
	harness, _ := newHarness(t, executor)
	_, err := harness.Admit(context.Background(), AdmissionRequest{TaskID: "task-3", CorrelationID: "corr-3", Policy: knowledge.DefaultPolicy(knowledge.PolicyRead), Profile: knowledge.TaskProfile{Objective: "workflow", Complexity: 1, Budget: 1000}, RequestedMode: knowledge.ModeWorkflow})
	var unsupported UnsupportedModeError
	if !errors.As(err, &unsupported) || unsupported.Mode != knowledge.ModeWorkflow || executor.children != 0 {
		t.Fatalf("error=%v unsupported=%#v", err, unsupported)
	}
}

func TestHarnessPauseResumeAndTaskRevisionReplan(t *testing.T) {
	harness, store := newHarness(t, &fakeExecutor{parent: knowledge.ExecutionOutcome{Status: "succeeded", Result: "resumed"}})
	run, err := harness.Admit(context.Background(), AdmissionRequest{ExecutionID: "pause-1", TaskID: "task-4", TaskRevision: 1, CorrelationID: "corr-4", IdempotencyKey: "pause-once", Policy: knowledge.DefaultPolicy(knowledge.PolicyRead), Profile: knowledge.TaskProfile{Objective: "pause", Complexity: 1, Budget: 1000}, ProjectionDigest: "projection-4"})
	if err != nil {
		t.Fatal(err)
	}
	if err := harness.Pause(context.Background(), run.Execution.ID, knowledge.PauseApprovalRequired, "before-dispatch", []string{"effect-1"}); err != nil {
		t.Fatal(err)
	}
	checkpoint, err := store.Executions().GetCheckpoint(context.Background(), run.Execution.ID)
	if err != nil || string(checkpoint.PauseReason) != string(knowledge.PauseApprovalRequired) {
		t.Fatalf("checkpoint=%#v err=%v", checkpoint, err)
	}
	resumed, err := harness.Resume(context.Background(), run.Execution.ID, knowledge.ResumeRequest{PlanVersion: run.Plan.Version, PlanDigest: run.Plan.Digest, CapabilityDigest: run.Plan.CapabilityDigest, ProjectionDigest: "projection-4"})
	if err != nil || resumed.Execution.Status != storage.ExecutionSucceeded {
		t.Fatalf("resumed=%#v err=%v", resumed, err)
	}
	second, err := harness.Admit(context.Background(), AdmissionRequest{ExecutionID: "pause-2", TaskID: "task-4", TaskRevision: 1, CorrelationID: "corr-4b", IdempotencyKey: "pause-once-2", Policy: knowledge.DefaultPolicy(knowledge.PolicyRead), Profile: knowledge.TaskProfile{Objective: "pause", Complexity: 1, Budget: 1000}, ProjectionDigest: "projection-4"})
	if err != nil {
		t.Fatal(err)
	}
	replanned, err := harness.EnsureTaskRevision(context.Background(), second.Execution.ID, 2, knowledge.TaskProfile{Objective: "changed", Complexity: 5, Parallelism: 2, Budget: 2000}, "task revision changed")
	if err != nil {
		t.Fatal(err)
	}
	if replanned.Version == run.Plan.Version {
		t.Fatalf("replan version did not change: %#v", replanned)
	}
}

func TestHarnessAuthorityExpansionPausesBeforeDispatch(t *testing.T) {
	harness, store := newHarness(t, &fakeExecutor{parent: knowledge.ExecutionOutcome{Status: "succeeded"}})
	run, err := harness.Admit(context.Background(), AdmissionRequest{ExecutionID: "authority-1", TaskID: "task-5", TaskRevision: 1, CorrelationID: "corr-5", IdempotencyKey: "authority-once", Policy: knowledge.DefaultPolicy(knowledge.PolicyRead), Profile: knowledge.TaskProfile{Objective: "read", Complexity: 1, Budget: 100}, ProjectionDigest: "projection-5"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := harness.Replan(context.Background(), run.Execution.ID, knowledge.TaskProfile{Objective: "write", Complexity: 6, Parallelism: 2, Budget: 2000, WriteScope: 2}, "authority expansion"); err != nil {
		t.Fatal(err)
	}
	execution, err := store.Executions().GetExecution(context.Background(), run.Execution.ID)
	if err != nil || execution.Status != storage.ExecutionPaused {
		t.Fatalf("execution=%#v err=%v", execution, err)
	}
}

func TestHarnessPersistsUncertainDispatchForRecovery(t *testing.T) {
	harness, _ := newHarness(t, &fakeExecutor{err: ErrDispatchUncertain})
	run, err := harness.Admit(context.Background(), AdmissionRequest{ExecutionID: "uncertain-1", TaskID: "task-6", CorrelationID: "corr-6", IdempotencyKey: "uncertain-once", Policy: knowledge.DefaultPolicy(knowledge.PolicyRead), Profile: knowledge.TaskProfile{Objective: "dispatch", Budget: 1000}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := harness.Execute(context.Background(), run.Execution.ID); !errors.Is(err, ErrDispatchUncertain) {
		t.Fatalf("error=%v", err)
	}
	execution, err := harness.Repository.GetExecution(context.Background(), run.Execution.ID)
	if err != nil || execution.Status != storage.ExecutionRecoveryRequired {
		t.Fatalf("execution=%#v err=%v", execution, err)
	}
}
