package runtimeboundary

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestCapabilityManifestIsDeterministicAndRejectsTampering(t *testing.T) {
	m := DefaultCapabilityManifest()
	if err := m.Validate(); err != nil || !m.DigestMatches() {
		t.Fatalf("manifest invalid: %v", err)
	}
	m.Capabilities[0].Reason = "tampered"
	if m.DigestMatches() {
		t.Fatal("tampered manifest digest accepted")
	}
	if !errors.Is(m.Allows(FormParallelTeam), ErrCapabilityBlocked) && !errors.Is(m.Allows(FormParallelTeam), ErrCapabilityMissing) {
		t.Fatal("unexpected capability error")
	}
}

func TestCapabilityManifestCoversFormsAndRecordsModeSpecificEvidence(t *testing.T) {
	manifest := DefaultCapabilityManifest()
	if len(manifest.Capabilities) != 7 {
		t.Fatalf("capability count=%d, want 7", len(manifest.Capabilities))
	}
	for _, form := range []ExecutionForm{FormDirect, FormSubagent, FormParallelTeam, FormVoteTeam, FormWorkflow, FormMailbox, FormScheduled} {
		capability, err := manifest.Lookup(form)
		if err != nil {
			t.Fatalf("lookup %s: %v", form, err)
		}
		if capability.Dependency != "baymax" || capability.Pin != "v0.0.0-20261103091945-d2e08cd6dc8c" || capability.PublicAPI == "" || capability.Evidence == "" {
			t.Fatalf("incomplete evidence for %s: %#v", form, capability)
		}
	}
	parallel, _ := manifest.Lookup(FormParallelTeam)
	if parallel.Status != CapabilityUnsupported || !strings.Contains(parallel.Reason, "pause/restore") {
		t.Fatalf("parallel capability incorrectly advertised: %#v", parallel)
	}
}

func TestExecutionRequestRoundTripAndAuthorityValidation(t *testing.T) {
	r := ExecutionRequest{Version: ExecutionContractV1, ExecutionID: "run-1", CorrelationID: "corr-1", Form: FormDirect, Authority: Authority{TaskRevision: 7, PlanVersion: "plan.v1", PlanDigest: "plan", CapabilityDigest: "cap", ProjectionDigest: "projection", WriteScope: "task:7", EffectNamespace: "task:run-1"}, Input: "answer"}
	encoded, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var decoded ExecutionRequest
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if err := decoded.Validate(DefaultCapabilityManifest()); err != nil {
		t.Fatal(err)
	}
	decoded.Version = "danny.execution.v9"
	if !errors.Is(decoded.Validate(DefaultCapabilityManifest()), ErrUnsupportedVersion) {
		t.Fatal("unknown version accepted")
	}
}

func TestExecutionBoundaryDTOsRoundTripAndRejectIncompleteAuthority(t *testing.T) {
	request := ExecutionRequest{Version: ExecutionContractV1, ExecutionID: "run-1", ParentRunID: "parent-1", CorrelationID: "corr-1", Form: FormDirect, Authority: Authority{TaskRevision: 9, PlanVersion: "plan.v2", PlanDigest: "plan-digest", CapabilityDigest: "cap-digest", ProjectionDigest: "projection-digest", WriteScope: "task:9", EffectNamespace: "task:9"}, Input: "input"}
	event := Event{Version: ExecutionContractV1, ExecutionID: "run-1", NativeID: "native-1", NativeParent: "parent-1", Kind: "tool.finished", Status: "succeeded", CorrelationID: "corr-1"}
	outcome := Outcome{Version: ExecutionContractV1, ExecutionID: "run-1", NativeRunID: "native-1", Status: "succeeded", TerminalEvent: "event-2"}
	control := ControlRequest{Version: ExecutionContractV1, ExecutionID: "run-1", NativeRunID: "native-1", Action: "cancel", CausationID: "event-1", IdempotencyKey: "cancel:run-1"}
	checkpoint := CheckpointBinding{Version: ExecutionContractV1, ExecutionID: "run-1", NativeID: "checkpoint-1", NativeVersion: "native.checkpoint.v1", NativeDigest: "checkpoint-digest", SourceCursor: "opaque-cursor", Authority: request.Authority, PauseReason: "approval_required"}

	encoded, err := json.Marshal([]any{request, event, outcome, control, checkpoint})
	if err != nil {
		t.Fatal(err)
	}
	var decoded []json.RawMessage
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	var gotRequest ExecutionRequest
	var gotEvent Event
	var gotOutcome Outcome
	var gotControl ControlRequest
	var gotCheckpoint CheckpointBinding
	for i, target := range []any{&gotRequest, &gotEvent, &gotOutcome, &gotControl, &gotCheckpoint} {
		if err := json.Unmarshal(decoded[i], target); err != nil {
			t.Fatal(err)
		}
	}
	if gotRequest.Authority != request.Authority || gotRequest.ParentRunID != request.ParentRunID || gotEvent.NativeParent != event.NativeParent || gotEvent.CorrelationID != event.CorrelationID || gotOutcome.TerminalEvent != outcome.TerminalEvent || gotControl.CausationID != control.CausationID || gotControl.IdempotencyKey != control.IdempotencyKey || gotCheckpoint.Authority != checkpoint.Authority || gotCheckpoint.SourceCursor != checkpoint.SourceCursor {
		t.Fatal("execution boundary round-trip lost authority or native correlation")
	}
	for name, err := range map[string]error{
		"event":      (Event{Version: "unknown", ExecutionID: "run", NativeID: "native", Kind: "run.started", CorrelationID: "corr"}).Validate(),
		"outcome":    (Outcome{Version: ExecutionContractV1, ExecutionID: "run", NativeRunID: "native", Status: "succeeded"}).Validate(),
		"control":    (ControlRequest{Version: ExecutionContractV1, ExecutionID: "run", NativeRunID: "native", Action: "cancel"}).Validate(),
		"checkpoint": (CheckpointBinding{Version: ExecutionContractV1, ExecutionID: "run", NativeID: "cp", NativeVersion: "v1", NativeDigest: "digest", PauseReason: "input_required"}).Validate(),
	} {
		if !errors.Is(err, ErrUnsupportedVersion) && name == "event" {
			t.Fatalf("unknown event version error = %v", err)
		}
		if err == nil && name != "event" {
			t.Fatalf("incomplete %s authority accepted", name)
		}
	}
	request.Authority.WriteScope = ""
	if !errors.Is(request.Validate(DefaultCapabilityManifest()), ErrMissingAuthority) {
		t.Fatal("missing write scope accepted")
	}
}

