package baymax

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/FelixSeptem/baymax/core/history"
	"github.com/FelixSeptem/baymax/core/types"
	"github.com/FelixSeptem/baymax/orchestration/composer"
)

// These fixtures reproduce the two Baymax upstream findings against the
// currently pinned module version. They cover public contracts only: they do
// not claim that Danny's production projection transaction or any execution
// form has been promoted to a durable, cross-process restore path.

func pinnedOwnerSource() types.SourceRef {
	return types.SourceRef{
		Kind:      types.HistorySourceRunner,
		Namespace: "runner.history.v1",
		ID:        "danny-pinned-owner-store",
	}
}

func TestPinnedOwnerStoreRequiresExplicitDurableBinding(t *testing.T) {
	source := pinnedOwnerSource()

	if _, err := history.OpenOwnerStore(history.OwnerStoreOptions{Source: source}); !errors.Is(err, types.ErrHistoryUnsupported) {
		t.Fatalf("unbound owner store error = %v, want ErrHistoryUnsupported", err)
	}

	root := t.TempDir()
	_, err := history.OpenOwnerStore(history.OwnerStoreOptions{
		Source:            source,
		JournalPath:       filepath.Join(root, "history.json"),
		DeploymentRequest: types.DeploymentRequest{Profile: types.DeploymentProfileCrossMachine},
	})
	if !errors.Is(err, types.ErrDeploymentUnsupported) {
		t.Fatalf("cross-machine request error = %v, want ErrDeploymentUnsupported", err)
	}

	store, err := history.OpenOwnerStore(history.OwnerStoreOptions{
		Source:      source,
		JournalPath: filepath.Join(root, "history.json"),
	})
	if err != nil {
		t.Fatal(err)
	}
	caps, err := store.Capabilities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !caps.Durable || caps.Deployment.Profile != types.DeploymentProfileSameHostSingleWriterSequentialReopen {
		t.Fatalf("owner store capabilities = %+v, want durable same-host single-writer profile", caps)
	}
}

func TestPinnedOwnerStoreReplaysCommittedFactAfterFreshOpen(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	source := pinnedOwnerSource()
	journalPath := filepath.Join(root, "history.json")
	ledgerPath := filepath.Join(root, "ledger.json")

	first, err := history.OpenOwnerStore(history.OwnerStoreOptions{Source: source, JournalPath: journalPath})
	if err != nil {
		t.Fatal(err)
	}
	identity, err := first.Append(ctx, types.NativeExecutionEvent{
		Version:    types.NativeExecutionEventVersionV1,
		NativeType: "runner.completed",
	})
	if err != nil {
		t.Fatal(err)
	}

	// A fresh owner binding stands in for a new process: the source fact must
	// be readable from the retained journal, not from in-memory state.
	reopened, err := history.OpenOwnerStore(history.OwnerStoreOptions{
		Source:               source,
		JournalPath:          journalPath,
		ProjectionLedgerPath: ledgerPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	page, err := reopened.ReadAfter(ctx, nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 1 || page.Events[0].History.EventID != identity.EventID {
		t.Fatalf("replayed source page = %+v, want one event with id %q", page, identity.EventID)
	}

	duplicateIdentity, err := reopened.Append(ctx, page.Events[0])
	if err != nil || duplicateIdentity != identity {
		t.Fatalf("duplicate append = %+v err=%v, want original identity %+v", duplicateIdentity, err, identity)
	}
	conflicting := page.Events[0]
	conflicting.NativeType = "runner.conflicting"
	if _, err := reopened.Append(ctx, conflicting); !errors.Is(err, types.ErrHistoryConflict) {
		t.Fatalf("conflicting append error = %v, want ErrHistoryConflict", err)
	}

	firstReconcile, err := reopened.ReconcileProjection(ctx, page.Events[0])
	if err != nil || firstReconcile.Duplicate {
		t.Fatalf("first projection reconciliation = %+v err=%v", firstReconcile, err)
	}
	secondReconcile, err := reopened.ReconcileProjection(ctx, page.Events[0])
	if err != nil || !secondReconcile.Duplicate {
		t.Fatalf("duplicate projection reconciliation = %+v err=%v", secondReconcile, err)
	}
}

func TestPinnedContinuationOwnersFailClosedWithoutConfiguredRestore(t *testing.T) {
	unconfigured := &composer.Composer{}
	if _, err := unconfigured.ContinuationCapabilities(context.Background()); !errors.Is(err, types.ErrCheckpointUnsupported) {
		t.Fatalf("unconfigured composer continuation error = %v, want ErrCheckpointUnsupported", err)
	}

	// A capability document cannot claim restore support with a partial pause
	// matrix; every canonical reason needs its own source evidence.
	partial := types.ContinuationCapabilities{
		ProfileVersion:      types.NativeContinuationVersion,
		SourceNamespace:     "child",
		ContractVersion:     "baymax.runtime.contract.v1",
		ModuleRevision:      "composer-child-continuation",
		APIVersion:          "host_runtime_capability_manifest.v1",
		RetentionPolicy:     "bounded",
		ExpiryPolicy:        "bounded",
		CheckpointCreation:  true,
		CrossProcessRestore: true,
		PauseReasonEvidence: map[string]types.PauseReasonCapability{
			"input_required":    {Supported: true, EvidenceReference: "test:partial/input_required"},
			"approval_required": {Supported: true, EvidenceReference: "test:partial/approval_required"},
		},
	}
	if err := partial.Validate(); err == nil {
		t.Fatal("partial pause-reason matrix validated; want fail-closed rejection")
	}
	if capability, ok := partial.PauseReason("budget_exhausted"); ok && capability.Supported {
		t.Fatal("undeclared pause reason reported as supported")
	}
}
