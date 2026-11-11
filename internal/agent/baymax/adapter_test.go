package baymax

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/FelixSeptem/baymax/core/history"
	"github.com/FelixSeptem/baymax/core/runner"
	"github.com/FelixSeptem/baymax/core/types"
	"github.com/FelixSeptem/baymax/integration/fakes"
	"github.com/FelixSeptem/baymax/orchestration/composer"
	"github.com/FelixSeptem/baymax/orchestration/teams"
	"github.com/FelixSeptem/baymax/orchestration/workflow"
	"github.com/FelixSeptem/danny/internal/runtimeboundary"
	"github.com/FelixSeptem/danny/internal/runtimeprobe"
)

func TestPinnedSourceHistoryOwnersPreserveIdentityOrderAndFileReplay(t *testing.T) {
	owners := []struct {
		kind types.HistorySourceKind
		open func(string, types.SourceRef, ...history.Option) (*history.Journal, error)
	}{
		{kind: types.HistorySourceRunner, open: runner.OpenNativeEventHistory},
		{kind: types.HistorySourceChild, open: composer.OpenNativeEventHistory},
		{kind: types.HistorySourceTeam, open: teams.OpenNativeEventHistory},
		{kind: types.HistorySourceWorkflow, open: workflow.OpenNativeEventHistory},
	}
	for _, owner := range owners {
		t.Run(string(owner.kind), func(t *testing.T) {
			source := types.SourceRef{Kind: owner.kind, Namespace: "history.v1", ID: string(owner.kind) + "-run"}
			path := filepath.Join(t.TempDir(), "history.json")
			journal, err := owner.open(path, source, history.WithRetention(2))
			if err != nil {
				t.Fatal(err)
			}
			initial, err := journal.ReadAfter(context.Background(), source, nil, 2)
			if err != nil || initial.NextCursor == nil {
				t.Fatalf("initial source cursor: page=%+v err=%v", initial, err)
			}
			for i := 0; i < 3; i++ {
				_, err := journal.Append(context.Background(), types.NativeExecutionEvent{
					Version:    types.NativeExecutionEventVersionV1,
					NativeType: "run.started",
					Event:      types.EventEnvelope{RunID: source.ID, Payload: map[string]any{"discard": "payload"}},
				})
				if err != nil {
					t.Fatal(err)
				}
			}
			caps, err := journal.Capabilities(context.Background(), source)
			if err != nil || !caps.Supported || !caps.SequenceDefined || !caps.Durable || caps.RetentionFloor == nil {
				t.Fatalf("file journal capabilities: %+v err=%v", caps, err)
			}
			if _, err := journal.ReadAfter(context.Background(), source, initial.NextCursor, 2); !errors.Is(err, types.ErrCursorExpired) {
				t.Fatalf("expected retention-floor rejection, got %v", err)
			}
			floor, err := journal.ReadAfter(context.Background(), source, caps.RetentionFloor, 2)
			if err != nil || len(floor.Events) != 2 {
				t.Fatalf("read from retention floor: page=%+v err=%v", floor, err)
			}
			if floor.Events[0].History.Source != source || floor.Events[0].History.Sequence != 2 || floor.Events[1].History.Sequence != 3 {
				t.Fatalf("source identity/order mismatch: %+v", floor.Events)
			}
			if floor.Events[0].Event.Payload != nil {
				t.Fatalf("journal retained event payload: %+v", floor.Events[0].Event.Payload)
			}
			reopened, err := owner.open(path, source, history.WithRetention(2))
			if err != nil {
				t.Fatal(err)
			}
			replayed, err := reopened.ReadAfter(context.Background(), source, caps.RetentionFloor, 2)
			if err != nil || len(replayed.Events) != 2 || replayed.Events[1].History.Sequence != 3 {
				t.Fatalf("same-host sequential reopen: page=%+v err=%v", replayed, err)
			}
		})
	}
}

func TestClientRunsPinnedBaymaxRunnerWithFakeModel(t *testing.T) {
	model := fakes.NewModel([]fakes.ModelStep{{Response: types.ModelResponse{FinalAnswer: "ok"}}})
	client, err := New(model)
	if err != nil {
		t.Fatal(err)
	}
	out, err := client.Run(context.Background(), runtimeprobe.ProbeRequest{CorrelationID: "corr-1", Input: "hello", Capability: "run"})
	if err != nil {
		t.Fatal(err)
	}
	bundle := runtimeprobe.Bundle{SchemaVersion: runtimeprobe.EvidenceSchemaVersion, BaymaxVersion: client.Version(), SteleVersion: "unconfigured", Events: out.Events}
	if err := bundle.Validate(); err != nil {
		t.Fatalf("evidence invalid: %v", err)
	}
	if len(out.Events) != 2 || out.Events[1].Status != runtimeprobe.StatusPassed {
		t.Fatalf("unexpected events: %#v", out.Events)
	}
}

func TestClientRunsTypedLocalToolLifecycle(t *testing.T) {
	model := fakes.NewModel([]fakes.ModelStep{
		{Response: types.ModelResponse{ToolCalls: []types.ToolCall{{CallID: "call-1", Name: "local.probe", Args: map[string]any{}}}}},
		{Response: types.ModelResponse{FinalAnswer: "tool complete"}},
	})
	client, err := New(model)
	if err != nil {
		t.Fatal(err)
	}
	out, err := client.Run(context.Background(), runtimeprobe.ProbeRequest{CorrelationID: "tool-corr", Capability: "run", Input: "use the tool"})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Events) != 2 || out.Events[1].Status != runtimeprobe.StatusPassed {
		t.Fatalf("unexpected tool evidence: %#v", out.Events)
	}
}

func TestClientRunAndStreamAgreeOnTerminalStatus(t *testing.T) {
	client, err := New(fakes.NewModel(nil))
	if err != nil {
		t.Fatal(err)
	}
	run, err := client.Run(context.Background(), runtimeprobe.ProbeRequest{CorrelationID: "run-corr", Capability: "run", Input: "same"})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := client.Stream(context.Background(), runtimeprobe.ProbeRequest{CorrelationID: "stream-corr", Capability: "stream", Input: "same"})
	if err != nil {
		t.Fatal(err)
	}
	if run.Events[len(run.Events)-1].Status != stream.Events[len(stream.Events)-1].Status {
		t.Fatalf("run/stream status mismatch: %v vs %v", run.Events[len(run.Events)-1].Status, stream.Events[len(stream.Events)-1].Status)
	}
}

