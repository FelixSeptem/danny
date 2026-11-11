package tasks

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/FelixSeptem/danny/internal/agent/knowledge"
	"github.com/FelixSeptem/danny/internal/runtimeboundary"
	"github.com/FelixSeptem/danny/internal/storage"
)

type fakeExecutor struct {
	parent      knowledge.ExecutionOutcome
	child       knowledge.ExecutionOutcome
	children    int
	parentCalls int
	onRun       func()
	err         error
}

type fakeNativeContinuation struct {
	called int
	last   runtimeboundary.NativeResumeRequest
	out    runtimeboundary.NativeResumeOutcome
}

type fakeNativeControl struct {
	called int
	last   runtimeboundary.ControlRequest
	out    runtimeboundary.ControlOutcome
}

func (f *fakeNativeControl) Control(_ context.Context, req runtimeboundary.ControlRequest) (runtimeboundary.ControlOutcome, error) {
	f.called++
	f.last = req
	return f.out, nil
}

type fakeNativeRetry struct {
	called int
	last   runtimeboundary.RetryRequest
	out    runtimeboundary.RetryOutcome
}

type fakeNativeRecovery struct {
	called int
	last   runtimeboundary.RecoveryRequest
	out    runtimeboundary.RecoveryOutcome
	err    error
}

func (f *fakeNativeRecovery) Reconcile(_ context.Context, req runtimeboundary.RecoveryRequest) (runtimeboundary.RecoveryOutcome, error) {
	f.called++
	f.last = req
	return f.out, f.err
}

func (f *fakeNativeRetry) Retry(_ context.Context, req runtimeboundary.RetryRequest) (runtimeboundary.RetryOutcome, error) {
	f.called++
	f.last = req
	return f.out, nil
}

func (f *fakeNativeContinuation) ResumeNative(_ context.Context, req runtimeboundary.NativeResumeRequest) (runtimeboundary.NativeResumeOutcome, error) {
	f.called++
	f.last = req
	return f.out, nil
}

func (f *fakeExecutor) Run(context.Context, knowledge.ExecutionRequest) (knowledge.ExecutionOutcome, error) {
	f.parentCalls++
	if f.onRun != nil {
		f.onRun()
	}
	if f.err != nil {
		return knowledge.ExecutionOutcome{}, f.err
	}
	return f.parent, nil
}
func (f *fakeExecutor) RunChild(context.Context, knowledge.ExecutionRequest) (knowledge.ExecutionOutcome, error) {
	f.children++
	return f.child, nil
}

