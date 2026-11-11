package baymax

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/FelixSeptem/baymax/core/history"
	bmtypes "github.com/FelixSeptem/baymax/core/types"
	"github.com/FelixSeptem/baymax/orchestration/composer"
	"github.com/FelixSeptem/baymax/orchestration/mailbox"
	"github.com/FelixSeptem/baymax/orchestration/scheduler"
	"github.com/FelixSeptem/baymax/orchestration/teams"
)

// NativePauseReasons are the three pause reasons the Milestone 8 exit gate
// requires per execution form. Capability promotion is evidence-based: every
// reason must be declared separately by the configured source owner.
var NativePauseReasons = []string{"input_required", "approval_required", "budget_exhausted"}

// ContinuationOwnerConfig opts Danny's production adapter into upstream change
// 194/198 and the team continuation contract. A zero Root leaves the owners
// unconfigured, so unconfigured forms continue to fail closed.
type ContinuationOwnerConfig struct {
	Root           string
	OwnerNamespace string
	Retention      int
	// PauseBoundary declares where a team run may pause instead of failing.
	// Nil keeps the engine's always-dispatch behavior.
	PauseBoundary teams.TeamPauseController
	// Handler runs a mailbox delivery. Nil installs a no-op handler so the
	// read-only capability probe can still be built.
	Handler mailbox.ContinuationWorkerHandler
	// Resumer continues a paused mailbox delivery. Nil installs a no-op
	// resumer, which is enough for probing but not for real restore.
	Resumer mailbox.ContinuationWorkerResumer
	// Revalidate re-checks authority before a paused delivery resumes.
	Revalidate mailbox.HandlerResumeRevalidator
}

type continuationOwners struct {
	config ContinuationOwnerConfig
}

func (c ContinuationOwnerConfig) validate() error {
	if strings.TrimSpace(c.Root) == "" {
		return fmt.Errorf("%w: continuation owner root is empty", ErrAuthorizedRequest)
	}
	if strings.TrimSpace(c.OwnerNamespace) == "" {
		return fmt.Errorf("%w: continuation owner namespace is empty", ErrAuthorizedRequest)
	}
	if err := os.MkdirAll(c.Root, 0o700); err != nil {
		return fmt.Errorf("continuation owner root: %w", err)
	}
	return nil
}

// BindContinuationOwners wires file-backed child, team and mailbox owners into
// this client. It does not promote any capability: promotion still requires the
// real integration path and the Milestone 8 live gate (task 7.3).
func (c *Client) BindContinuationOwners(config ContinuationOwnerConfig) error {
	if c == nil {
		return ErrAuthorizedRequest
	}
	if err := config.validate(); err != nil {
		return err
	}
	c.continuationOwners = &continuationOwners{config: config}
	return nil
}

func (o *continuationOwners) paths(form, runID string) string {
	sum := sha256.Sum256([]byte(form + ":" + runID))
	return filepath.Join(o.config.Root, form+"-"+hex.EncodeToString(sum[:12])+".json")
}

func (o *continuationOwners) historyOptions() []history.Option {
	if o.config.Retention > 0 {
		return []history.Option{history.WithRetention(o.config.Retention)}
	}
	return nil
}

// childComposer builds the production child composer with an explicitly
// configured file owner, durable child source history and restore executor.
func (c *Client) childComposer(runID string) (*composer.Composer, error) {
	if c.continuationOwners == nil {
		return composer.NewBuilder(c.model).WithLocalRegistry(c.registry).Build()
	}
	owner := c.continuationOwners
	source := bmtypes.SourceRef{Kind: bmtypes.HistorySourceChild, Namespace: owner.config.OwnerNamespace + ".child.v1", ID: runID}
	journal, err := composer.OpenNativeEventHistory(owner.paths("child-history", runID), source, owner.historyOptions()...)
	if err != nil {
		return nil, err
	}
	store, err := composer.NewFileChildContinuationStore(owner.paths("child-continuation", runID), "child")
	if err != nil {
		return nil, err
	}
	queue, err := scheduler.NewFileStore(owner.paths("child-scheduler", runID))
	if err != nil {
		return nil, err
	}
	return composer.New(c.model,
		composer.WithLocalRegistry(c.registry),
		composer.WithSchedulerStore(queue),
		composer.WithNativeEventHistory(journal),
		composer.WithChildContinuationStore(store, "child"),
		composer.WithChildContinuationExecutor(composer.ChildContinuationExecutorFunc(func(_ context.Context, continuation composer.ChildContinuation) (scheduler.TerminalCommit, error) {
			return scheduler.TerminalCommit{
				TaskID:    continuation.Claimed.Record.Task.TaskID,
				AttemptID: continuation.Claimed.Attempt.AttemptID,
				Status:    scheduler.TaskStateSucceeded,
			}, nil
		})),
	)
}

// teamContinuationStore opens the source-owned team continuation store for a
// run. Both the pause path and the restore path go through this exact file.
func (c *Client) teamContinuationStore(runID string) (*teams.FileTeamContinuationStore, error) {
	if c.continuationOwners == nil {
		return nil, ErrCapabilityNotWired
	}
	owner := c.continuationOwners
	return teams.NewFileTeamContinuationStore(owner.paths("team-continuation", runID), owner.config.OwnerNamespace)
}