func TestClientRunsSchedulerAndContextAssemblerProbes(t *testing.T) {
	client, err := New(fakes.NewModel(nil))
	if err != nil {
		t.Fatal(err)
	}
	schedulerOut, err := client.Scheduler(context.Background(), runtimeprobe.ProbeRequest{CorrelationID: "scheduler-corr", Capability: "scheduler", Input: "input"})
	if err != nil {
		t.Fatal(err)
	}
	if got := schedulerOut.Events[1].Payload; got["delayed"] != "true" || got["pre_dispatch_claim"] != "false" || got["lease"] != "claimed" || got["terminal"] != "succeeded" {
		t.Fatalf("delayed scheduler evidence missing: %#v", got)
	}
	schedulerBundle := runtimeprobe.Bundle{SchemaVersion: runtimeprobe.EvidenceSchemaVersion, BaymaxVersion: client.Version(), SteleVersion: "stele", Events: schedulerOut.Events}
	if err := schedulerBundle.Validate(); err != nil {
		t.Fatalf("scheduler evidence invalid: %v", err)
	}

	contextOut, err := client.ContextAssembly(context.Background(), runtimeprobe.ProbeRequest{CorrelationID: "context-corr", Capability: "context", Input: "input"})
	if err != nil {
		t.Fatal(err)
	}
	contextPayload := contextOut.Events[1].Payload
	if contextPayload["prefix_guard"] == "" || contextPayload["handoff_digest"] == "" || contextPayload["derived_budget"] != "2048" {
		t.Fatalf("context projection evidence missing: %#v", contextPayload)
	}
	contextBundle := runtimeprobe.Bundle{SchemaVersion: runtimeprobe.EvidenceSchemaVersion, BaymaxVersion: client.Version(), SteleVersion: "stele", Events: contextOut.Events}
	if err := contextBundle.Validate(); err != nil {
		t.Fatalf("context evidence invalid: %v", err)
	}

	for _, call := range []func(context.Context, runtimeprobe.ProbeRequest) (runtimeprobe.ProbeOutcome, error){client.Scheduler, client.ContextAssembly} {
		out, err := call(context.Background(), runtimeprobe.ProbeRequest{CorrelationID: "probe-corr", Capability: "probe", Input: "input"})
		if err != nil {
			t.Fatal(err)
		}
		bundle := runtimeprobe.Bundle{SchemaVersion: runtimeprobe.EvidenceSchemaVersion, BaymaxVersion: client.Version(), SteleVersion: "stele", Events: out.Events}
		if err := bundle.Validate(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestClientRequiresModel(t *testing.T) {
	if _, err := New(nil); err == nil {
		t.Fatal("New(nil) unexpectedly succeeded")
	}
}

func TestClientRunsBaymaxTeamParallelAndVotePrimitives(t *testing.T) {
	client, err := New(fakes.NewModel(nil))
	if err != nil {
		t.Fatal(err)
	}
	for _, call := range []func(context.Context, runtimeprobe.ProbeRequest) (runtimeprobe.ProbeOutcome, error){client.TeamParallel, client.TeamVote} {
		out, err := call(context.Background(), runtimeprobe.ProbeRequest{CorrelationID: "team-corr", Capability: "team"})
		if err != nil {
			t.Fatal(err)
		}
		bundle := runtimeprobe.Bundle{SchemaVersion: runtimeprobe.EvidenceSchemaVersion, BaymaxVersion: client.Version(), SteleVersion: "stele", Events: out.Events}
		if err := bundle.Validate(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestClientRunsBaymaxMailboxLifecycle(t *testing.T) {
	client, err := New(fakes.NewModel(nil))
	if err != nil {
		t.Fatal(err)
	}
	out, err := client.Mailbox(context.Background(), runtimeprobe.ProbeRequest{CorrelationID: "mail-corr", Capability: "mailbox", Input: "message"})
	if err != nil {
		t.Fatal(err)
	}
	bundle := runtimeprobe.Bundle{SchemaVersion: runtimeprobe.EvidenceSchemaVersion, BaymaxVersion: client.Version(), SteleVersion: "stele", Events: out.Events}
	if err := bundle.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestClientDispatchesBoundedBaymaxChild(t *testing.T) {
	client, err := New(fakes.NewModel(nil))
	if err != nil {
		t.Fatal(err)
	}
	out, err := client.Child(context.Background(), runtimeprobe.ProbeRequest{CorrelationID: "child-corr", ParentID: "parent", Capability: "child", Input: "child"})
	if err != nil {
		t.Fatal(err)
	}
	bundle := runtimeprobe.Bundle{SchemaVersion: runtimeprobe.EvidenceSchemaVersion, BaymaxVersion: client.Version(), SteleVersion: "stele", Events: out.Events}
	if err := bundle.Validate(); err != nil {
		t.Fatal(err)
	}
	if out.Events[1].Payload["budget_ms"] != "1000" || out.Events[1].Payload["capability_digest"] == "" {
		t.Fatalf("child budget/capability evidence missing: %#v", out.Events[1].Payload)
	}
}

func TestAuthorizedRunAndStreamProjectEquivalentTerminals(t *testing.T) {
	newClient := func() *Client {
		client, err := New(fakes.NewModel([]fakes.ModelStep{{Response: types.ModelResponse{FinalAnswer: "ok"}}}))
		if err != nil {
			t.Fatal(err)
		}
		return client
	}
	req := AuthorizedRequest{RunID: "run-1", CorrelationID: "corr-1", PlanVersion: "plan-1", CapabilityDigest: "cap-1", ProjectionDigest: "projection-1", Prefix: "prefix", Input: "hello"}
	run, err := newClient().RunAuthorized(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := newClient().StreamAuthorized(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if run.Terminal.Status != stream.Terminal.Status || run.Terminal.FailureClass != stream.Terminal.FailureClass {
		t.Fatalf("terminal mismatch: %#v %#v", run.Terminal, stream.Terminal)
	}
	if run.ParentRunID != req.RunID || run.PlanVersion != req.PlanVersion || run.CapabilityDigest != req.CapabilityDigest || run.ProjectionDigest != req.ProjectionDigest {
		t.Fatalf("correlation lost: %#v", run)
	}
	if len(run.Events) < 2 || run.Events[0].Kind != EventParent || run.Events[len(run.Events)-1].Kind != EventTerminal {
		t.Fatalf("events=%#v", run.Events)
	}
}

func TestAuthorizedNativeContinuationResumesAcrossFreshClient(t *testing.T) {
	root := t.TempDir()
	config := ContinuationConfig{
		StorePath:          filepath.Join(root, "checkpoints"),
		OwnerNamespace:     "danny-test",
		DependencyProfile:  "fake-model",
		DependencyRevision: "test-v1",
		Approval:           true,
		Budget:             true,
		BudgetOwner: func(_ context.Context, _ CheckpointReference, _ NativeBudget, proposed NativeBudget) (NativeBudget, error) {
			proposed.Revision = "test-v2"
			return proposed, nil
		},
	}
	initialModel := fakes.NewModel([]fakes.ModelStep{{Response: types.ModelResponse{ToolCalls: []types.ToolCall{
		{CallID: "call-1", Name: "local.probe"},
		{CallID: "call-2", Name: "local.probe"},
	}}}})
	first, err := NewWithContinuation(initialModel, config)
	if err != nil {
		t.Fatal(err)
	}
	request := AuthorizedRequest{
		RunID: "continuation-run", CorrelationID: "continuation-session",
		PlanVersion: "plan-1", PlanDigest: "plan-digest", CapabilityDigest: "cap-digest",
		ProjectionDigest: "projection-digest", WriteScopeDigest: "write-scope", EffectNamespace: "effects",
		Input: "pause before tools", ToolPolicy: ToolPolicy{AllowedTools: []string{"probe"}},
		LoopPolicy: func() *types.LoopPolicy { p := types.DefaultLoopPolicy(); p.ToolCallLimit = 1; return &p }(),
	}
	paused, err := first.RunAuthorized(context.Background(), request)
	if err != nil {
		t.Fatalf("pause run: %v", err)
	}
	if paused.NativeCheckpoint == nil || paused.NativeCheckpoint.PauseReason != "budget_exhausted" {
		t.Fatalf("expected native budget checkpoint, got %#v", paused.NativeCheckpoint)
	}
	checkpoint := *paused.NativeCheckpoint
	if checkpoint.CheckpointID == "" || checkpoint.Digest == "" || checkpoint.Version == "" || checkpoint.RunID != request.RunID || checkpoint.SessionID != request.CorrelationID {
		t.Fatalf("checkpoint lost native identity: %#v", checkpoint)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	resumedModel := fakes.NewModel([]fakes.ModelStep{{Response: types.ModelResponse{FinalAnswer: "resumed"}}})
	second, err := NewWithContinuation(resumedModel, config)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = second.Close() }()
	budget := checkpoint.Budget
	budget.ToolLimit = 4
	budget.Revision = "proposed-v2"
	resumed, err := second.ResumeAuthorized(context.Background(), NativeResumeRequest{
		Checkpoint: checkpoint, DecisionToken: paused.PendingActionToken, IdempotencyKey: "resume-1",
		Authority:      AuthorityBinding{PlanDigest: request.PlanDigest, CapabilityDigest: request.CapabilityDigest, ProjectionDigest: request.ProjectionDigest, WriteScopeDigest: request.WriteScopeDigest, EffectNamespace: request.EffectNamespace},
		ProposedBudget: &budget,
	})
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if resumed.Terminal.Status != "succeeded" || resumed.Terminal.Result != "resumed" {
		t.Fatalf("unexpected resumed result: %#v", resumed.Terminal)
	}
	duplicate, err := second.ResumeAuthorized(context.Background(), NativeResumeRequest{
		Checkpoint: checkpoint, DecisionToken: paused.PendingActionToken, IdempotencyKey: "resume-1",
		Authority:      AuthorityBinding{PlanDigest: request.PlanDigest, CapabilityDigest: request.CapabilityDigest, ProjectionDigest: request.ProjectionDigest, WriteScopeDigest: request.WriteScopeDigest, EffectNamespace: request.EffectNamespace},
		ProposedBudget: &budget,
	})
	if err != nil || duplicate.Terminal.Result != "resumed" {
		t.Fatalf("duplicate resume: result=%#v err=%v", duplicate, err)
	}
}

func TestAuthorizedRunRejectsMissingCapabilityDigest(t *testing.T) {
	client, err := New(fakes.NewModel([]fakes.ModelStep{{Response: types.ModelResponse{FinalAnswer: "ok"}}}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.RunAuthorized(context.Background(), AuthorizedRequest{RunID: "run", CorrelationID: "corr", Input: "hello"}); !errors.Is(err, ErrAuthorizedRequest) {
		t.Fatalf("error=%v", err)
	}
}

func TestAuthorizedControlCancelsActiveRunThroughNativeEngine(t *testing.T) {
	started := make(chan struct{})
	client, err := New(&blockingModel{started: started})
	if err != nil {
		t.Fatal(err)
	}
	resultCh := make(chan ExecutionResult, 1)
	errCh := make(chan error, 1)
	go func() {
		result, runErr := client.RunAuthorized(context.Background(), AuthorizedRequest{RunID: "authorized-cancel", CorrelationID: "authorized-cancel-session", PlanVersion: "plan-v1", CapabilityDigest: "cap-v1", ProjectionDigest: "projection-v1", Input: "wait"})
		resultCh <- result
		errCh <- runErr
	}()
	<-started
	status, err := client.ControlAuthorized(context.Background(), AuthorizedControlRequest{RunID: "authorized-cancel", SessionID: "authorized-cancel-session", Action: ControlCancel, IdempotencyKey: "cancel:authorized-cancel"})
	if err != nil || status != ControlAccepted {
		t.Fatalf("cancel status=%v err=%v", status, err)
	}
	duplicate, err := client.ControlAuthorized(context.Background(), AuthorizedControlRequest{RunID: "authorized-cancel", SessionID: "authorized-cancel-session", Action: ControlCancel, IdempotencyKey: "cancel:authorized-cancel"})
	if err != nil || duplicate != ControlDuplicate {
		t.Fatalf("duplicate cancel status=%v err=%v", duplicate, err)
	}
	result := <-resultCh
	if runErr := <-errCh; !errors.Is(runErr, context.Canceled) || result.Terminal.Status != "canceled" {
		t.Fatalf("cancel result=%#v err=%v", result, runErr)
	}
}

func TestAuthorizedRetryUsesNativeCausationAndFreshRun(t *testing.T) {
	client, err := New(fakes.NewModel([]fakes.ModelStep{{Response: types.ModelResponse{FinalAnswer: "recovered"}}}))
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.RetryAuthorized(context.Background(), types.RunRef{RunID: "failed-source", SessionID: "retry-session", State: types.RunStateFailed, Source: types.ProtocolSourceRunner}, AuthorizedRequest{RunID: "retry-attempt", CorrelationID: "retry-correlation", PlanVersion: "plan-v1", CapabilityDigest: "cap-v1", ProjectionDigest: "projection-v1", Input: "retry"})
	if err != nil || result.Terminal.Status != "succeeded" || result.ParentRunID == "failed-source" {
		t.Fatalf("retry result=%#v err=%v", result, err)
	}
	if result.Terminal.Result != "recovered" {
		t.Fatalf("retry result=%#v", result.Terminal)
	}
}

func TestNativeControlAdapterRoutesTimeoutThroughNativeCancellation(t *testing.T) {
	started := make(chan struct{})
	client, err := New(&blockingModel{started: started})
	if err != nil {
		t.Fatal(err)
	}
	resultCh := make(chan ExecutionResult, 1)
	errCh := make(chan error, 1)
	go func() {
		result, runErr := client.RunAuthorized(context.Background(), AuthorizedRequest{RunID: "native-timeout", CorrelationID: "native-timeout-session", PlanVersion: "plan-v1", CapabilityDigest: "cap-v1", ProjectionDigest: "projection-v1", Input: "wait"})
		resultCh <- result
		errCh <- runErr
	}()
	<-started
	out, err := client.Control(context.Background(), runtimeboundary.ControlRequest{Version: runtimeboundary.ExecutionContractV1, ExecutionID: "product-timeout", NativeRunID: "native-timeout", SessionID: "native-timeout-session", Action: "timeout", IdempotencyKey: "timeout:native-timeout"})
	if err != nil || out.Status != string(ControlAccepted) || out.TerminalObserved {
		t.Fatalf("timeout outcome=%#v err=%v", out, err)
	}
	result := <-resultCh
	if runErr := <-errCh; !errors.Is(runErr, context.Canceled) || result.Terminal.Status != "canceled" {
		t.Fatalf("timeout terminal=%#v err=%v", result, runErr)
	}
}

func TestNativeRetryAdapterPreservesCausationAndRequestedRunIdentity(t *testing.T) {
	client, err := New(fakes.NewModel([]fakes.ModelStep{{Response: types.ModelResponse{FinalAnswer: "recovered"}}}))
	if err != nil {
		t.Fatal(err)
	}
	req := runtimeboundary.RetryRequest{Version: runtimeboundary.ExecutionContractV1, ExecutionID: "product-retry", NativeRunID: "product-retry:retry:one", CausationID: "failed-native-run", IdempotencyKey: "retry:one", Input: "retry", Authority: runtimeboundary.Authority{TaskRevision: 1, PlanVersion: "plan-v1", PlanDigest: "plan-digest", CapabilityDigest: "cap-v1", ProjectionDigest: "projection-v1", WriteScope: "none", EffectNamespace: "product-retry:effects"}}
	out, err := client.Retry(context.Background(), req)
	if err != nil || out.NativeRunID != req.NativeRunID || out.CausationID != req.CausationID || out.Status != "succeeded" {
		t.Fatalf("retry outcome=%#v err=%v", out, err)
	}
}

func TestNativeRecoveryReadsRetainedHistoryWithoutStartingAnotherRun(t *testing.T) {
	client, err := New(fakes.NewModel([]fakes.ModelStep{{Response: types.ModelResponse{FinalAnswer: "source answer"}}}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.RunAuthorized(context.Background(), AuthorizedRequest{RunID: "recover-source-run", CorrelationID: "recover-source-session", PlanVersion: "plan-v1", PlanDigest: "plan-digest", CapabilityDigest: "cap-v1", ProjectionDigest: "projection-v1", Input: "source"}); err != nil {
		t.Fatal(err)
	}
	out, err := client.Reconcile(context.Background(), runtimeboundary.RecoveryRequest{Version: runtimeboundary.ExecutionContractV1, ExecutionID: "product-recovery", NativeRunID: "recover-source-run", SourceKind: "runner", SourceNamespace: "danny.baymax.runner.history.v1", SourceID: "recover-source-run", Authority: runtimeboundary.Authority{TaskRevision: 1, PlanVersion: "plan-v1", PlanDigest: "plan-digest", CapabilityDigest: "cap-v1", ProjectionDigest: "projection-v1", WriteScope: "none", EffectNamespace: "product-recovery:effects"}})
	if err != nil || out.Status != runtimeboundary.RecoveryTerminal || out.NativeRunID != "recover-source-run" || out.NativeEventID == "" || out.TerminalStatus != "succeeded" {
		t.Fatalf("recovery outcome=%#v err=%v", out, err)
	}
}

func TestPinnedRunnerPersistsSourceOwnedNativeHistory(t *testing.T) {
	const runID = "corr-1:parent"
	source := types.SourceRef{Kind: types.HistorySourceRunner, Namespace: "danny.baymax.runner.history.v1", ID: runID}
	journal, err := runner.NewNativeEventHistory(source)
	if err != nil {
		t.Fatal(err)
	}
	model := fakes.NewModel([]fakes.ModelStep{{Response: types.ModelResponse{FinalAnswer: "ok"}}})
	client, err := New(model, runner.WithNativeEventHistory(journal))
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Run(context.Background(), runtimeprobe.ProbeRequest{CorrelationID: "corr-1", Input: "hello", Capability: "run"})
	if err != nil {
		t.Fatal(err)
	}
	caps, err := journal.Capabilities(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	if !caps.Supported || !caps.SequenceDefined || caps.Durable {
		t.Fatalf("in-memory history capabilities=%#v", caps)
	}
	page, err := journal.ReadAfter(context.Background(), source, nil, 200)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) == 0 {
		t.Fatal("runner emitted no source-owned native history")
	}
	var lastSequence uint64
	var sawCompletedTerminal bool
	for _, event := range page.Events {
		identity := event.History
		if identity == nil || identity.Source != source || identity.Sequence <= lastSequence || identity.EventID == "" {
			t.Fatalf("event lacks increasing source identity: %#v", event)
		}
		lastSequence = identity.Sequence
		if event.IsTerminal() && event.SourceState() == "completed" {
			sawCompletedTerminal = true
		}
	}
	if !sawCompletedTerminal {
		t.Fatalf("native source history lacks completed terminal fact: %#v", page.Events)
	}
}

func TestAuthorizedRunProjectsSourceIdentityAndOpaqueCursor(t *testing.T) {
	client, err := New(fakes.NewModel([]fakes.ModelStep{{Response: types.ModelResponse{FinalAnswer: "ok"}}}))
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.RunAuthorized(context.Background(), AuthorizedRequest{
		RunID: "native-project-run", CorrelationID: "native-project-correlation", PlanVersion: "plan-v1",
		CapabilityDigest: "cap-v1", ProjectionDigest: "projection-v1", Input: "hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var projected struct {
		NativeCursor         string `json:"native_cursor"`
		NativeHistoryDurable *bool  `json:"native_history_durable"`
		Events               []struct {
			Sequence     int `json:"Sequence"`
			NativeSource struct {
				Kind      string `json:"kind"`
				Namespace string `json:"namespace"`
				ID        string `json:"id"`
			} `json:"native_source"`
			NativeEventID  string `json:"native_event_id"`
			NativeSequence uint64 `json:"native_sequence"`
			NativeType     string `json:"native_type"`
		} `json:"events"`
	}
	if err := json.Unmarshal(raw, &projected); err != nil {
		t.Fatal(err)
	}
	if projected.NativeHistoryDurable == nil || *projected.NativeHistoryDurable || projected.NativeCursor == "" || len(projected.Events) == 0 {
		t.Fatalf("native history projection missing cursor or events: %s", raw)
	}
	for _, event := range projected.Events {
		if event.NativeSource.Kind != "runner" || event.NativeSource.ID != "native-project-run" || event.NativeEventID == "" || event.NativeSequence == 0 || event.NativeType == "" || uint64(event.Sequence) != event.NativeSequence {
			t.Fatalf("event lacks source-owned identity: %#v", event)
		}
	}
}

func TestAuthorizedRunProjectsNativeToolLifecycle(t *testing.T) {
	model := fakes.NewModel([]fakes.ModelStep{
		{Response: types.ModelResponse{ToolCalls: []types.ToolCall{{CallID: "native-tool-call", Name: "local.probe", Args: map[string]any{}}}}},
		{Response: types.ModelResponse{FinalAnswer: "tool complete"}},
	})
	client, err := New(model)
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.RunAuthorized(context.Background(), AuthorizedRequest{
		RunID: "native-tool-run", CorrelationID: "native-tool-correlation", PlanVersion: "plan-v1",
		CapabilityDigest: "cap-v1", ProjectionDigest: "projection-v1", Input: "use the tool",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range result.Events {
		if event.NativeType == "tool.dispatch.completed" && event.Kind == EventTool {
			return
		}
	}
	t.Fatalf("native tool lifecycle was not projected: %#v", result.Events)
}

func TestAuthorizedRunActivatesWithNativeEventObserver(t *testing.T) {
	observed := &recordingEventHandler{}
	client, err := New(fakes.NewModel([]fakes.ModelStep{{Response: types.ModelResponse{FinalAnswer: "ok"}}}))
	if err != nil {
		t.Fatal(err)
	}
	client.eventHandlerFactory = func() types.EventHandler { return observed }

	if _, err := client.RunAuthorized(context.Background(), AuthorizedRequest{
		RunID: "native-observer-run", CorrelationID: "native-observer-correlation", PlanVersion: "plan-v1",
		CapabilityDigest: "cap-v1", ProjectionDigest: "projection-v1", Input: "hello",
	}); err != nil {
		t.Fatal(err)
	}
	if len(observed.events) == 0 {
		t.Fatal("authorized activation did not install a native event observer")
	}
	for _, event := range observed.events {
		if event.RunID != "native-observer-run" {
			t.Fatalf("observer received foreign run event: %#v", event)
		}
	}
}

func TestAuthorizedToolPolicyRejectsUndeclaredMutationBeforeInvoke(t *testing.T) {
	model := fakes.NewModel([]fakes.ModelStep{
		{Response: types.ModelResponse{ToolCalls: []types.ToolCall{{CallID: "save-1", Name: "save_memory", Args: map[string]any{"content": "from memory"}}}}},
		{Response: types.ModelResponse{FinalAnswer: "done"}},
	})
	client, err := New(model)
	if err != nil {
		t.Fatal(err)
	}
	tool := &countingTool{name: "save_memory"}
	if err := client.RegisterTool(tool); err != nil {
		t.Fatal(err)
	}
	_, err = client.RunAuthorized(context.Background(), AuthorizedRequest{
		RunID: "policy-run", CorrelationID: "policy-correlation", PlanVersion: "plan-v1",
		CapabilityDigest: "cap-v1", ProjectionDigest: "projection-v1", Input: "save it",
		ToolPolicy: ToolPolicy{AllowedTools: []string{"probe"}, WriteScope: "task", TaskRevision: 7, EffectNamespace: "task:7", ApprovalGranted: true},
	})
	if err == nil {
		t.Fatal("undeclared mutation unexpectedly succeeded")
	}
	if tool.calls != 0 {
		t.Fatalf("undeclared mutation invoked tool %d times", tool.calls)
	}
}

func TestAuthorizedToolPolicyAllowsDeclaredReadTool(t *testing.T) {
	model := fakes.NewModel([]fakes.ModelStep{
		{Response: types.ModelResponse{ToolCalls: []types.ToolCall{{CallID: "read-1", Name: "local.read_source", Args: map[string]any{}}}}},
		{Response: types.ModelResponse{FinalAnswer: "done"}},
	})
	client, err := New(model)
	if err != nil {
		t.Fatal(err)
	}
	tool := &countingTool{name: "read_source"}
	if err := client.RegisterTool(tool); err != nil {
		t.Fatal(err)
	}
	_, err = client.RunAuthorized(context.Background(), AuthorizedRequest{
		RunID: "read-policy-run", CorrelationID: "read-policy-correlation", PlanVersion: "plan-v1",
		CapabilityDigest: "cap-v1", ProjectionDigest: "projection-v1", Input: "read it",
		ToolPolicy: ToolPolicy{AllowedTools: []string{"read_source"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if tool.calls != 1 {
		t.Fatalf("declared read tool invoked %d times, want 1", tool.calls)
	}
}

func TestAuthorizedToolPolicyRequiresLiveAuthorityForMutation(t *testing.T) {
	for _, policy := range []ToolPolicy{
		{AllowedTools: []string{"save_memory"}, WriteScope: "task", TaskRevision: 7, EffectNamespace: "task:7", ApprovalGranted: true},
		{AllowedTools: []string{"save_memory"}, WriteScope: "none", TaskRevision: 7, EffectNamespace: "task:7", ApprovalGranted: true, Authorize: allowMutation},
		{AllowedTools: []string{"save_memory"}, WriteScope: "task", EffectNamespace: "task:7", ApprovalGranted: true, Authorize: allowMutation},
		{AllowedTools: []string{"save_memory"}, WriteScope: "task", TaskRevision: 7, ApprovalGranted: true, Authorize: allowMutation},
		{AllowedTools: []string{"save_memory"}, WriteScope: "task", TaskRevision: 7, EffectNamespace: "task:7", Authorize: allowMutation},
	} {
		model := fakes.NewModel([]fakes.ModelStep{{Response: types.ModelResponse{ToolCalls: []types.ToolCall{{CallID: "save-1", Name: "local.save_memory", Args: map[string]any{}}}}}})
		client, err := New(model)
		if err != nil {
			t.Fatal(err)
		}
		tool := &countingTool{name: "save_memory"}
		if err := client.RegisterTool(tool); err != nil {
			t.Fatal(err)
		}
		_, err = client.RunAuthorized(context.Background(), AuthorizedRequest{RunID: "mutation-policy-run", CorrelationID: "mutation-policy-correlation", PlanVersion: "plan-v1", CapabilityDigest: "cap-v1", ProjectionDigest: "projection-v1", Input: "save it", ToolPolicy: policy})
		if err == nil || tool.calls != 0 {
			t.Fatalf("policy=%+v err=%v calls=%d", policy, err, tool.calls)
		}
	}
}

func TestAuthorizedToolPolicyPassesFullyAuthorizedMutation(t *testing.T) {
	model := fakes.NewModel([]fakes.ModelStep{{Response: types.ModelResponse{ToolCalls: []types.ToolCall{{CallID: "save-1", Name: "local.save_memory", Args: map[string]any{}}}}}, {Response: types.ModelResponse{FinalAnswer: "saved"}}})
	client, err := New(model)
	if err != nil {
		t.Fatal(err)
	}
	tool := &countingTool{name: "save_memory"}
	if err := client.RegisterTool(tool); err != nil {
		t.Fatal(err)
	}
	_, err = client.RunAuthorized(context.Background(), AuthorizedRequest{
		RunID: "authorized-mutation-run", CorrelationID: "authorized-mutation-correlation", PlanVersion: "plan-v1", CapabilityDigest: "cap-v1", ProjectionDigest: "projection-v1", Input: "save it",
		ToolPolicy: ToolPolicy{AllowedTools: []string{"save_memory"}, WriteScope: "task", TaskRevision: 7, EffectNamespace: "task:7", ApprovalGranted: true, Authorize: allowMutation},
	})
	if err != nil || tool.calls != 1 {
		t.Fatalf("authorized mutation err=%v calls=%d", err, tool.calls)
	}
}

func TestAuthorizedChildDispatchesNativeAssignmentAndPreservesParent(t *testing.T) {
	client, err := New(fakes.NewModel([]fakes.ModelStep{{Response: types.ModelResponse{FinalAnswer: "child result"}}}))
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.RunChildAuthorized(context.Background(), AuthorizedRequest{
		RunID: "child-run", ParentRunID: "parent-run", CorrelationID: "child-correlation", PlanVersion: "plan-v1", CapabilityDigest: "cap-v1", ProjectionDigest: "projection-v1", Input: "perform child assignment",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ParentRunID != "parent-run" || result.Terminal.Status != "succeeded" || result.Terminal.Result != "child result" {
		t.Fatalf("native child result=%#v", result)
	}
	if len(result.Events) == 0 || result.Events[0].NativeSource.Kind != "runner" {
		t.Fatalf("child lacks source-owned native execution evidence: %#v", result.Events)
	}
}

func TestAuthorizedChildHonorsAssignedBudget(t *testing.T) {
	started := make(chan struct{})
	client, err := New(&blockingModel{started: started})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.RunChildAuthorized(context.Background(), AuthorizedRequest{
		RunID: "budgeted-child", ParentRunID: "parent-run", CorrelationID: "budgeted-child-correlation", PlanVersion: "plan-v1", CapabilityDigest: "cap-v1", ProjectionDigest: "projection-v1", PerChildBudget: 200, Input: "wait",
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("child budget error=%v, want deadline exceeded", err)
	}
	select {
	case <-started:
	default:
		t.Fatal("budgeted child did not reach native model execution")
	}
}

func TestAuthorizedChildBatchUsesStableAssignmentsAndNativeEvidence(t *testing.T) {
	client, err := New(fakes.NewModel([]fakes.ModelStep{
		{Response: types.ModelResponse{FinalAnswer: "child-a"}},
		{Response: types.ModelResponse{FinalAnswer: "child-b"}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	results, err := client.RunChildrenAuthorized(context.Background(), AuthorizedRequest{
		RunID: "batch-parent", CorrelationID: "batch-correlation", PlanVersion: "plan-v1", CapabilityDigest: "cap-v1", ProjectionDigest: "projection-v1", Input: "bounded assignment",
	}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("results=%#v", results)
	}
	for index, result := range results {
		wantID := fmt.Sprintf("batch-parent:child:%d", index+1)
		if result.NativeRunID != wantID || result.ParentRunID != "batch-parent" || result.Terminal.Status != "succeeded" || len(result.Events) == 0 {
			t.Fatalf("result[%d]=%#v", index, result)
		}
		if result.Events[0].NativeSource.ID == "" || result.Events[0].ProjectionDigest != "projection-v1" {
			t.Fatalf("native evidence[%d]=%#v", index, result.Events[0])
		}
	}
}

func TestAuthorizedParallelTeamUsesExecutionDerivedMembersAndNativeAggregate(t *testing.T) {
	client, err := New(fakes.NewModel([]fakes.ModelStep{
		{Response: types.ModelResponse{FinalAnswer: "member-a"}},
		{Response: types.ModelResponse{FinalAnswer: "member-b"}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.RunTeamAuthorized(context.Background(), TeamRequest{
		RunID: "team-run", TeamID: "team-run:parallel", CorrelationID: "team-correlation", PlanVersion: "plan-v1", PlanDigest: "plan-digest", CapabilityDigest: "cap-v1", ProjectionDigest: "projection-v1", WriteScopeDigest: "none", EffectNamespace: "team-run:effects", Input: "analyze",
		Members: []TeamMember{{ID: "a", Role: "worker", Input: "a"}, {ID: "b", Role: "worker", Input: "b"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.TeamID != "team-run:parallel" || len(result.Members) != 2 || result.Status != "succeeded" {
		t.Fatalf("team result=%#v", result)
	}
	for _, member := range result.Members {
		if member.RunID != "team-run:parallel:"+member.MemberID || member.ParentRunID != "team-run" || member.Status != "succeeded" {
			t.Fatalf("member identity/status=%#v", member)
		}
	}
}

func TestAuthorizedParallelTeamRetainsConcurrentNativeMemberAndTeamEvidence(t *testing.T) {
	model := newTeamBarrierModel(2)
	client, err := New(model)
	if err != nil {
		t.Fatal(err)
	}
	type outcome struct {
		result TeamExecutionResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := client.RunTeamAuthorized(context.Background(), TeamRequest{
			RunID: "concurrent-team-run", TeamID: "concurrent-team-run:parallel", CorrelationID: "concurrent-team-correlation", PlanVersion: "plan-v1", PlanDigest: "plan-digest", CapabilityDigest: "cap-v1", ProjectionDigest: "projection-v1", WriteScopeDigest: "none", EffectNamespace: "concurrent-team-run:effects", Input: "analyze",
			Members: []TeamMember{{ID: "a", Role: "worker", Input: "a"}, {ID: "b", Role: "worker", Input: "b"}},
		})
		done <- outcome{result: result, err: err}
	}()
	select {
	case <-model.started:
	case completed := <-done:
		t.Fatalf("members did not execute concurrently: result=%#v err=%v", completed.result, completed.err)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for concurrent native member execution")
	}
	close(model.release)
	completed := <-done
	if completed.err != nil {
		t.Fatal(completed.err)
	}
	if completed.result.NativeCursor == "" || len(completed.result.Members) != 2 {
		t.Fatalf("missing native team/member evidence: %#v", completed.result)
	}
	for _, member := range completed.result.Members {
		if member.NativeCursor == "" || len(member.Events) == 0 {
			t.Fatalf("missing native member evidence: %#v", member)
		}
	}
}

func TestAuthorizedParallelTeamAppliesNativeWorkerLimitAndTimeout(t *testing.T) {
	model := newTeamGateModel()
	client, err := New(model)
	if err != nil {
		t.Fatal(err)
	}
	resultCh := make(chan struct {
		result TeamExecutionResult
		err    error
	}, 1)
	go func() {
		result, err := client.RunTeamAuthorized(context.Background(), TeamRequest{
			RunID: "limited-team-run", TeamID: "limited-team-run:parallel", CorrelationID: "limited-team-correlation", PlanVersion: "plan-v1", PlanDigest: "plan-digest", CapabilityDigest: "cap-v1", ProjectionDigest: "projection-v1", WriteScopeDigest: "none", EffectNamespace: "limited-team-run:effects",
			ParallelMaxWorkers: 1, TaskTimeout: 200 * time.Millisecond,
			Members: []TeamMember{{ID: "a", Role: "worker", Input: "a"}, {ID: "b", Role: "worker", Input: "b"}},
		})
		resultCh <- struct {
			result TeamExecutionResult
			err    error
		}{result: result, err: err}
	}()
	select {
	case <-model.entered:
	case <-time.After(time.Second):
		t.Fatal("first bounded team member did not start")
	}
	select {
	case <-model.entered:
		t.Fatal("native worker limit admitted a second member")
	case <-time.After(50 * time.Millisecond):
	}
	completed := <-resultCh
	if completed.err == nil || completed.result.Status != "failed" {
		t.Fatalf("timeout was not retained as team failure: result=%#v err=%v", completed.result, completed.err)
	}
}

func TestAuthorizedTeamInheritsMemberToolAuthority(t *testing.T) {
	var observed ToolPolicy
	client, err := New(fakes.NewModel([]fakes.ModelStep{
		{Response: types.ModelResponse{ToolCalls: []types.ToolCall{{CallID: "save-1", Name: "local.save_memory", Args: map[string]any{"value": "x"}}}}},
		{Response: types.ModelResponse{FinalAnswer: "saved"}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if err := client.RegisterTool(teamSaveMemoryTool{}); err != nil {
		t.Fatal(err)
	}
	result, err := client.RunTeamAuthorized(context.Background(), TeamRequest{
		RunID: "authority-team-run", TeamID: "authority-team-run:team", CorrelationID: "authority-team-correlation", PlanVersion: "plan-v1", PlanDigest: "plan-digest", CapabilityDigest: "cap-v1", ProjectionDigest: "projection-v1", WriteScopeDigest: "write-scope", EffectNamespace: "authority-team-run:effects",
		ToolPolicy: ToolPolicy{AllowedTools: []string{"save_memory"}, WriteScope: "write-scope", TaskRevision: 7, EffectNamespace: "authority-team-run:effects", ApprovalGranted: true, Authorize: func(_ context.Context, _ string, _ map[string]any, policy ToolPolicy) error {
			observed = policy
			return nil
		}},
		Members: []TeamMember{{ID: "writer", Role: "worker", Input: "save"}},
	})
	if err != nil || result.Status != "succeeded" {
		t.Fatalf("authorized team failed: result=%#v err=%v", result, err)
	}
	if observed.WriteScope != "write-scope" || observed.EffectNamespace != "authority-team-run:effects" || observed.TaskRevision != 7 {
		t.Fatalf("member authority was not inherited: %#v", observed)
	}
}

func TestAuthorizedParallelTeamPropagatesCancellationToNativeMembers(t *testing.T) {
	model := newTeamBarrierModel(2)
	client, err := New(model)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	resultCh := make(chan struct {
		result TeamExecutionResult
		err    error
	}, 1)
	go func() {
		result, err := client.RunTeamAuthorized(ctx, TeamRequest{
			RunID: "cancel-team-run", TeamID: "cancel-team-run:parallel", CorrelationID: "cancel-team-correlation", PlanVersion: "plan-v1", PlanDigest: "plan-digest", CapabilityDigest: "cap-v1", ProjectionDigest: "projection-v1", WriteScopeDigest: "none", EffectNamespace: "cancel-team-run:effects",
			Members: []TeamMember{{ID: "a", Role: "worker", Input: "a"}, {ID: "b", Role: "worker", Input: "b"}},
		})
		resultCh <- struct {
			result TeamExecutionResult
			err    error
		}{result: result, err: err}
	}()
	select {
	case <-model.started:
		cancel()
	case completed := <-resultCh:
		t.Fatalf("team completed before cancellation: result=%#v err=%v", completed.result, completed.err)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for native team members")
	}
	select {
	case completed := <-resultCh:
		if !errors.Is(completed.err, context.Canceled) && completed.result.Status != "failed" {
			t.Fatalf("cancellation was not retained: result=%#v err=%v", completed.result, completed.err)
		}
	case <-time.After(time.Second):
		t.Fatal("native team did not stop after cancellation")
	}
}

func TestAuthorizedParallelTeamRetainsFailureAndDoesNotFabricateSuccess(t *testing.T) {
	client, err := New(fakes.NewModel([]fakes.ModelStep{{Err: errors.New("member model failure")}, {Response: types.ModelResponse{FinalAnswer: "other"}}}))
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.RunTeamAuthorized(context.Background(), TeamRequest{
		RunID: "team-failure-run", TeamID: "team-failure:parallel", CorrelationID: "team-failure-correlation", PlanVersion: "plan-v1", PlanDigest: "plan-digest", CapabilityDigest: "cap-v1", ProjectionDigest: "projection-v1", WriteScopeDigest: "none", EffectNamespace: "team-failure-run:effects",
		Members: []TeamMember{{ID: "a", Role: "worker", Input: "a"}, {ID: "b", Role: "worker", Input: "b"}},
	})
	if err == nil || result.Status != "failed" || result.FailureClass == "" {
		t.Fatalf("team failure was fabricated as success: result=%#v err=%v", result, err)
	}
}

func TestAuthorizedVoteTeamUsesMemberEvidenceAndNativeTieBreak(t *testing.T) {
	client, err := New(voteEvidenceModel{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.RunTeamAuthorized(context.Background(), TeamRequest{
		RunID: "vote-run", TeamID: "vote-run:team", CorrelationID: "vote-correlation", PlanVersion: "plan-v1", PlanDigest: "plan-digest", CapabilityDigest: "cap-v1", ProjectionDigest: "projection-v1", WriteScopeDigest: "none", EffectNamespace: "vote-run:effects", Strategy: teams.StrategyVote, VoteTieBreak: teams.VoteTieBreakFirstTaskID,
		Members: []TeamMember{{ID: "a", Role: "worker", Input: "decide approve"}, {ID: "b", Role: "worker", Input: "decide reject"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.WinnerVote != "approve" || result.Status != "succeeded" || result.NativeCursor == "" || len(result.Events) == 0 || len(result.Members) != 2 {
		t.Fatalf("vote result=%#v", result)
	}
	for _, member := range result.Members {
		if member.NativeCursor == "" || len(member.Events) == 0 || member.Result == "" {
			t.Fatalf("vote member provenance=%#v", member)
		}
	}
}

type voteEvidenceModel struct{}

func (voteEvidenceModel) Generate(_ context.Context, req types.ModelRequest) (types.ModelResponse, error) {
	if strings.Contains(req.Input, "decide approve") {
		return types.ModelResponse{FinalAnswer: "approve"}, nil
	}
	return types.ModelResponse{FinalAnswer: "reject"}, nil
}

func (m voteEvidenceModel) Stream(ctx context.Context, req types.ModelRequest, _ func(types.ModelEvent) error) error {
	_, err := m.Generate(ctx, req)
	return err
}

func (voteEvidenceModel) ProviderName() string { return "vote-evidence" }
func (voteEvidenceModel) DiscoverCapabilities(context.Context, types.ModelRequest) (types.ProviderCapabilities, error) {
	return types.ProviderCapabilities{Provider: "vote-evidence", Support: map[types.ModelCapability]types.CapabilitySupport{types.ModelCapabilityStreaming: types.CapabilitySupportSupported}}, nil
}

func TestAuthorizedVoteTeamRejectsOverBoundedParticipation(t *testing.T) {
	client, err := New(fakes.NewModel([]fakes.ModelStep{{Response: types.ModelResponse{FinalAnswer: "approve"}}}))
	if err != nil {
		t.Fatal(err)
	}
	members := make([]TeamMember, 9)
	for index := range members {
		members[index] = TeamMember{ID: fmt.Sprintf("member-%d", index), Role: "worker", Input: "decide"}
	}
	_, err = client.RunTeamAuthorized(context.Background(), TeamRequest{RunID: "vote-limit-run", TeamID: "vote-limit:team", CorrelationID: "vote-limit-correlation", PlanVersion: "plan-v1", PlanDigest: "plan-digest", CapabilityDigest: "cap-v1", ProjectionDigest: "projection-v1", WriteScopeDigest: "none", EffectNamespace: "vote-limit-run:effects", Strategy: teams.StrategyVote, Members: members})
	if !errors.Is(err, ErrAuthorizedRequest) {
		t.Fatalf("over-bounded vote participation error=%v", err)
	}
}

func TestAuthorizedVoteTeamRejectsMissingDecisionEvidence(t *testing.T) {
	client, err := New(fakes.NewModel([]fakes.ModelStep{{Response: types.ModelResponse{FinalAnswer: "unclear"}}}))
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.RunTeamAuthorized(context.Background(), TeamRequest{
		RunID: "vote-invalid-run", TeamID: "vote-invalid:team", CorrelationID: "vote-invalid-correlation", PlanVersion: "plan-v1", PlanDigest: "plan-digest", CapabilityDigest: "cap-v1", ProjectionDigest: "projection-v1", WriteScopeDigest: "none", EffectNamespace: "vote-invalid-run:effects", Strategy: teams.StrategyVote,
		Members: []TeamMember{{ID: "a", Role: "worker", Input: "decide"}},
	})
	if err == nil || result.Status != "failed" {
		t.Fatalf("missing decision evidence was accepted: result=%#v err=%v", result, err)
	}
}

func TestAuthorizedWorkflowUsesNativeDependencyOrderAndCorrelatedAssignments(t *testing.T) {
	client, err := New(fakes.NewModel([]fakes.ModelStep{
		{Response: types.ModelResponse{FinalAnswer: "first"}},
		{Response: types.ModelResponse{FinalAnswer: "second"}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.RunWorkflowAuthorized(context.Background(), WorkflowRequest{
		Version: workflowContractVersion,
		RunID:   "workflow-run", WorkflowID: "workflow-run:graph", SessionID: "workflow-session", CorrelationID: "workflow-correlation",
		PlanVersion: "plan-v1", PlanDigest: "plan-digest", CapabilityDigest: "cap-v1", ProjectionDigest: "projection-v1",
		WriteScopeDigest: "write-scope", EffectNamespace: "effects:workflow", HistoryPath: filepath.Join(t.TempDir(), "workflow-history.json"),
		Assignments: []WorkflowAssignment{
			{ID: "step-b", Role: "worker", Objective: "second", DependsOn: []string{"step-a"}, Budget: 1000},
			{ID: "step-a", Role: "worker", Objective: "first", Budget: 1000},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "succeeded" || len(result.Steps) != 2 {
		t.Fatalf("workflow result=%#v", result)
	}
	if got, want := result.ExecutionOrder, []string{"step-a", "step-b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("execution order=%v, want %v", got, want)
	}
	if result.Steps[0].NativeRunID != "workflow-run:step:step-a" || result.Steps[1].NativeRunID != "workflow-run:step:step-b" {
		t.Fatalf("step native identities=%#v", result.Steps)
	}
	if result.NativeCursor == "" || !result.NativeHistoryDurable || len(result.Events) == 0 {
		t.Fatalf("workflow native evidence=%#v", result)
	}
	for _, event := range result.Events {
		if event.NativeSource.Kind != string(types.HistorySourceWorkflow) || event.NativeEventID == "" {
			t.Fatalf("workflow event lacks source identity=%#v", event)
		}
	}
}

func TestAuthorizedWorkflowBlocksDependentStepAfterRequiredFailure(t *testing.T) {
	client, err := New(fakes.NewModel([]fakes.ModelStep{{Err: errors.New("required step failed")}}))
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.RunWorkflowAuthorized(context.Background(), WorkflowRequest{
		Version: workflowContractVersion,
		RunID:   "workflow-failure", WorkflowID: "workflow-failure:graph", SessionID: "workflow-failure-session", CorrelationID: "workflow-failure-correlation",
		PlanVersion: "plan-v1", PlanDigest: "plan-digest", CapabilityDigest: "cap-v1", ProjectionDigest: "projection-v1",
		WriteScopeDigest: "write-scope", EffectNamespace: "effects:workflow", HistoryPath: filepath.Join(t.TempDir(), "workflow-history.json"),
		Assignments: []WorkflowAssignment{
			{ID: "required", Role: "worker", Objective: "fail", Budget: 1000},
			{ID: "dependent", Role: "worker", Objective: "must not run", DependsOn: []string{"required"}, Budget: 1000},
		},
	})
	if err == nil || result.Status != "failed" {
		t.Fatalf("required failure was not retained: result=%#v err=%v", result, err)
	}
	if len(result.ExecutionOrder) != 1 || result.ExecutionOrder[0] != "required" {
		t.Fatalf("dependent step executed after failure: order=%v", result.ExecutionOrder)
	}
}

func TestAuthorizedWorkflowReturnsNativeCheckpointAuthority(t *testing.T) {
	client, err := New(fakes.NewModel([]fakes.ModelStep{{Response: types.ModelResponse{FinalAnswer: "unused"}}}))
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.RunWorkflowAuthorized(context.Background(), WorkflowRequest{
		Version: workflowContractVersion,
		RunID:   "workflow-pause", WorkflowID: "workflow-pause:graph", SessionID: "workflow-pause-session", CorrelationID: "workflow-pause-correlation",
		PlanVersion: "plan-v1", PlanDigest: "plan-digest", CapabilityDigest: "cap-v1", ProjectionDigest: "projection-v1",
		WriteScopeDigest: "write-scope", EffectNamespace: "effects:workflow", HistoryPath: filepath.Join(t.TempDir(), "workflow-history.json"), ContinuationPath: t.TempDir(),
		PauseBeforeStep: func(context.Context, WorkflowAssignment) (WorkflowPause, bool) {
			return WorkflowPause{Reason: "approval_required", PendingActionReference: "approval:step-a"}, true
		},
		Assignments: []WorkflowAssignment{{ID: "step-a", Role: "worker", Objective: "requires approval", Budget: 1000}},
	})
	if !errors.Is(err, ErrWorkflowPaused) || result.Status != "approval_required" {
		t.Fatalf("workflow pause result=%#v err=%v", result, err)
	}
	if result.Checkpoint == nil || result.Checkpoint.AuthorityDigest != "cap-v1" || result.Checkpoint.PlanDigest != "plan-digest" || result.Checkpoint.WriteScopeDigest != "write-scope" || result.Checkpoint.EffectNamespace != "effects:workflow" {
		t.Fatalf("checkpoint authority=%#v", result.Checkpoint)
	}
}

func TestAuthorizedWorkflowReleasesContinuationStoreAfterReturn(t *testing.T) {
	client, err := New(fakes.NewModel([]fakes.ModelStep{{Response: types.ModelResponse{FinalAnswer: "unused"}}}))
	if err != nil {
		t.Fatal(err)
	}
	continuationPath := t.TempDir()
	_, err = client.RunWorkflowAuthorized(context.Background(), WorkflowRequest{
		Version: workflowContractVersion,
		RunID:   "workflow-release-store", WorkflowID: "workflow-release-store:graph", SessionID: "workflow-release-store-session", CorrelationID: "workflow-release-store-correlation",
		PlanVersion: "plan-v1", PlanDigest: "plan-digest", CapabilityDigest: "cap-v1", ProjectionDigest: "projection-v1",
		WriteScopeDigest: "write-scope", EffectNamespace: "effects:workflow", HistoryPath: filepath.Join(t.TempDir(), "workflow-history.json"), ContinuationPath: continuationPath,
		PauseBeforeStep: func(context.Context, WorkflowAssignment) (WorkflowPause, bool) {
			return WorkflowPause{Reason: "approval_required", PendingActionReference: "approval:step-a"}, true
		},
		Assignments: []WorkflowAssignment{{ID: "step-a", Role: "worker", Objective: "requires approval", Budget: 1000}},
	})
	if !errors.Is(err, ErrWorkflowPaused) {
		t.Fatalf("workflow pause err=%v, want ErrWorkflowPaused", err)
	}
	reopened, err := workflow.NewFileWorkflowContinuationStore(continuationPath, "workflow")
	if err != nil {
		t.Fatalf("continuation store remained locked after workflow returned: %v", err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatalf("close reopened continuation store: %v", err)
	}
}

func TestAuthorizedWorkflowRejectsUnknownPauseReason(t *testing.T) {
	client, err := New(fakes.NewModel([]fakes.ModelStep{{Response: types.ModelResponse{FinalAnswer: "must not run"}}}))
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.RunWorkflowAuthorized(context.Background(), WorkflowRequest{
		Version: workflowContractVersion,
		RunID:   "workflow-invalid-pause", WorkflowID: "workflow-invalid-pause:graph", SessionID: "workflow-invalid-pause-session", CorrelationID: "workflow-invalid-pause-correlation",
		PlanVersion: "plan-v1", PlanDigest: "plan-digest", CapabilityDigest: "cap-v1", ProjectionDigest: "projection-v1",
		WriteScopeDigest: "write-scope", EffectNamespace: "effects:workflow", HistoryPath: filepath.Join(t.TempDir(), "workflow-history.json"), ContinuationPath: t.TempDir(),
		PauseBeforeStep: func(context.Context, WorkflowAssignment) (WorkflowPause, bool) {
			return WorkflowPause{Reason: "unrecognized_pause"}, true
		},
		Assignments: []WorkflowAssignment{{ID: "step-a", Role: "worker", Objective: "task", Budget: 1000}},
	})
	if !errors.Is(err, ErrWorkflowRequest) {
		t.Fatalf("unknown pause reason error=%v, want ErrWorkflowRequest", err)
	}
}

func TestAuthorizedWorkflowEnforcesStepAndTotalResourceLimits(t *testing.T) {
	for _, tc := range []struct {
		name        string
		assignments []WorkflowAssignment
	}{
		{name: "step budget", assignments: []WorkflowAssignment{{ID: "step-a", Role: "worker", Objective: "task", Budget: maxWorkflowStepBudgetMS + 1}}},
		{name: "retry count", assignments: []WorkflowAssignment{{ID: "step-a", Role: "worker", Objective: "task", Budget: 1000, RetryMaxAttempts: maxWorkflowRetries + 1}}},
		{name: "aggregate budget", assignments: []WorkflowAssignment{{ID: "step-a", Role: "worker", Objective: "a", Budget: maxWorkflowStepBudgetMS}, {ID: "step-b", Role: "worker", Objective: "b", Budget: maxWorkflowStepBudgetMS}, {ID: "step-c", Role: "worker", Objective: "c", Budget: maxWorkflowStepBudgetMS}, {ID: "step-d", Role: "worker", Objective: "d", Budget: 1}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, err := New(fakes.NewModel(nil))
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.RunWorkflowAuthorized(context.Background(), WorkflowRequest{
				Version: workflowContractVersion,
				RunID:   "workflow-limit", WorkflowID: "workflow-limit:graph", SessionID: "workflow-limit-session", CorrelationID: "workflow-limit-correlation",
				PlanVersion: "plan-v1", PlanDigest: "plan-digest", CapabilityDigest: "cap-v1", ProjectionDigest: "projection-v1",
				WriteScopeDigest: "write-scope", EffectNamespace: "effects:workflow", HistoryPath: filepath.Join(t.TempDir(), "workflow-history.json"),
				Assignments: tc.assignments,
			})
			if !errors.Is(err, ErrWorkflowRequest) {
				t.Fatalf("resource limit error=%v, want ErrWorkflowRequest", err)
			}
		})
	}
}

func allowMutation(context.Context, string, map[string]any, ToolPolicy) error { return nil }

type countingTool struct {
	name  string
	calls int
}

func (t *countingTool) Name() string               { return t.name }
func (t *countingTool) Description() string        { return "test mutation" }
func (t *countingTool) JSONSchema() map[string]any { return map[string]any{"type": "object"} }
func (t *countingTool) Invoke(context.Context, map[string]any) (types.ToolResult, error) {
	t.calls++
	return types.ToolResult{Content: "mutated"}, nil
}

type recordingEventHandler struct{ events []types.Event }

func (h *recordingEventHandler) OnEvent(_ context.Context, event types.Event) {
	h.events = append(h.events, event)
}

func TestPinnedHostAdmissionReservesBeforeActivationAndRejectsDuplicate(t *testing.T) {
	model := fakes.NewModel([]fakes.ModelStep{{Response: types.ModelResponse{FinalAnswer: "activated"}}})
	client, err := New(model)
	if err != nil {
		t.Fatal(err)
	}
	request := types.RunRequest{RunID: "host-admission-run", SessionID: "host-admission-session", Input: "hello"}
	first, err := client.engine.AdmitHostRun(context.Background(), request, types.HostRunExecutionModeRun)
	if err != nil || first.Status != types.HostAdmissionStatusAccepted || first.Execution == nil {
		t.Fatalf("first admission=%#v err=%v", first, err)
	}
	if got := model.Calls(); got != 0 {
		t.Fatalf("model calls before activation=%d, want 0", got)
	}
	duplicate, err := client.engine.AdmitHostRun(context.Background(), request, types.HostRunExecutionModeRun)
	if err != nil || duplicate.Status != types.HostAdmissionStatusDuplicate || duplicate.Execution != nil {
		t.Fatalf("duplicate admission=%#v err=%v", duplicate, err)
	}
	if got := model.Calls(); got != 0 {
		t.Fatalf("duplicate admission activated model: calls=%d", got)
	}
	first.Execution.Release()
	retryAdmission, err := client.engine.AdmitHostRun(context.Background(), request, types.HostRunExecutionModeRun)
	if err != nil || retryAdmission.Status != types.HostAdmissionStatusAccepted {
		t.Fatalf("admission after release=%#v err=%v", retryAdmission, err)
	}
	result, err := retryAdmission.Execution.Execute(nil)
	if err != nil || result.TerminalOutcome == nil || result.TerminalOutcome.State != types.RunStateCompleted {
		t.Fatalf("activated result=%#v err=%v", result, err)
	}
	if got := model.Calls(); got != 1 {
		t.Fatalf("model calls after activation=%d, want 1", got)
	}
}

func TestPinnedHostCancelAndCausalRetryContracts(t *testing.T) {
	started := make(chan struct{})
	model := &blockingModel{started: started}
	client, err := New(model)
	if err != nil {
		t.Fatal(err)
	}
	admission, err := client.engine.AdmitHostRun(context.Background(), types.RunRequest{RunID: "cancel-run", SessionID: "cancel-session", Input: "cancel"}, types.HostRunExecutionModeRun)
	if err != nil || admission.Status != types.HostAdmissionStatusAccepted {
		t.Fatalf("admission=%#v err=%v", admission, err)
	}
	resultCh := make(chan types.RunResult, 1)
	errCh := make(chan error, 1)
	go func() { result, runErr := admission.Execution.Execute(nil); resultCh <- result; errCh <- runErr }()
	<-started
	status, err := client.engine.CancelRun("cancel-run", "cancel-session")
	if err != nil || status != types.HostAdmissionStatusAccepted {
		t.Fatalf("cancel status=%v err=%v", status, err)
	}
	duplicate, err := client.engine.CancelRun("cancel-run", "cancel-session")
	if err != nil || duplicate != types.HostAdmissionStatusDuplicate {
		t.Fatalf("duplicate cancel status=%v err=%v", duplicate, err)
	}
	result := <-resultCh
	if runErr := <-errCh; !errors.Is(runErr, context.Canceled) || result.TerminalOutcome == nil || result.TerminalOutcome.State != types.RunStateCanceled {
		t.Fatalf("cancel result=%#v err=%v", result, runErr)
	}

	retryModel := fakes.NewModel([]fakes.ModelStep{{Response: types.ModelResponse{FinalAnswer: "recovered"}}})
	retryClient, err := New(retryModel)
	if err != nil {
		t.Fatal(err)
	}
	previous := types.RunRef{RunID: "failed-source", SessionID: "retry-session", State: types.RunStateFailed, Source: types.ProtocolSourceRunner}
	retry, err := retryClient.engine.RetryRun(context.Background(), previous, types.RunRequest{RunID: "retry-attempt", Input: "retry"}, nil, false)
	if err != nil || retry.RunID == previous.RunID || retry.TerminalOutcome == nil || retry.TerminalOutcome.CausationID != previous.RunID {
		t.Fatalf("retry=%#v err=%v", retry, err)
	}
}

func TestPinnedRunnerPreservesDeadlineTerminal(t *testing.T) {
	started := make(chan struct{})
	client, err := New(&blockingModel{started: started})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	result, err := client.engine.Run(ctx, types.RunRequest{RunID: "deadline-run", SessionID: "deadline-session", Input: "timeout"}, nil)
	if !errors.Is(err, context.DeadlineExceeded) || result.TerminalOutcome == nil || result.TerminalOutcome.State != types.RunStateFailed || result.Error == nil {
		t.Fatalf("deadline result=%#v err=%v", result, err)
	}
}

type blockingModel struct {
	started chan struct{}
	once    sync.Once
}

type teamBarrierModel struct {
	started chan struct{}
	release chan struct{}
	mu      sync.Mutex
	count   int
}

type teamGateModel struct {
	entered chan struct{}
}

func newTeamGateModel() *teamGateModel {
	return &teamGateModel{entered: make(chan struct{}, 2)}
}

func (m *teamGateModel) Generate(ctx context.Context, _ types.ModelRequest) (types.ModelResponse, error) {
	m.entered <- struct{}{}
	<-ctx.Done()
	return types.ModelResponse{}, ctx.Err()
}

func (m *teamGateModel) Stream(ctx context.Context, req types.ModelRequest, _ func(types.ModelEvent) error) error {
	_, err := m.Generate(ctx, req)
	return err
}

func (*teamGateModel) ProviderName() string { return "team-gate" }
func (*teamGateModel) DiscoverCapabilities(context.Context, types.ModelRequest) (types.ProviderCapabilities, error) {
	return types.ProviderCapabilities{Provider: "team-gate", Support: map[types.ModelCapability]types.CapabilitySupport{types.ModelCapabilityStreaming: types.CapabilitySupportSupported}}, nil
}

type teamSaveMemoryTool struct{}

func (teamSaveMemoryTool) Name() string        { return "save_memory" }
func (teamSaveMemoryTool) Description() string { return "test mutation" }
func (teamSaveMemoryTool) JSONSchema() map[string]any {
	return map[string]any{"type": "object"}
}
func (teamSaveMemoryTool) Invoke(context.Context, map[string]any) (types.ToolResult, error) {
	return types.ToolResult{Content: "saved"}, nil
}

func newTeamBarrierModel(expected int) *teamBarrierModel {
	return &teamBarrierModel{started: make(chan struct{}), release: make(chan struct{}), count: -expected}
}

func (m *teamBarrierModel) Generate(ctx context.Context, _ types.ModelRequest) (types.ModelResponse, error) {
	m.mu.Lock()
	m.count++
	if m.count == 0 {
		close(m.started)
	}
	m.mu.Unlock()
	select {
	case <-m.release:
		return types.ModelResponse{FinalAnswer: "member"}, nil
	case <-ctx.Done():
		return types.ModelResponse{}, ctx.Err()
	}
}

func (m *teamBarrierModel) Stream(ctx context.Context, req types.ModelRequest, onEvent func(types.ModelEvent) error) error {
	_, err := m.Generate(ctx, req)
	return err
}

func (*teamBarrierModel) ProviderName() string { return "team-barrier" }
func (*teamBarrierModel) DiscoverCapabilities(context.Context, types.ModelRequest) (types.ProviderCapabilities, error) {
	return types.ProviderCapabilities{Provider: "team-barrier", Support: map[types.ModelCapability]types.CapabilitySupport{types.ModelCapabilityStreaming: types.CapabilitySupportSupported}}, nil
}

func (m *blockingModel) Generate(ctx context.Context, _ types.ModelRequest) (types.ModelResponse, error) {
	m.once.Do(func() { close(m.started) })
	<-ctx.Done()
	return types.ModelResponse{}, ctx.Err()
}
func (m *blockingModel) Stream(ctx context.Context, _ types.ModelRequest, _ func(types.ModelEvent) error) error {
	_, err := m.Generate(ctx, types.ModelRequest{})
	return err
}
func (*blockingModel) ProviderName() string { return "blocking" }
func (*blockingModel) DiscoverCapabilities(context.Context, types.ModelRequest) (types.ProviderCapabilities, error) {
	return types.ProviderCapabilities{Provider: "blocking", Support: map[types.ModelCapability]types.CapabilitySupport{types.ModelCapabilityStreaming: types.CapabilitySupportSupported}}, nil
}