func newHarness(t *testing.T, executor knowledge.Executor) (*Harness, *storage.Store) {
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

func createHarnessTask(t *testing.T, store *storage.Store, id string) storage.Task {
	t.Helper()
	task, err := store.Tasks().Create(context.Background(), storage.Task{ID: id, Title: id})
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func TestHarnessDirectExecutionCommitsOnce(t *testing.T) {
	harness, store := newHarness(t, &fakeExecutor{parent: knowledge.ExecutionOutcome{Status: "succeeded", Result: "answer"}})
	task := createHarnessTask(t, store, "task-1")
	run, err := harness.Admit(context.Background(), AdmissionRequest{ExecutionID: "direct-1", TaskID: task.ID, TaskRevision: task.Revision, CorrelationID: "corr-1", IdempotencyKey: "direct-once", Policy: knowledge.DefaultPolicy(knowledge.PolicyRead), Profile: knowledge.TaskProfile{Objective: "answer", Complexity: 1, Parallelism: 1, Budget: 1000}, ProjectionDigest: "projection-1"})
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

func TestHarnessRetainsNativeSuccessWhenTaskRevisionChangesBeforeProductCommit(t *testing.T) {
	ctx := context.Background()
	executor := &fakeExecutor{parent: knowledge.ExecutionOutcome{Status: "succeeded", Result: "native answer"}}
	harness, store := newHarness(t, executor)
	task, err := store.Tasks().Create(ctx, storage.Task{ID: "task-stale-commit", Title: "original"})
	if err != nil {
		t.Fatal(err)
	}
	executor.onRun = func() {
		description := "updated while native run was active"
		if _, updateErr := store.Tasks().Update(ctx, task.ID, task.Revision, storage.TaskPatch{Description: &description}); updateErr != nil {
			t.Errorf("update task: %v", updateErr)
		}
	}
	run, err := harness.Admit(ctx, AdmissionRequest{ExecutionID: "stale-commit-1", TaskID: task.ID, TaskRevision: task.Revision, CorrelationID: "corr-stale-commit", IdempotencyKey: "stale-commit-once", Policy: knowledge.DefaultPolicy(knowledge.PolicyRead), Profile: knowledge.TaskProfile{Objective: "answer", Complexity: 1, Budget: 1000}, ProjectionDigest: "projection-stale"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := harness.Execute(ctx, run.Execution.ID); !errors.Is(err, ErrProductRevisionConflict) {
		t.Fatalf("execute error=%v, want product revision conflict", err)
	}
	evidence, err := store.Executions().Evidence(ctx, run.Execution.ID)
	if err != nil || len(evidence) != 1 || string(evidence[0].Result) != "native answer" || string(evidence[0].Status) != "succeeded" {
		t.Fatalf("native evidence=%#v err=%v", evidence, err)
	}
	stored, err := store.Executions().GetExecution(ctx, run.Execution.ID)
	if err != nil || stored.Status != storage.ExecutionBlocked {
		t.Fatalf("execution=%#v err=%v", stored, err)
	}
	if _, err := harness.Execute(ctx, run.Execution.ID); err != nil || executor.parentCalls != 1 {
		t.Fatalf("replay err=%v native calls=%d", err, executor.parentCalls)
	}
}

func TestHarnessRejectsStaleTaskRevisionBeforeNativeAdmission(t *testing.T) {
	ctx := context.Background()
	executor := &fakeExecutor{parent: knowledge.ExecutionOutcome{Status: "succeeded"}}
	harness, store := newHarness(t, executor)
	task, err := store.Tasks().Create(ctx, storage.Task{ID: "task-stale-admission", Title: "original"})
	if err != nil {
		t.Fatal(err)
	}
	title := "updated"
	if _, err := store.Tasks().Update(ctx, task.ID, task.Revision, storage.TaskPatch{Title: &title}); err != nil {
		t.Fatal(err)
	}
	_, err = harness.Admit(ctx, AdmissionRequest{ExecutionID: "stale-admission-1", TaskID: task.ID, TaskRevision: task.Revision, CorrelationID: "corr-stale-admission", IdempotencyKey: "stale-admission-once", Policy: knowledge.DefaultPolicy(knowledge.PolicyRead), Profile: knowledge.TaskProfile{Objective: "answer", Complexity: 1, Budget: 1000}, ProjectionDigest: "projection-stale"})
	if !errors.Is(err, ErrProductRevisionConflict) || executor.parentCalls != 0 {
		t.Fatalf("admit error=%v native calls=%d", err, executor.parentCalls)
	}
}

func TestHarnessReplanBindsCurrentTaskRevision(t *testing.T) {
	ctx := context.Background()
	harness, store := newHarness(t, &fakeExecutor{parent: knowledge.ExecutionOutcome{Status: "succeeded"}})
	task := createHarnessTask(t, store, "task-replan-revision")
	run, err := harness.Admit(ctx, AdmissionRequest{ExecutionID: "replan-revision-1", TaskID: task.ID, TaskRevision: task.Revision, CorrelationID: "corr-replan-revision", IdempotencyKey: "replan-revision-once", Policy: knowledge.DefaultPolicy(knowledge.PolicyRead), Profile: knowledge.TaskProfile{Objective: "original", Complexity: 1, Budget: 1000}, ProjectionDigest: "projection-replan"})
	if err != nil {
		t.Fatal(err)
	}
	completedEffects := []byte("[\"effect:one\"]")
	if _, err := store.DB().ExecContext(ctx, `UPDATE task_harness_plans SET completed_effect_keys=? WHERE execution_id=?`, completedEffects, run.Execution.ID); err != nil {
		t.Fatal(err)
	}
	description := "changed"
	updated, err := store.Tasks().Update(ctx, task.ID, task.Revision, storage.TaskPatch{Description: &description})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := harness.EnsureTaskRevision(ctx, run.Execution.ID, updated.Revision, knowledge.TaskProfile{Objective: "changed", Complexity: 2, Budget: 2000}, "task changed"); err != nil {
		t.Fatal(err)
	}
	bound, err := harness.Repository.GetExecution(ctx, run.Execution.ID)
	if err != nil {
		t.Fatal(err)
	}
	if bound.TaskRevision != updated.Revision {
		t.Fatalf("task revision binding=%d, want %d", bound.TaskRevision, updated.Revision)
	}
	plans, err := harness.Repository.Plans(ctx, run.Execution.ID)
	if err != nil || len(plans) != 2 || string(plans[1].CompletedEffectKeys) != string(completedEffects) {
		t.Fatalf("replanned completed effects=%#v err=%v", plans, err)
	}
}

func TestHarnessReplanBindsNewProjectionDigest(t *testing.T) {
	ctx := context.Background()
	harness, store := newHarness(t, &fakeExecutor{parent: knowledge.ExecutionOutcome{Status: "succeeded"}})
	task := createHarnessTask(t, store, "task-replan-projection")
	run, err := harness.Admit(ctx, AdmissionRequest{ExecutionID: "replan-projection-1", TaskID: task.ID, TaskRevision: task.Revision, CorrelationID: "corr-replan-projection", IdempotencyKey: "replan-projection-once", Policy: knowledge.DefaultPolicy(knowledge.PolicyRead), Profile: knowledge.TaskProfile{Objective: "original", Complexity: 1, Budget: 1000}, ProjectionDigest: "projection-before"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := harness.ReplanWithProjection(ctx, run.Execution.ID, knowledge.TaskProfile{Objective: "refreshed", Complexity: 1, Budget: 1000}, "projection-after", "context refreshed"); err != nil {
		t.Fatal(err)
	}
	bound, err := harness.Repository.GetExecution(ctx, run.Execution.ID)
	if err != nil || bound.ProjectionDigest != "projection-after" {
		t.Fatalf("execution projection=%#v err=%v", bound, err)
	}
	plans, err := harness.Repository.Plans(ctx, run.Execution.ID)
	if err != nil || len(plans) != 2 || plans[1].ProjectionDigest != "projection-after" {
		t.Fatalf("replanned projection=%#v err=%v", plans, err)
	}
}

func TestHarnessFanoutBlocksCommitOnChildFailure(t *testing.T) {
	executor := &fakeExecutor{parent: knowledge.ExecutionOutcome{Status: "succeeded"}, child: knowledge.ExecutionOutcome{Status: "failed", FailureClass: "child failure"}}
	harness, store := newHarness(t, executor)
	task := createHarnessTask(t, store, "task-2")
	manifest := runtimeboundary.DefaultCapabilityManifest()
	for i := range manifest.Capabilities {
		if manifest.Capabilities[i].Form == runtimeboundary.FormSubagent {
			manifest.Capabilities[i].Status = runtimeboundary.CapabilityVerified
			manifest.Capabilities[i].Reason = "test-only complete contract fixture"
		}
	}
	harness.Planner = knowledge.NewPlannerWithManifest("planner.v1", runtimeboundary.NewCapabilityManifest(manifest.Capabilities))
	run, err := harness.Admit(context.Background(), AdmissionRequest{ExecutionID: "fanout-1", TaskID: task.ID, TaskRevision: task.Revision, CorrelationID: "corr-2", IdempotencyKey: "fanout-once", Policy: knowledge.DefaultPolicy(knowledge.PolicyRead), Profile: knowledge.TaskProfile{Objective: "split", Complexity: 6, Parallelism: 3, Budget: 3000}, ProjectionDigest: "projection-2"})
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

type batchFakeExecutor struct {
	parent     knowledge.ExecutionOutcome
	children   []knowledge.ExecutionOutcome
	batchCalls int
	childCalls int
}

func (f *batchFakeExecutor) Run(context.Context, knowledge.ExecutionRequest) (knowledge.ExecutionOutcome, error) {
	return f.parent, nil
}

func (f *batchFakeExecutor) RunChild(context.Context, knowledge.ExecutionRequest) (knowledge.ExecutionOutcome, error) {
	f.childCalls++
	return knowledge.ExecutionOutcome{Status: "failed", FailureClass: "unexpected single-child dispatch"}, nil
}

func (f *batchFakeExecutor) RunChildren(_ context.Context, _ knowledge.ExecutionRequest, count int) ([]knowledge.ExecutionOutcome, error) {
	f.batchCalls++
	if count != len(f.children) {
		return nil, fmt.Errorf("batch count=%d want=%d", count, len(f.children))
	}
	return append([]knowledge.ExecutionOutcome(nil), f.children...), nil
}

func TestHarnessUsesNativeBoundedChildBatchBoundary(t *testing.T) {
	executor := &batchFakeExecutor{
		parent: knowledge.ExecutionOutcome{Status: "succeeded", Result: "parent"},
		children: []knowledge.ExecutionOutcome{
			{Status: "succeeded", Result: "child-a", NativeRunID: "fanout-batch-1:child:1"},
			{Status: "succeeded", Result: "child-b", NativeRunID: "fanout-batch-1:child:2"},
		},
	}
	harness, store := newHarness(t, executor)
	task := createHarnessTask(t, store, "task-native-batch")
	manifest := runtimeboundary.DefaultCapabilityManifest()
	for i := range manifest.Capabilities {
		if manifest.Capabilities[i].Form == runtimeboundary.FormSubagent {
			manifest.Capabilities[i].Status = runtimeboundary.CapabilityVerified
			manifest.Capabilities[i].Reason = "test-only complete contract fixture"
		}
	}
	harness.Planner = knowledge.NewPlannerWithManifest("planner.v1", runtimeboundary.NewCapabilityManifest(manifest.Capabilities))
	run, err := harness.Admit(context.Background(), AdmissionRequest{ExecutionID: "fanout-batch-1", TaskID: task.ID, TaskRevision: task.Revision, CorrelationID: "batch-correlation", IdempotencyKey: "fanout-batch-once", Policy: knowledge.DefaultPolicy(knowledge.PolicyRead), Profile: knowledge.TaskProfile{Objective: "split", Complexity: 6, Parallelism: 2, Budget: 2000}, ProjectionDigest: "batch-projection"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := harness.Execute(context.Background(), run.Execution.ID); err != nil {
		t.Fatal(err)
	}
	if executor.batchCalls != 1 || executor.childCalls != 0 {
		t.Fatalf("batch_calls=%d child_calls=%d", executor.batchCalls, executor.childCalls)
	}
	evidence, err := store.Executions().Evidence(context.Background(), run.Execution.ID)
	if err != nil {
		t.Fatal(err)
	}
	children := 0
	for _, item := range evidence {
		if item.ChildRunID != "" {
			children++
		}
	}
	if children != 2 {
		t.Fatalf("child evidence=%#v", evidence)
	}
}

func TestHarnessRejectsUnsupportedModeWithoutDispatch(t *testing.T) {
	executor := &fakeExecutor{parent: knowledge.ExecutionOutcome{Status: "succeeded"}}
	harness, _ := newHarness(t, executor)
	_, err := harness.Admit(context.Background(), AdmissionRequest{TaskID: "task-3", CorrelationID: "corr-3", Policy: knowledge.DefaultPolicy(knowledge.PolicyRead), Profile: knowledge.TaskProfile{Objective: "team", Complexity: 1, Budget: 1000}, RequestedMode: knowledge.ModeTeamParallel})
	var unsupported UnsupportedModeError
	if !errors.As(err, &unsupported) || unsupported.Mode != knowledge.ModeTeamParallel || executor.children != 0 {
		t.Fatalf("error=%v unsupported=%#v", err, unsupported)
	}
}

func TestHarnessPauseResumeAndTaskRevisionReplan(t *testing.T) {
	harness, store := newHarness(t, &fakeExecutor{parent: knowledge.ExecutionOutcome{Status: "succeeded", Result: "resumed"}})
	task := createHarnessTask(t, store, "task-4")
	run, err := harness.Admit(context.Background(), AdmissionRequest{ExecutionID: "pause-1", TaskID: task.ID, TaskRevision: task.Revision, CorrelationID: "corr-4", IdempotencyKey: "pause-once", Policy: knowledge.DefaultPolicy(knowledge.PolicyRead), Profile: knowledge.TaskProfile{Objective: "pause", Complexity: 1, Budget: 1000}, ProjectionDigest: "projection-4"})
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
	if _, err := harness.Resume(context.Background(), run.Execution.ID, knowledge.ResumeRequest{PlanVersion: run.Plan.Version, PlanDigest: run.Plan.Digest, CapabilityDigest: run.Plan.CapabilityDigest, ProjectionDigest: "projection-4"}); !errors.Is(err, ErrRecoveryMismatch) {
		t.Fatalf("legacy resume error=%v", err)
	}
	second, err := harness.Admit(context.Background(), AdmissionRequest{ExecutionID: "pause-2", TaskID: task.ID, TaskRevision: task.Revision, CorrelationID: "corr-4b", IdempotencyKey: "pause-once-2", Policy: knowledge.DefaultPolicy(knowledge.PolicyRead), Profile: knowledge.TaskProfile{Objective: "pause", Complexity: 1, Budget: 1000}, ProjectionDigest: "projection-4"})
	if err != nil {
		t.Fatal(err)
	}
	description := "task revision changed"
	if _, err := store.Tasks().Update(context.Background(), task.ID, task.Revision, storage.TaskPatch{Description: &description}); err != nil {
		t.Fatal(err)
	}
	replanned, err := harness.EnsureTaskRevision(context.Background(), second.Execution.ID, 2, knowledge.TaskProfile{Objective: "changed", Complexity: 2, Parallelism: 1, Budget: 2000}, "task revision changed")
	if err != nil {
		t.Fatal(err)
	}
	if replanned.Version == run.Plan.Version {
		t.Fatalf("replan version did not change: %#v", replanned)
	}
}

func TestHarnessDoesNotResumeLegacyApplicationCheckpoint(t *testing.T) {
	executor := &fakeExecutor{parent: knowledge.ExecutionOutcome{Status: "succeeded"}}
	harness, store := newHarness(t, executor)
	task := createHarnessTask(t, store, "task-legacy-checkpoint")
	run, err := harness.Admit(context.Background(), AdmissionRequest{ExecutionID: "legacy-checkpoint-1", TaskID: task.ID, TaskRevision: task.Revision, CorrelationID: "legacy-checkpoint-correlation", IdempotencyKey: "legacy-checkpoint-once", Policy: knowledge.DefaultPolicy(knowledge.PolicyRead), Profile: knowledge.TaskProfile{Objective: "pause", Complexity: 1, Budget: 1000}, ProjectionDigest: "projection-legacy"})
	if err != nil {
		t.Fatal(err)
	}
	if err := harness.Pause(context.Background(), run.Execution.ID, knowledge.PauseInputRequired, "awaiting-input", []string{"effect:one"}); err != nil {
		t.Fatal(err)
	}
	if _, err := harness.Resume(context.Background(), run.Execution.ID, knowledge.ResumeRequest{PlanVersion: run.Plan.Version, PlanDigest: run.Plan.Digest, CapabilityDigest: run.Plan.CapabilityDigest, ProjectionDigest: "projection-legacy"}); !errors.Is(err, ErrRecoveryMismatch) {
		t.Fatalf("legacy checkpoint resume error=%v", err)
	}
	if executor.parentCalls != 0 {
		t.Fatalf("legacy checkpoint dispatched native execution %d times", executor.parentCalls)
	}
}

func TestHarnessBindsNativeCheckpointAuthority(t *testing.T) {
	ctx := context.Background()
	harness, store := newHarness(t, &fakeExecutor{parent: knowledge.ExecutionOutcome{Status: "succeeded"}})
	task := createHarnessTask(t, store, "task-native-checkpoint")
	run, err := harness.Admit(ctx, AdmissionRequest{ExecutionID: "native-checkpoint-1", TaskID: task.ID, TaskRevision: task.Revision, CorrelationID: "native-checkpoint-correlation", IdempotencyKey: "native-checkpoint-once", Policy: knowledge.DefaultPolicy(knowledge.PolicyRead), Profile: knowledge.TaskProfile{Objective: "pause", Complexity: 1, Budget: 1000}, ProjectionDigest: "projection-native"})
	if err != nil {
		t.Fatal(err)
	}
	binding := runtimeboundary.CheckpointBinding{Version: runtimeboundary.ExecutionContractV1, ExecutionID: run.Execution.ID, NativeID: "native-run-1", NativeVersion: "checkpoint.v1", NativeDigest: "checkpoint-digest", SourceCursor: "cursor-10", PauseReason: string(knowledge.PauseApprovalRequired), Authority: runtimeboundary.Authority{TaskRevision: task.Revision, PlanVersion: run.Plan.Version, PlanDigest: run.Plan.Digest, CapabilityDigest: run.Plan.CapabilityDigest, ProjectionDigest: run.Execution.ProjectionDigest, WriteScope: "none", EffectNamespace: "task:native-checkpoint"}}
	if err := harness.PauseNative(ctx, binding, "before-tool", []string{"effect:one"}); err != nil {
		t.Fatal(err)
	}
	checkpoint, err := store.Executions().GetCheckpoint(ctx, run.Execution.ID)
	if err != nil || checkpoint.ContractVersion != storage.NativeCheckpointContract || checkpoint.NativeCheckpointID != binding.NativeID || string(checkpoint.CompletedEffectKeys) != `["effect:one"]` {
		t.Fatalf("native checkpoint=%#v err=%v", checkpoint, err)
	}
	var storedBinding, storedCheckpoint string
	if err := store.DB().QueryRowContext(ctx, `SELECT contract_version, native_checkpoint_id FROM execution_authority_bindings WHERE execution_id=?`, run.Execution.ID).Scan(&storedBinding, &storedCheckpoint); err != nil || storedBinding != storage.NativeCheckpointContract || storedCheckpoint != binding.NativeID {
		t.Fatalf("authority binding=%q checkpoint=%q err=%v", storedBinding, storedCheckpoint, err)
	}
}

func TestHarnessDoesNotRestartNativeCheckpointWithoutRestoreAdapter(t *testing.T) {
	executor := &fakeExecutor{parent: knowledge.ExecutionOutcome{Status: "succeeded"}}
	harness, store := newHarness(t, executor)
	task := createHarnessTask(t, store, "task-native-resume")
	run, err := harness.Admit(context.Background(), AdmissionRequest{ExecutionID: "native-resume-1", TaskID: task.ID, TaskRevision: task.Revision, CorrelationID: "native-resume-correlation", IdempotencyKey: "native-resume-once", Policy: knowledge.DefaultPolicy(knowledge.PolicyRead), Profile: knowledge.TaskProfile{Objective: "pause", Complexity: 1, Budget: 1000}, ProjectionDigest: "projection-native-resume"})
	if err != nil {
		t.Fatal(err)
	}
	binding := runtimeboundary.CheckpointBinding{Version: runtimeboundary.ExecutionContractV1, ExecutionID: run.Execution.ID, NativeID: "native-run-resume", NativeVersion: "checkpoint.v1", NativeDigest: "checkpoint-digest-resume", PauseReason: string(knowledge.PauseInputRequired), Authority: runtimeboundary.Authority{TaskRevision: task.Revision, PlanVersion: run.Plan.Version, PlanDigest: run.Plan.Digest, CapabilityDigest: run.Plan.CapabilityDigest, ProjectionDigest: run.Execution.ProjectionDigest, WriteScope: "none", EffectNamespace: "task:native-resume"}}
	if err := harness.PauseNative(context.Background(), binding, "awaiting-input", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := harness.Resume(context.Background(), run.Execution.ID, knowledge.ResumeRequest{PlanVersion: run.Plan.Version, PlanDigest: run.Plan.Digest, CapabilityDigest: run.Plan.CapabilityDigest, ProjectionDigest: run.Execution.ProjectionDigest}); !errors.Is(err, ErrRecoveryMismatch) {
		t.Fatalf("native checkpoint restart error=%v", err)
	}
	if executor.parentCalls != 0 {
		t.Fatalf("native checkpoint restarted from beginning %d times", executor.parentCalls)
	}
}

func TestHarnessResumesNativeCheckpointThroughAdapter(t *testing.T) {
	ctx := context.Background()
	executor := &fakeExecutor{parent: knowledge.ExecutionOutcome{Status: "succeeded"}}
	harness, store := newHarness(t, executor)
	continuation := &fakeNativeContinuation{out: runtimeboundary.NativeResumeOutcome{ExecutionID: "native-harness-resume", NativeRunID: "native-harness-resume", Status: "succeeded", Result: "restored"}}
	harness.Continuation = continuation
	task := createHarnessTask(t, store, "task-native-adapter-resume")
	run, err := harness.Admit(ctx, AdmissionRequest{ExecutionID: "native-adapter-resume-1", TaskID: task.ID, TaskRevision: task.Revision, CorrelationID: "native-adapter-correlation", IdempotencyKey: "native-adapter-once", Policy: knowledge.DefaultPolicy(knowledge.PolicyRead), Profile: knowledge.TaskProfile{Objective: "pause", Complexity: 1, Budget: 1000}, ProjectionDigest: "projection-native-adapter"})
	if err != nil {
		t.Fatal(err)
	}
	binding := runtimeboundary.CheckpointBinding{Version: runtimeboundary.ExecutionContractV1, ExecutionID: run.Execution.ID, NativeID: "native-harness-resume", NativeVersion: "native_continuation.v1", NativeDigest: "checkpoint-digest-harness", DecisionToken: "opaque-token", PauseReason: string(knowledge.PauseInputRequired), Authority: runtimeboundary.Authority{TaskRevision: task.Revision, PlanVersion: run.Plan.Version, PlanDigest: run.Plan.Digest, CapabilityDigest: run.Plan.CapabilityDigest, ProjectionDigest: run.Execution.ProjectionDigest, WriteScope: "none", EffectNamespace: "task:native-adapter"}}
	if err := harness.PauseNative(ctx, binding, "awaiting-input", []string{"effect:completed"}); err != nil {
		t.Fatal(err)
	}
	resumed, err := harness.Resume(ctx, run.Execution.ID, knowledge.ResumeRequest{PlanVersion: run.Plan.Version, PlanDigest: run.Plan.Digest, CapabilityDigest: run.Plan.CapabilityDigest, ProjectionDigest: run.Execution.ProjectionDigest, DecisionToken: "opaque-token", IdempotencyKey: "resume-once"})
	if err != nil || resumed.Execution.Status != storage.ExecutionSucceeded || resumed.Outcome.Result != "restored" {
		t.Fatalf("resumed=%#v err=%v", resumed, err)
	}
	if continuation.called != 1 || continuation.last.Checkpoint.CheckpointID != binding.NativeID || continuation.last.DecisionToken != binding.DecisionToken || continuation.last.Authority.PlanDigest != run.Plan.Digest {
		t.Fatalf("native request=%#v calls=%d", continuation.last, continuation.called)
	}
	if executor.parentCalls != 0 {
		t.Fatalf("resume reran parent execution %d times", executor.parentCalls)
	}
}

func TestHarnessAuthorityExpansionPausesBeforeDispatch(t *testing.T) {
	harness, store := newHarness(t, &fakeExecutor{parent: knowledge.ExecutionOutcome{Status: "succeeded"}})
	task := createHarnessTask(t, store, "task-5")
	run, err := harness.Admit(context.Background(), AdmissionRequest{ExecutionID: "authority-1", TaskID: task.ID, TaskRevision: task.Revision, CorrelationID: "corr-5", IdempotencyKey: "authority-once", Policy: knowledge.DefaultPolicy(knowledge.PolicyRead), Profile: knowledge.TaskProfile{Objective: "read", Complexity: 1, Budget: 100}, ProjectionDigest: "projection-5"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := harness.Replan(context.Background(), run.Execution.ID, knowledge.TaskProfile{Objective: "write", Complexity: 2, Parallelism: 1, Budget: 2000, WriteScope: 2}, "authority expansion"); err != nil {
		t.Fatal(err)
	}
	execution, err := store.Executions().GetExecution(context.Background(), run.Execution.ID)
	if err != nil || execution.Status != storage.ExecutionPaused {
		t.Fatalf("execution=%#v err=%v", execution, err)
	}
}

func TestHarnessPersistsUncertainDispatchForRecovery(t *testing.T) {
	harness, store := newHarness(t, &fakeExecutor{err: ErrDispatchUncertain})
	task := createHarnessTask(t, store, "task-6")
	run, err := harness.Admit(context.Background(), AdmissionRequest{ExecutionID: "uncertain-1", TaskID: task.ID, TaskRevision: task.Revision, CorrelationID: "corr-6", IdempotencyKey: "uncertain-once", Policy: knowledge.DefaultPolicy(knowledge.PolicyRead), Profile: knowledge.TaskProfile{Objective: "dispatch", Budget: 1000}})
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

func TestHarnessReconcilesUncertainDispatchFromNativeTerminalWithoutRerun(t *testing.T) {
	ctx := context.Background()
	executor := &fakeExecutor{parent: knowledge.ExecutionOutcome{Status: "succeeded"}}
	harness, store := newHarness(t, executor)
	recovery := &fakeNativeRecovery{out: runtimeboundary.RecoveryOutcome{Status: "terminal", NativeRunID: "native-recovered-run", NativeEventID: "native-recovered-terminal", NativeVersion: runtimeboundary.ExecutionContractV1, SourceCursor: "cursor-2", SourceSequence: 2, ObservationScope: "recovery-scope", TerminalStatus: "succeeded", Result: "source result"}}
	harness.Recovery = recovery
	task := createHarnessTask(t, store, "task-reconcile-terminal")
	run, err := harness.Admit(ctx, AdmissionRequest{ExecutionID: "reconcile-terminal-1", TaskID: task.ID, TaskRevision: task.Revision, CorrelationID: "reconcile-terminal-correlation", IdempotencyKey: "reconcile-terminal-admission", Policy: knowledge.DefaultPolicy(knowledge.PolicyRead), Profile: knowledge.TaskProfile{Objective: "uncertain", Complexity: 1, Budget: 1000}, ProjectionDigest: "projection-reconcile-terminal"})
	if err != nil {
		t.Fatal(err)
	}
	uncertain, err := store.Executions().UpdateExecution(ctx, run.Execution.ID, run.Execution.Revision, storage.ExecutionRecoveryRequired, nil, []byte("dispatch uncertain"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := harness.Reconcile(ctx, uncertain.ID)
	if err != nil || result.Execution.Status != storage.ExecutionSucceeded || recovery.called != 1 || executor.parentCalls != 0 {
		t.Fatalf("reconciled=%#v recovery_calls=%d parent_calls=%d err=%v", result, recovery.called, executor.parentCalls, err)
	}
	if recovery.last.NativeRunID != uncertain.ID || recovery.last.ExecutionID != uncertain.ID {
		t.Fatalf("recovery request=%#v", recovery.last)
	}
	evidence, err := store.Executions().Evidence(ctx, uncertain.ID)
	if err != nil || len(evidence) != 1 || evidence[0].NativeSourceID != "native-recovered-run" || evidence[0].NativeEventID != "native-recovered-terminal" || string(evidence[0].Result) != "source result" {
		t.Fatalf("recovered evidence=%#v err=%v", evidence, err)
	}
}

func TestHarnessReconcilesUnboundNativeCheckpointWithoutInventingOrRerunning(t *testing.T) {
	ctx := context.Background()
	executor := &fakeExecutor{parent: knowledge.ExecutionOutcome{Status: "succeeded"}}
	harness, store := newHarness(t, executor)
	task := createHarnessTask(t, store, "task-reconcile-checkpoint")
	run, err := harness.Admit(ctx, AdmissionRequest{ExecutionID: "reconcile-checkpoint-1", TaskID: task.ID, TaskRevision: task.Revision, CorrelationID: "reconcile-checkpoint-correlation", IdempotencyKey: "reconcile-checkpoint-admission", Policy: knowledge.DefaultPolicy(knowledge.PolicyRead), Profile: knowledge.TaskProfile{Objective: "uncertain", Complexity: 1, Budget: 1000}, ProjectionDigest: "projection-reconcile-checkpoint"})
	if err != nil {
		t.Fatal(err)
	}
	checkpoint := runtimeboundary.CheckpointReference{Version: "checkpoint.v1", CheckpointID: "native-checkpoint-recovered", Digest: "checkpoint-digest-recovered", SourceComponent: "runner", OwnerNamespace: "danny.baymax.runner.history.v1", RunID: "native-checkpoint-run", SessionID: run.Execution.CorrelationID, PauseReason: string(knowledge.PauseInputRequired), SourcePosition: "cursor-checkpoint"}
	recovery := &fakeNativeRecovery{out: runtimeboundary.RecoveryOutcome{Status: "checkpoint", Checkpoint: &checkpoint}}
	harness.Recovery = recovery
	uncertain, err := store.Executions().UpdateExecution(ctx, run.Execution.ID, run.Execution.Revision, storage.ExecutionRecoveryRequired, nil, []byte("checkpoint binding uncertain"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := harness.Reconcile(ctx, uncertain.ID); err != nil {
		t.Fatal(err)
	}
	current, err := store.Executions().GetExecution(ctx, uncertain.ID)
	if err != nil || current.Status != storage.ExecutionPaused || executor.parentCalls != 0 || recovery.called != 1 {
		t.Fatalf("execution=%#v parent_calls=%d recovery_calls=%d err=%v", current, executor.parentCalls, recovery.called, err)
	}
	stored, err := store.Executions().GetCheckpoint(ctx, uncertain.ID)
	if err != nil || stored.NativeCheckpointID != checkpoint.CheckpointID || stored.NativeCheckpointDigest != checkpoint.Digest {
		t.Fatalf("stored checkpoint=%#v err=%v", stored, err)
	}
}

func TestHarnessReconcileReturnsTypedGapAndKeepsRecoveryRequired(t *testing.T) {
	ctx := context.Background()
	harness, store := newHarness(t, &fakeExecutor{parent: knowledge.ExecutionOutcome{Status: "succeeded"}})
	task := createHarnessTask(t, store, "task-reconcile-gap")
	run, err := harness.Admit(ctx, AdmissionRequest{ExecutionID: "reconcile-gap-1", TaskID: task.ID, TaskRevision: task.Revision, CorrelationID: "reconcile-gap-correlation", IdempotencyKey: "reconcile-gap-admission", Policy: knowledge.DefaultPolicy(knowledge.PolicyRead), Profile: knowledge.TaskProfile{Objective: "uncertain", Complexity: 1, Budget: 1000}, ProjectionDigest: "projection-reconcile-gap"})
	if err != nil {
		t.Fatal(err)
	}
	harness.Recovery = &fakeNativeRecovery{out: runtimeboundary.RecoveryOutcome{Status: "gap"}}
	uncertain, err := store.Executions().UpdateExecution(ctx, run.Execution.ID, run.Execution.Revision, storage.ExecutionRecoveryRequired, nil, []byte("history unavailable"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := harness.Reconcile(ctx, uncertain.ID); !errors.Is(err, ErrRecoveryGap) {
		t.Fatalf("reconcile error=%v, want recovery gap", err)
	}
	current, err := store.Executions().GetExecution(ctx, uncertain.ID)
	if err != nil || current.Status != storage.ExecutionRecoveryRequired {
		t.Fatalf("execution after gap=%#v err=%v", current, err)
	}
}

func TestHarnessReconcileAfterProjectionCommitDoesNotDuplicateSourceEvidence(t *testing.T) {
	ctx := context.Background()
	harness, store := newHarness(t, &fakeExecutor{parent: knowledge.ExecutionOutcome{Status: "succeeded"}})
	task := createHarnessTask(t, store, "task-reconcile-projected")
	run, err := harness.Admit(ctx, AdmissionRequest{ExecutionID: "reconcile-projected-1", TaskID: task.ID, TaskRevision: task.Revision, CorrelationID: "reconcile-projected-correlation", IdempotencyKey: "reconcile-projected-admission", Policy: knowledge.DefaultPolicy(knowledge.PolicyRead), Profile: knowledge.TaskProfile{Objective: "uncertain", Complexity: 1, Budget: 1000}, ProjectionDigest: "projection-reconcile-projected"})
	if err != nil {
		t.Fatal(err)
	}
	uncertain, err := store.Executions().UpdateExecution(ctx, run.Execution.ID, run.Execution.Revision, storage.ExecutionRecoveryRequired, nil, []byte("product commit uncertain"))
	if err != nil {
		t.Fatal(err)
	}
	sequence := int64(4)
	base := storage.ExecutionEvidenceRecord{ExecutionID: uncertain.ID, EffectKey: uncertain.ID + ":recovery:native-terminal-4", ParentRunID: uncertain.ParentRunID, PlanVersion: uncertain.PlanVersion, CapabilityDigest: uncertain.CapabilityDigest, ProjectionDigest: uncertain.ProjectionDigest, ContractVersion: storage.NativeEvidenceContractV1, NativeSourceKind: "runner", NativeSourceNamespace: "danny.baymax.runner.history.v1", NativeSourceID: "native-recovered-run-4", NativeEventID: "native-terminal-4", NativeVersion: runtimeboundary.ExecutionContractV1, NativeSequence: &sequence, ObservationScope: "recovery-projected-scope", Status: []byte("succeeded"), Result: []byte("already projected")}
	if _, err := store.Executions().ProjectNativeEvidence(ctx, base, storage.ExecutionSourceCursor{ExecutionID: uncertain.ID, SourceKind: base.NativeSourceKind, SourceNamespace: base.NativeSourceNamespace, SourceID: base.NativeSourceID, CursorVersion: runtimeboundary.ExecutionContractV1, CursorValue: "cursor-4", SourceSequence: sequence, ContractVersion: storage.NativeEvidenceContractV1}); err != nil {
		t.Fatal(err)
	}
	harness.Recovery = &fakeNativeRecovery{out: runtimeboundary.RecoveryOutcome{Status: runtimeboundary.RecoveryTerminal, NativeRunID: base.NativeSourceID, NativeEventID: base.NativeEventID, NativeVersion: base.NativeVersion, SourceSequence: sequence, SourceCursor: "cursor-4", ObservationScope: base.ObservationScope, TerminalStatus: "succeeded", Result: "already projected"}}
	result, err := harness.Reconcile(ctx, uncertain.ID)
	if err != nil || result.Execution.Status != storage.ExecutionSucceeded {
		t.Fatalf("reconciled=%#v err=%v", result, err)
	}
	evidence, err := store.Executions().Evidence(ctx, uncertain.ID)
	if err != nil || len(evidence) != 1 || string(evidence[0].Result) != "already projected" {
		t.Fatalf("evidence after replay=%#v err=%v", evidence, err)
	}
}

func TestHarnessControlAcceptanceIsSeparateFromTerminalCompletionAndIdempotent(t *testing.T) {
	ctx := context.Background()
	harness, store := newHarness(t, &fakeExecutor{parent: knowledge.ExecutionOutcome{Status: "succeeded"}})
	control := &fakeNativeControl{out: runtimeboundary.ControlOutcome{Status: "accepted", TerminalObserved: false}}
	harness.Control = control
	task := createHarnessTask(t, store, "task-control-acceptance")
	run, err := harness.Admit(ctx, AdmissionRequest{ExecutionID: "control-acceptance-1", TaskID: task.ID, TaskRevision: task.Revision, CorrelationID: "control-correlation", IdempotencyKey: "control-admission", Policy: knowledge.DefaultPolicy(knowledge.PolicyRead), Profile: knowledge.TaskProfile{Objective: "wait", Complexity: 1, Budget: 1000}, ProjectionDigest: "projection-control"})
	if err != nil {
		t.Fatal(err)
	}
	dispatching, err := store.Executions().UpdateExecution(ctx, run.Execution.ID, run.Execution.Revision, storage.ExecutionDispatching, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	running, err := store.Executions().UpdateExecution(ctx, run.Execution.ID, dispatching.Revision, storage.ExecutionRunning, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	out, err := harness.Cancel(ctx, running.ID, "native-control-run", "cancel-control-1")
	if err != nil || out.Status != "accepted" || out.TerminalObserved {
		t.Fatalf("cancel outcome=%#v err=%v", out, err)
	}
	current, err := store.Executions().GetExecution(ctx, running.ID)
	if err != nil || current.Status != storage.ExecutionRunning {
		t.Fatalf("execution after accepted cancel=%#v err=%v", current, err)
	}
	duplicate, err := harness.Cancel(ctx, running.ID, "native-control-run", "cancel-control-1")
	if err != nil || duplicate.Status != "accepted" || control.called != 1 {
		t.Fatalf("duplicate cancel outcome=%#v calls=%d err=%v", duplicate, control.called, err)
	}
	evidence, err := store.Executions().Evidence(ctx, running.ID)
	if err != nil || len(evidence) != 1 || string(evidence[0].Status) != "control_accepted" || evidence[0].EffectKey != running.ID+":control:cancel:cancel-control-1" {
		t.Fatalf("control evidence=%#v err=%v", evidence, err)
	}
}

func TestHarnessTimeoutUsesDistinctStableControlEffect(t *testing.T) {
	ctx := context.Background()
	harness, store := newHarness(t, &fakeExecutor{parent: knowledge.ExecutionOutcome{Status: "succeeded"}})
	harness.Control = &fakeNativeControl{out: runtimeboundary.ControlOutcome{Status: "accepted"}}
	task := createHarnessTask(t, store, "task-timeout-control")
	run, err := harness.Admit(ctx, AdmissionRequest{ExecutionID: "timeout-control-1", TaskID: task.ID, TaskRevision: task.Revision, CorrelationID: "timeout-correlation", IdempotencyKey: "timeout-admission", Policy: knowledge.DefaultPolicy(knowledge.PolicyRead), Profile: knowledge.TaskProfile{Objective: "wait", Complexity: 1, Budget: 1000}, ProjectionDigest: "projection-timeout"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := harness.Timeout(ctx, run.Execution.ID, "native-timeout-run", "timeout-1"); err != nil {
		t.Fatal(err)
	}
	evidence, err := store.Executions().Evidence(ctx, run.Execution.ID)
	if err != nil || len(evidence) != 1 || evidence[0].EffectKey != run.Execution.ID+":control:timeout:timeout-1" {
		t.Fatalf("timeout evidence=%#v err=%v", evidence, err)
	}
	current, err := store.Executions().GetExecution(ctx, run.Execution.ID)
	if err != nil || current.Status != storage.ExecutionAdmitted {
		t.Fatalf("timeout changed execution state=%#v err=%v", current, err)
	}
}

func TestHarnessRetryUsesNewNativeRunAndPreservesPriorTerminalEvidence(t *testing.T) {
	ctx := context.Background()
	harness, store := newHarness(t, &fakeExecutor{parent: knowledge.ExecutionOutcome{Status: "succeeded"}})
	retry := &fakeNativeRetry{out: runtimeboundary.RetryOutcome{ExecutionID: "retry-1", NativeRunID: "retry-1:retry:retry-1", CausationID: "native-failed-run", Status: "succeeded", Result: "recovered"}}
	harness.RetryAdapter = retry
	task := createHarnessTask(t, store, "task-retry")
	run, err := harness.Admit(ctx, AdmissionRequest{ExecutionID: "retry-1", TaskID: task.ID, TaskRevision: task.Revision, CorrelationID: "retry-correlation", IdempotencyKey: "retry-admission", Policy: knowledge.DefaultPolicy(knowledge.PolicyRead), Profile: knowledge.TaskProfile{Objective: "retry", Complexity: 1, Budget: 1000}, ProjectionDigest: "projection-retry"})
	if err != nil {
		t.Fatal(err)
	}
	failed, err := store.Executions().UpdateExecution(ctx, run.Execution.ID, run.Execution.Revision, storage.ExecutionDispatching, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	failed, err = store.Executions().UpdateExecution(ctx, failed.ID, failed.Revision, storage.ExecutionRunning, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	failed, err = store.Executions().UpdateExecution(ctx, failed.ID, failed.Revision, storage.ExecutionFailed, []byte("old-result"), []byte("old-failure"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Executions().RecordEvidence(ctx, storage.ExecutionEvidenceRecord{ExecutionID: failed.ID, EffectKey: failed.ID + ":parent", ParentRunID: "native-failed-run", PlanVersion: failed.PlanVersion, CapabilityDigest: failed.CapabilityDigest, ProjectionDigest: failed.ProjectionDigest, Status: []byte("failed"), FailureClass: []byte("old-failure"), Result: []byte("old-result")}); err != nil {
		t.Fatal(err)
	}
	result, err := harness.Retry(ctx, failed.ID, "native-failed-run", "recovered input", "retry-1")
	if err != nil || result.Execution.Status != storage.ExecutionSucceeded || retry.called != 1 {
		t.Fatalf("retry result=%#v calls=%d err=%v", result, retry.called, err)
	}
	if retry.last.NativeRunID != failed.ID+":retry:retry-1" || retry.last.CausationID != "native-failed-run" || retry.last.IdempotencyKey != "retry-1" {
		t.Fatalf("retry request=%#v", retry.last)
	}
	evidence, err := store.Executions().Evidence(ctx, failed.ID)
	if err != nil || len(evidence) != 2 {
		t.Fatalf("retry evidence=%#v err=%v", evidence, err)
	}
	var prior, retried storage.ExecutionEvidenceRecord
	for _, item := range evidence {
		switch item.EffectKey {
		case failed.ID + ":parent":
			prior = item
		case failed.ID + ":retry:retry-1":
			retried = item
		}
	}
	if string(prior.Status) != "failed" || string(prior.Result) != "old-result" || retried.NativeSourceID != failed.ID+":retry:retry-1" || string(retried.Status) != "succeeded" {
		t.Fatalf("prior=%#v retried=%#v", prior, retried)
	}
	duplicate, err := harness.Retry(ctx, failed.ID, "native-failed-run", "changed input", "retry-1")
	if err != nil || retry.called != 1 || duplicate.Execution.Status != storage.ExecutionSucceeded {
		t.Fatalf("duplicate retry=%#v calls=%d err=%v", duplicate, retry.called, err)
	}
}

func TestHarnessTerminalEvidenceRetainsNativeRunIdentity(t *testing.T) {
	ctx := context.Background()
	executor := &fakeExecutor{parent: knowledge.ExecutionOutcome{Status: "failed", NativeRunID: "native-failed-terminal", FailureClass: "provider"}}
	harness, store := newHarness(t, executor)
	task := createHarnessTask(t, store, "task-native-terminal")
	run, err := harness.Admit(ctx, AdmissionRequest{ExecutionID: "native-terminal-1", TaskID: task.ID, TaskRevision: task.Revision, CorrelationID: "native-terminal-correlation", IdempotencyKey: "native-terminal-admission", Policy: knowledge.DefaultPolicy(knowledge.PolicyRead), Profile: knowledge.TaskProfile{Objective: "fail", Complexity: 1, Budget: 1000}, ProjectionDigest: "projection-native-terminal"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := harness.Execute(ctx, run.Execution.ID); !errors.Is(err, ErrCommitBlocked) {
		t.Fatalf("execute error=%v", err)
	}
	evidence, err := store.Executions().Evidence(ctx, run.Execution.ID)
	if err != nil || len(evidence) != 1 || evidence[0].NativeSourceID != "native-failed-terminal" || evidence[0].NativeEventID != "native-failed-terminal:terminal" {
		t.Fatalf("native terminal evidence=%#v err=%v", evidence, err)
	}
}
