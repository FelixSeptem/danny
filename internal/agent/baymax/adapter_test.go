package baymax

import (
	"context"
	"testing"

	"github.com/FelixSeptem/baymax/core/types"
	"github.com/FelixSeptem/baymax/integration/fakes"
	"github.com/FelixSeptem/danny/internal/runtimeprobe"
)

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
