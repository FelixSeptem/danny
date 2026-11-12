package baymax

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/FelixSeptem/baymax/core/history"
	"github.com/FelixSeptem/baymax/core/runner"
	bmtypes "github.com/FelixSeptem/baymax/core/types"
	"github.com/FelixSeptem/baymax/integration/fakes"
	"github.com/FelixSeptem/danny/internal/runtimeboundary"
)

// These reproductions run against Danny's production adapter path, not an
// upstream package fixture. They exercise the exact crash window described in
// docs/upstream/baymax-findings.md: source facts are committed to the owner
// journal before the product projection is written.

func ownerRecoveryRequest(runID string) runtimeboundary.RecoveryRequest {
	return runtimeboundary.RecoveryRequest{
		Version:         runtimeboundary.ExecutionContractV1,
		ExecutionID:     "product-" + runID,
		NativeRunID:     runID,
		SourceKind:      "runner",
		SourceNamespace: runnerHistoryNamespace,
		SourceID:        runID,
		Authority: runtimeboundary.Authority{
			TaskRevision: 1, PlanVersion: "plan-v1", PlanDigest: "plan-digest",
			CapabilityDigest: "cap-v1", ProjectionDigest: "projection-v1",
			WriteScope: "none", EffectNamespace: "product-" + runID + ":effects",
		},
	}
}

