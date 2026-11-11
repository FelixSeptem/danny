package baymax

import (
	"context"
	"errors"
	"strings"
	"testing"

	bmtypes "github.com/FelixSeptem/baymax/core/types"
	"github.com/FelixSeptem/baymax/integration/fakes"
)

// These fixtures wire the upstream 194/198 and team continuation owners into
// Danny's production adapter and verify the per-pause-reason capability matrix.
// They are deterministic source evidence, not a live integration claim: the
// Milestone 8 live gate (task 7.3) still has to pass before any capability can
// be promoted in the manifest.

func TestUnconfiguredContinuationOwnersFailClosed(t *testing.T) {
	client, err := New(fakes.NewModel([]fakes.ModelStep{{Response: bmtypes.ModelResponse{FinalAnswer: "x"}}}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.ContinuationCapabilities(context.Background(), "unconfigured-run"); !errors.Is(err, ErrCapabilityNotWired) {
		t.Fatalf("unconfigured capability probe error = %v, want ErrCapabilityNotWired", err)
	}
	if err := client.BindContinuationOwners(ContinuationOwnerConfig{OwnerNamespace: "danny"}); err == nil {
		t.Fatal("continuation owner with empty root accepted")
	}
}

func TestConfiguredOwnersPublishCompletePauseReasonMatrix(t *testing.T) {
	client, err := New(fakes.NewModel([]fakes.ModelStep{{Response: bmtypes.ModelResponse{FinalAnswer: "x"}}}))
	if err != nil {
		t.Fatal(err)
	}
	if err := client.BindContinuationOwners(ContinuationOwnerConfig{Root: t.TempDir(), OwnerNamespace: "danny"}); err != nil {
		t.Fatal(err)
	}
	evidence, err := client.ContinuationCapabilities(context.Background(), "capability-run")
	if err != nil {
		t.Fatal(err)
	}
	if len(evidence) != 3 {
		t.Fatalf("evidence count = %d, want child/team/mailbox", len(evidence))
	}
	for _, item := range evidence {
		if !item.Supported {
			t.Errorf("%s (%s) not supported: reason=%q pause=%+v deployment=%+v", item.Form, item.Owner, item.Reason, item.PauseReason, item.Deployment)
			continue
		}
		for _, reason := range NativePauseReasons {
			if !item.PauseReason[reason] {
				t.Errorf("%s does not evidence pause reason %q", item.Form, reason)
			}
		}
		if item.Deployment.Profile != bmtypes.DeploymentProfileSameHostSingleWriterSequentialReopen {
			t.Errorf("%s deployment profile = %q, want same-host single-writer", item.Form, item.Deployment.Profile)
		}
		if err := item.Raw.Validate(); err != nil {
			t.Errorf("%s capability document invalid: %v", item.Form, err)
		}
	}
}

func TestPartialPauseMatrixCannotBePromoted(t *testing.T) {
	partial := bmtypes.ContinuationCapabilities{
		ProfileVersion: bmtypes.NativeContinuationVersion, SourceNamespace: "child",
		ContractVersion: "baymax.runtime.contract.v1", ModuleRevision: "partial",
		APIVersion: "host_runtime_capability_manifest.v1", RetentionPolicy: "bounded", ExpiryPolicy: "bounded",
		CheckpointCreation: true, CrossProcessRestore: true,
		PauseReasonEvidence: map[string]bmtypes.PauseReasonCapability{
			"input_required":    {Supported: true, EvidenceReference: "test:partial/input_required"},
			"approval_required": {Supported: true, EvidenceReference: "test:partial/approval_required"},
			"budget_exhausted":  {Supported: false, UnsupportedReason: "not evidenced"},
		},
	}
	item := evidenceFrom("subagent_fanout", "composer.child", partial, nil)
	if item.Supported || item.PauseReason["budget_exhausted"] {
		t.Fatalf("partial matrix promoted: %+v", item)
	}
	if !strings.Contains(item.Reason, "invalid capability document") {
		t.Fatalf("partial matrix reason = %q", item.Reason)
	}
}
