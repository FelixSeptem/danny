package baymax

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/FelixSeptem/baymax/core/history"
	"github.com/FelixSeptem/baymax/core/runner"
	bmtypes "github.com/FelixSeptem/baymax/core/types"
	"github.com/FelixSeptem/danny/internal/runtimeboundary"
)

const runnerHistoryNamespace = "danny.baymax.runner.history.v1"

// OwnerStoreConfig declares the deployment in which Danny keeps its retained
// runner source facts. It is deliberately explicit: an empty Root leaves the
// process-local probe history in place, so no durable claim is made by default.
//
// DeploymentRequest is checked against the source-owned scope at bind time.
// A cross-machine or multi-writer request is rejected rather than silently
// downgraded to the file store's same-host single-writer profile.
type OwnerStoreConfig struct {
	Root                string
	Retention           int
	ProjectionRetention int
	DeploymentRequest   bmtypes.DeploymentRequest
}

// ownerStoreRuntime is the product-side binding to upstream change 196.
// The runner keeps execution ownership; Danny owns only the retained source
// facts and the projection reconciliation boundary.
type ownerStoreRuntime struct {
	config OwnerStoreConfig
}

func (c OwnerStoreConfig) validate() error {
	if strings.TrimSpace(c.Root) == "" {
		return fmt.Errorf("%w: owner store root is empty", ErrAuthorizedRequest)
	}
	scope := bmtypes.SameHostSingleWriterScope(runnerHistoryNamespace, "danny:owner-store.v1")
	if err := scope.Check(c.DeploymentRequest); err != nil {
		return fmt.Errorf("owner store deployment: %w", err)
	}
	if err := os.MkdirAll(c.Root, 0o700); err != nil {
		return fmt.Errorf("owner store root: %w", err)
	}
	return nil
}

// BindOwnerStore opts a client into an explicit, durable source-owner binding.
// Existing clients created with New or NewWithContinuation are unaffected.
func (c *Client) BindOwnerStore(config OwnerStoreConfig) error {
	if c == nil {
		return ErrAuthorizedRequest
	}
	if err := config.validate(); err != nil {
		return err
	}
	c.owner = &ownerStoreRuntime{config: config}
	return nil
}

// NewWithOwnerStore is the convenience constructor for a bound client.
func NewWithOwnerStore(model bmtypes.ModelClient, config OwnerStoreConfig, opts ...runner.Option) (*Client, error) {
	client, err := New(model, opts...)
	if err != nil {
		return nil, err
	}
	if err := client.BindOwnerStore(config); err != nil {
		return nil, err
	}
	return client, nil
}

// OwnerDeploymentScope publishes the detached scope evidence for this binding.
func (c *Client) OwnerDeploymentScope(runID string) (bmtypes.DeploymentScope, error) {
	if c == nil || c.owner == nil {
		return bmtypes.DeploymentScope{}, ErrCapabilityNotWired
	}
	store, _, err := c.owner.openRecoveryStore(runID)
	if err != nil {
		return bmtypes.DeploymentScope{}, err
	}
	defer store.Close()
	return store.DeploymentScope(), nil
}

func (o *ownerStoreRuntime) source(runID string) bmtypes.SourceRef {
	return bmtypes.SourceRef{Kind: bmtypes.HistorySourceRunner, Namespace: runnerHistoryNamespace, ID: runID}
}

// paths derives traversal-free, per-run store paths. Run IDs are authority
// supplied but never trusted as path components.
func (o *ownerStoreRuntime) paths(runID string) (journal, ledger string) {
	sum := sha256.Sum256([]byte(runID))
	key := hex.EncodeToString(sum[:16])
	return filepath.Join(o.config.Root, key+"-history.json"), filepath.Join(o.config.Root, key+"-projection.json")
}

// openExecutionJournal is the execution-phase source owner. Source facts are
// committed to a durable journal first; the projection ledger is intentionally
// left to the recovery phase so a crash before product projection is replayable.
func (o *ownerStoreRuntime) openExecutionJournal(runID string) (*history.Journal, bmtypes.SourceRef, error) {
	journalPath, _ := o.paths(runID)
	source := o.source(runID)
	var options []history.Option
	if o.config.Retention > 0 {
		options = append(options, history.WithRetention(o.config.Retention))
	}
	journal, err := runner.OpenNativeEventHistory(journalPath, source, options...)
	return journal, source, err
}

// openRecoveryStore is the recovery-phase owner: a sequential fresh open of
// the same source with an explicit projection ledger and deployment check.
func (o *ownerStoreRuntime) openRecoveryStore(runID string) (*history.OwnerStore, bmtypes.SourceRef, error) {
	journalPath, ledgerPath := o.paths(runID)
	source := o.source(runID)
	store, err := history.OpenOwnerStore(history.OwnerStoreOptions{
		Source:               source,
		JournalPath:          journalPath,
		ProjectionLedgerPath: ledgerPath,
		Retention:            o.config.Retention,
		ProjectionRetention:  o.config.ProjectionRetention,
		DeploymentRequest:    o.config.DeploymentRequest,
	})
	return store, source, err
}