func runOwnerExecution(t *testing.T, root, runID string) {
	t.Helper()
	client, err := NewWithOwnerStore(fakes.NewModel([]fakes.ModelStep{{Response: bmtypes.ModelResponse{FinalAnswer: "owner answer"}}}), OwnerStoreConfig{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.RunAuthorized(context.Background(), AuthorizedRequest{
		RunID: runID, CorrelationID: runID + ":session", PlanVersion: "plan-v1",
		PlanDigest: "plan-digest", CapabilityDigest: "cap-v1", ProjectionDigest: "projection-v1",
		Input: "owner input",
	}); err != nil {
		t.Fatal(err)
	}
	// Dropping the client stands in for a process exit: no in-memory reader
	// survives into the recovery phase below.
}

func TestOwnerStoreBindingFailsClosedOnUnsupportedDeployment(t *testing.T) {
	root := t.TempDir()
	model := fakes.NewModel([]fakes.ModelStep{{Response: bmtypes.ModelResponse{FinalAnswer: "x"}}})
	_, err := NewWithOwnerStore(model, OwnerStoreConfig{
		Root:              root,
		DeploymentRequest: bmtypes.DeploymentRequest{Profile: bmtypes.DeploymentProfileCrossMachine},
	})
	if !errors.Is(err, bmtypes.ErrDeploymentUnsupported) {
		t.Fatalf("cross-machine bind error = %v, want ErrDeploymentUnsupported", err)
	}
	if _, err := NewWithOwnerStore(model, OwnerStoreConfig{}); err == nil {
		t.Fatal("unbound owner store accepted")
	}
}

func TestOwnerStoreReplaysTerminalAcrossFreshProcess(t *testing.T) {
	root := t.TempDir()
	const runID = "fresh-process-run"
	runOwnerExecution(t, root, runID)

	// A brand new client has an empty nativeHistories map. The only way to
	// recover a terminal is a fresh owner-store open of the retained journal.
	fresh, err := NewWithOwnerStore(fakes.NewModel([]fakes.ModelStep{{Response: bmtypes.ModelResponse{FinalAnswer: "unused"}}}), OwnerStoreConfig{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	out, err := fresh.Reconcile(context.Background(), ownerRecoveryRequest(runID))
	if err != nil || out.Status != runtimeboundary.RecoveryTerminal {
		t.Fatalf("fresh-process recovery = %+v err=%v, want terminal", out, err)
	}
	if out.NativeRunID != runID || out.NativeEventID == "" || out.SourceCursor == "" || out.TerminalStatus != "succeeded" {
		t.Fatalf("incomplete terminal evidence: %+v", out)
	}
	if out.NativeSequence == nil || *out.NativeSequence <= 0 || out.SourceSequence != *out.NativeSequence {
		t.Fatalf("source sequence not projected: %+v", out)
	}
}

func TestOwnerStoreReconcilesProjectionIdempotentlyAfterCrashBeforeProjection(t *testing.T) {
	root := t.TempDir()
	const runID = "crash-before-projection-run"
	runOwnerExecution(t, root, runID)

	// The execution journal was written without a projection ledger, i.e. the
	// process died between source commit and product projection. Recovery must
	// replay source facts into the ledger without re-running any effect.
	fresh, err := NewWithOwnerStore(fakes.NewModel([]fakes.ModelStep{{Response: bmtypes.ModelResponse{FinalAnswer: "unused"}}}), OwnerStoreConfig{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	req := ownerRecoveryRequest(runID)
	first, err := fresh.Reconcile(context.Background(), req)
	if err != nil || first.Status != runtimeboundary.RecoveryTerminal {
		t.Fatalf("first reconciliation = %+v err=%v", first, err)
	}

	_, ledgerPath := fresh.owner.paths(runID)
	if _, statErr := os.Stat(ledgerPath); statErr != nil {
		t.Fatalf("projection ledger was not repaired: %v", statErr)
	}
	// The second replay must converge as a duplicate, not append a second
	// ledger record or invent a second terminal.
	second, err := fresh.Reconcile(context.Background(), req)
	if err != nil || second.Status != runtimeboundary.RecoveryTerminal || second.NativeEventID != first.NativeEventID || second.SourceSequence != first.SourceSequence {
		t.Fatalf("duplicate replay = %+v err=%v, want convergence with %+v", second, err, first)
	}
}

func TestOwnerStoreReportsGapWhenSourceFactsAreUnavailable(t *testing.T) {
	root := t.TempDir()
	client, err := NewWithOwnerStore(fakes.NewModel([]fakes.ModelStep{{Response: bmtypes.ModelResponse{FinalAnswer: "x"}}}), OwnerStoreConfig{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	// No execution ever committed source facts for this run. A fresh recover
	// must not fabricate a terminal or start a replacement run.
	out, err := client.Reconcile(context.Background(), ownerRecoveryRequest("never-executed"))
	if err != nil {
		t.Fatalf("missing source returned error: %v", err)
	}
	if out.Status == runtimeboundary.RecoveryTerminal {
		t.Fatalf("missing source fabricated terminal: %+v", out)
	}
	if out.Status != runtimeboundary.RecoveryGap && out.Status != runtimeboundary.RecoveryMissing {
		t.Fatalf("missing source status = %q, want gap or missing", out.Status)
	}
}

func TestOwnerStoreRetentionBoundsReplayAndRejectsExpiredCursor(t *testing.T) {
	root := t.TempDir()
	const runID = "retention-run"
	owner := &ownerStoreRuntime{config: OwnerStoreConfig{Root: root, Retention: 2}}
	journalPath, _ := owner.paths(runID)
	source := owner.source(runID)
	journal, err := runner.OpenNativeEventHistory(journalPath, source, history.WithRetention(2))
	if err != nil {
		t.Fatal(err)
	}
	// Seed four source facts so the retention floor advances past the first
	// cursor. An old cursor must then fail closed as expired, not silently
	// skip source facts.
	for index := 0; index < 4; index++ {
		if _, err := journal.Append(context.Background(), bmtypes.NativeExecutionEvent{
			Version: bmtypes.NativeExecutionEventVersionV1, NativeType: "run.started",
		}); err != nil {
			t.Fatal(err)
		}
	}
	store, err := history.OpenOwnerStore(history.OwnerStoreOptions{
		Source: source, JournalPath: journalPath,
		ProjectionLedgerPath: filepath.Join(root, "retention-ledger.json"),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	page, err := store.ReadAfter(context.Background(), nil, bmtypes.MaxHistoryPageSize)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 2 || page.RetentionFloor == nil {
		t.Fatalf("bounded replay = %d events, floor=%+v, want 2 with a floor", len(page.Events), page.RetentionFloor)
	}
	// A cursor older than the floor is a gap: the source cannot prove that
	// nothing was lost between the cursor and the retained facts.
	expired := bmtypes.OpaqueCursor{Value: "stale-cursor"}
	if _, err := store.ReadAfter(context.Background(), &expired, bmtypes.MaxHistoryPageSize); err == nil {
		t.Fatal("expired cursor replay accepted")
	}
}