func TestRecoveryBoundaryPreservesSourceIdentityAndRejectsMissingAuthority(t *testing.T) {
	recovery := RecoveryRequest{Version: ExecutionContractV1, ExecutionID: "product-run", NativeRunID: "native-run", SourceKind: "runner", SourceNamespace: "history.v1", SourceID: "native-run", SourceCursor: "cursor-4", Authority: Authority{TaskRevision: 2, PlanVersion: "plan.v1", PlanDigest: "plan", CapabilityDigest: "cap", ProjectionDigest: "projection", WriteScope: "none", EffectNamespace: "product-run:effects"}}
	encoded, err := json.Marshal(recovery)
	if err != nil {
		t.Fatal(err)
	}
	var decoded RecoveryRequest
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if err := decoded.Validate(); err != nil || decoded.SourceID != recovery.SourceID || decoded.SourceCursor != recovery.SourceCursor || decoded.Authority != recovery.Authority {
		t.Fatalf("recovery round trip=%#v err=%v", decoded, err)
	}
	decoded.Authority.EffectNamespace = ""
	if !errors.Is(decoded.Validate(), ErrMissingAuthority) {
		t.Fatal("missing recovery authority accepted")
	}
	if (RecoveryOutcome{Status: RecoveryTerminal, NativeRunID: "native-run", NativeEventID: "terminal", NativeVersion: "native_execution_event.v1"}).Status != RecoveryTerminal {
		t.Fatal("recovery terminal status changed")
	}
}

func TestContextProjectionDigestChangesWithAuthorizedInput(t *testing.T) {
	req := ContextRequest{Version: ContextContractV1, RunID: "run", Scope: "tenant/project/ns", MemoryPath: "tasks/one", SessionID: "session", TurnID: "turn", Query: "q", ProjectionIntent: "task_context", Budget: ContextBudget{Unit: BudgetItems, Limit: 4}, MaxBytes: 1024, MaxReferences: 4, Role: "section_analyst", PolicyDigest: "policy"}
	if err := req.Validate(); err != nil {
		t.Fatal(err)
	}
	e := ContextEnvelope{Version: ContextContractV1, RunID: "run", Scope: req.Scope, MemoryPath: req.MemoryPath, SessionID: req.SessionID, TurnID: req.TurnID, Role: req.Role, ProjectionIntent: "task_context", Budget: req.Budget, PolicyDigest: req.PolicyDigest, SourceDigest: "source", DiagnosticsState: DiagnosticsOmitted, References: []ContextReference{{ID: "m1", Provenance: Provenance{SourceID: "m1"}}}}
	e.ProjectionDigest = e.ComputeProjectionDigest()
	if err := e.Validate(req); err != nil {
		t.Fatal(err)
	}
	e.References[0].ID = "m2"
	if e.ComputeProjectionDigest() == e.ProjectionDigest {
		t.Fatal("projection digest did not change")
	}
	e.References[0].ID = "m1"
	e.MemoryPath = "tasks/two"
	if e.ComputeProjectionDigest() == e.ProjectionDigest {
		t.Fatal("projection digest did not change with authorized path")
	}
}

func TestContextEnvelopeRejectsOmittedDiagnosticsAndWrongTurn(t *testing.T) {
	req := ContextRequest{Version: ContextContractV1, RunID: "run", Scope: "tenant/project/ns", SessionID: "session", TurnID: "turn", Query: "q", Budget: ContextBudget{Unit: BudgetItems, Limit: 1}, MaxBytes: 1024, MaxReferences: 1, PolicyDigest: "policy"}
	envelope := ContextEnvelope{Version: ContextContractV1, RunID: "run", Scope: req.Scope, SessionID: "session", TurnID: "wrong", Budget: req.Budget, PolicyDigest: "policy", SourceDigest: "source", DiagnosticsState: DiagnosticsOmitted, References: []ContextReference{{ID: "m", Citation: "m", Provenance: Provenance{SourceID: "m", Path: "tasks/m"}}}}
	envelope.ProjectionDigest = envelope.ComputeProjectionDigest()
	if err := envelope.Validate(req); err == nil {
		t.Fatal("wrong turn accepted")
	}
	envelope.TurnID = req.TurnID
	envelope.DiagnosticsState = DiagnosticsPresent
	envelope.Diagnostics = nil
	envelope.ProjectionDigest = envelope.ComputeProjectionDigest()
	if err := envelope.Validate(req); err == nil {
		t.Fatal("missing present diagnostics accepted")
	}
}