// newSourceHistory keeps the existing in-memory probe history unless an owner
// binding was explicitly configured.
func (c *Client) newSourceHistory(runID string, source bmtypes.SourceRef) (*history.Journal, error) {
	if c != nil && c.owner != nil {
		journal, _, err := c.owner.openExecutionJournal(runID)
		return journal, err
	}
	return runner.NewNativeEventHistory(source)
}

// reconcileOwnerStore resolves an uncertain dispatch from retained source
// facts only. It never activates an execution, retries an effect, or creates a
// replacement journal. ReconcileProjection is the product boundary's idempotent
// deduplication: a source fact that was committed before a projection crash is
// replayed into the ledger without re-running the parent/child effects.
func (c *Client) reconcileOwnerStore(ctx context.Context, req runtimeboundary.RecoveryRequest) (runtimeboundary.RecoveryOutcome, error) {
	runID := req.SourceID
	if runID == "" {
		runID = req.NativeRunID
	}
	store, _, err := c.owner.openRecoveryStore(runID)
	if err != nil {
		if isOwnerStoreUnavailable(err) {
			return runtimeboundary.RecoveryOutcome{Status: runtimeboundary.RecoveryMissing}, nil
		}
		return runtimeboundary.RecoveryOutcome{Status: runtimeboundary.RecoveryGap}, err
	}
	defer store.Close()

	caps, err := store.Capabilities(ctx)
	if err != nil || !caps.Supported || !caps.Durable || !caps.SequenceDefined || caps.CursorVersion == "" || caps.TerminalRecovery == "" || caps.TerminalRecovery == "none" {
		return runtimeboundary.RecoveryOutcome{Status: runtimeboundary.RecoveryGap}, nil
	}
	page, err := store.ReadAfter(ctx, nil, bmtypes.MaxHistoryPageSize)
	if err != nil {
		if isOwnerStoreGap(err) {
			return runtimeboundary.RecoveryOutcome{Status: runtimeboundary.RecoveryGap}, nil
		}
		return runtimeboundary.RecoveryOutcome{Status: runtimeboundary.RecoveryMissing}, nil
	}
	if page.NextCursor == nil || page.NextCursor.Value == "" {
		return runtimeboundary.RecoveryOutcome{Status: runtimeboundary.RecoveryGap}, nil
	}

	// Reconcile retained facts in source order. Duplicate results are the
	// expected outcome of an idempotent replay, not a conflict.
	for index := range page.Events {
		if _, reconcileErr := store.ReconcileProjection(ctx, page.Events[index]); reconcileErr != nil {
			return runtimeboundary.RecoveryOutcome{Status: runtimeboundary.RecoveryGap, SourceCursor: page.NextCursor.Value}, nil
		}
	}

	var terminal *bmtypes.NativeExecutionEvent
	for index := range page.Events {
		if page.Events[index].IsTerminal() {
			copy := page.Events[index]
			terminal = &copy
		}
	}
	if terminal == nil || terminal.History == nil {
		return runtimeboundary.RecoveryOutcome{Status: runtimeboundary.RecoveryMissing, SourceCursor: page.NextCursor.Value}, nil
	}
	status := terminal.SourceState()
	if status == "" {
		return runtimeboundary.RecoveryOutcome{Status: runtimeboundary.RecoveryGap, SourceCursor: page.NextCursor.Value}, nil
	}
	sequence := int64(terminal.History.Sequence)
	return runtimeboundary.RecoveryOutcome{
		Status:           runtimeboundary.RecoveryTerminal,
		NativeRunID:      terminal.History.Source.ID,
		NativeEventID:    terminal.History.EventID,
		NativeVersion:    bmtypes.NativeExecutionEventVersionV1,
		NativeSequence:   &sequence,
		SourceSequence:   sequence,
		SourceCursor:     page.NextCursor.Value,
		ObservationScope: req.ExecutionID,
		TerminalStatus:   normalizeTerminalState(status),
	}, nil
}

func isOwnerStoreUnavailable(err error) bool {
	for _, target := range []error{bmtypes.ErrHistoryUnsupported, bmtypes.ErrSourceUnavailable, os.ErrNotExist} {
		if errors.Is(err, target) {
			return true
		}
	}
	return false
}

func isOwnerStoreGap(err error) bool {
	for _, target := range []error{bmtypes.ErrCursorExpired, bmtypes.ErrHistoryGap, bmtypes.ErrHistoryConflict, bmtypes.ErrCursorForeign, bmtypes.ErrDeploymentUnsupported} {
		if errors.Is(err, target) {
			return true
		}
	}
	return false
}