// teamEngine builds the production team engine with the configured file
// continuation owner and a durable team source journal. It returns the journal
// so the caller can replay retained native events through the same source.
func (c *Client) teamEngine(runID string, source bmtypes.SourceRef) (*teams.Engine, *history.Journal, error) {
	if c.continuationOwners == nil {
		journal, err := teams.NewNativeEventHistory(source)
		if err != nil {
			return nil, nil, err
		}
		return teams.New(teams.WithNativeEventHistory(journal)), journal, nil
	}
	owner := c.continuationOwners
	journal, err := teams.OpenNativeEventHistory(owner.paths("team-history", runID), source, owner.historyOptions()...)
	if err != nil {
		return nil, nil, err
	}
	store, err := c.teamContinuationStore(runID)
	if err != nil {
		return nil, nil, err
	}
	options := []teams.Option{
		teams.WithNativeEventHistory(journal),
		teams.WithTeamContinuationStore(store, owner.config.OwnerNamespace),
	}
	if owner.config.PauseBoundary != nil {
		options = append(options, teams.WithTeamPauseController(owner.config.PauseBoundary))
	}
	return teams.New(options...), journal, nil
}

// mailboxOwner builds a durable mailbox with a handler-safe-point continuation
// worker. The worker must be used instead of the lifecycle-only worker for
// handler pause restore.
func (c *Client) mailboxOwner(runID string) (*mailbox.Mailbox, *mailbox.Worker, error) {
	if c.continuationOwners == nil {
		return nil, nil, ErrCapabilityNotWired
	}
	owner := c.continuationOwners
	store, err := mailbox.NewFileStore(owner.paths("mailbox", runID), mailbox.Policy{MaxAttempts: 2, DLQEnabled: true})
	if err != nil {
		return nil, nil, err
	}
	mb, err := mailbox.New(store)
	if err != nil {
		return nil, nil, err
	}
	handler := owner.config.Handler
	if handler == nil {
		handler = func(context.Context, mailbox.Record) (mailbox.HandlerResult, error) {
			return mailbox.HandlerResult{}, nil
		}
	}
	resumer := owner.config.Resumer
	if resumer == nil {
		resumer = func(context.Context, mailbox.MailboxContinuation, mailbox.Record) (mailbox.HandlerResult, error) {
			return mailbox.HandlerResult{}, nil
		}
	}
	revalidate := owner.config.Revalidate
	if revalidate == nil {
		revalidate = func(context.Context, mailbox.MailboxContinuation) error { return nil }
	}
	worker, err := mailbox.NewContinuationWorker(mb, mailbox.WorkerConfig{Enabled: true}, handler, resumer, revalidate, "danny-continuation-consumer")
	if err != nil {
		return nil, nil, err
	}
	return mb, worker, nil
}

// NativeContinuationEvidence is a detached, product-neutral projection of an
// upstream owner's capability declaration.
type NativeContinuationEvidence struct {
	Form        string                           `json:"form"`
	Owner       string                           `json:"owner"`
	Supported   bool                             `json:"supported"`
	PauseReason map[string]bool                  `json:"pause_reason"`
	Deployment  bmtypes.DeploymentScope          `json:"deployment"`
	Reason      string                           `json:"reason,omitempty"`
	Raw         bmtypes.ContinuationCapabilities `json:"-"`
}

// ContinuationCapabilities is the production-facing capability probe. It is
// deliberately read-only: it never creates a checkpoint or resumes an effect.
func (c *Client) ContinuationCapabilities(ctx context.Context, runID string) ([]NativeContinuationEvidence, error) {
	if c == nil || c.continuationOwners == nil {
		return nil, ErrCapabilityNotWired
	}
	child, err := c.childComposer(runID)
	if err != nil {
		return nil, err
	}
	childCaps, childErr := child.ContinuationCapabilities(ctx)
	teamSource := bmtypes.SourceRef{Kind: bmtypes.HistorySourceTeam, Namespace: c.continuationOwners.config.OwnerNamespace + ".team.v1", ID: runID}
	engine, _, err := c.teamEngine(runID, teamSource)
	if err != nil {
		return nil, err
	}
	teamCaps, teamErr := engine.ContinuationCapabilities(ctx)
	mb, _, err := c.mailboxOwner(runID)
	if err != nil {
		return nil, err
	}
	mailboxCaps, mailboxErr := mb.ContinuationCapabilities(ctx)

	return []NativeContinuationEvidence{
		evidenceFrom("subagent_fanout", "composer.child", childCaps, childErr),
		evidenceFrom("team_parallel", "teams.engine", teamCaps, teamErr),
		evidenceFrom("mailbox_async", "mailbox.handler", mailboxCaps, mailboxErr),
	}, nil
}

func evidenceFrom(form, owner string, caps bmtypes.ContinuationCapabilities, err error) NativeContinuationEvidence {
	out := NativeContinuationEvidence{
		Form: form, Owner: owner, Raw: caps,
		PauseReason: map[string]bool{}, Deployment: caps.Deployment,
	}
	if err != nil {
		out.Reason = err.Error()
		return out
	}
	if validateErr := caps.Validate(); validateErr != nil {
		out.Reason = "invalid capability document: " + validateErr.Error()
		return out
	}
	for _, reason := range NativePauseReasons {
		entry, ok := caps.PauseReason(reason)
		out.PauseReason[reason] = ok && entry.Supported
	}
	out.Supported = out.PauseReason[NativePauseReasons[0]] && out.PauseReason[NativePauseReasons[1]] && out.PauseReason[NativePauseReasons[2]] && caps.CheckpointCreation && caps.CrossProcessRestore
	return out
}
