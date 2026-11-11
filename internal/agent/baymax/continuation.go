package baymax

import (
	"context"
	"fmt"
	"strings"

	"github.com/FelixSeptem/baymax/core/checkpoint"
	"github.com/FelixSeptem/baymax/core/runner"
	bmtypes "github.com/FelixSeptem/baymax/core/types"
	"github.com/FelixSeptem/danny/internal/runtimeboundary"
)

type NativeBudget = runtimeboundary.NativeBudget
type CheckpointReference = runtimeboundary.CheckpointReference
type AuthorityBinding = runtimeboundary.AuthorityBinding
type NativeResumeRequest = runtimeboundary.NativeResumeRequest

type ContinuationConfig struct {
	StorePath          string
	OwnerNamespace     string
	DependencyProfile  string
	DependencyRevision string
	Approval           bool
	Budget             bool
	BudgetOwner        func(context.Context, CheckpointReference, NativeBudget, NativeBudget) (NativeBudget, error)
	Capacity           int
}

type continuationRuntime struct {
	config ContinuationConfig
	store  *checkpoint.FileStore
}

// NewWithContinuation creates an authorized client with a durable native
// checkpoint store. The directory is single-writer by Baymax's public store
// contract; close the old client before opening it in a fresh process.
func NewWithContinuation(model bmtypes.ModelClient, config ContinuationConfig, opts ...runner.Option) (*Client, error) {
	if strings.TrimSpace(config.StorePath) == "" || strings.TrimSpace(config.OwnerNamespace) == "" || strings.TrimSpace(config.DependencyProfile) == "" || strings.TrimSpace(config.DependencyRevision) == "" {
		return nil, ErrAuthorizedRequest
	}
	if config.Capacity <= 0 {
		config.Capacity = 64
	}
	store, err := checkpoint.OpenFileStore(config.StorePath, checkpoint.Options{Namespace: config.OwnerNamespace, Capacity: config.Capacity})
	if err != nil {
		return nil, err
	}
	client, err := New(model, opts...)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	client.continuation = &continuationRuntime{config: config, store: store}
	return client, nil
}

// Close releases the single-writer checkpoint store. It is safe for clients
// without continuation enabled.
func (c *Client) Close() error {
	if c == nil || c.continuation == nil || c.continuation.store == nil {
		return nil
	}
	return c.continuation.store.Close()
}

func (c *Client) continuationOptions(req AuthorizedRequest, options []runner.Option) ([]runner.Option, error) {
	if c == nil || c.continuation == nil {
		return options, nil
	}
	config := c.continuation.config
	if config.BudgetOwner == nil {
		return nil, fmt.Errorf("%w: continuation budget owner is required", ErrAuthorizedRequest)
	}
	binding := bmtypes.NativeCheckpointRef{
		SourceComponent: "runner", OwnerNamespace: config.OwnerNamespace,
		RunID: req.RunID, SessionID: req.CorrelationID,
		AuthorityDigest: req.CapabilityDigest, PlanDigest: req.PlanDigest,
		WriteScopeDigest: req.WriteScopeDigest, EffectNamespace: req.EffectNamespace, OccurrenceID: req.OccurrenceKey,
	}
	options = append(options, runner.WithNativeContinuation(runner.NativeContinuationOptions{
		Store: c.continuation.store, OwnerNamespace: config.OwnerNamespace,
		DependencyProfile: config.DependencyProfile, DependencyRevision: config.DependencyRevision,
		Binding: binding, Approval: config.Approval, Budget: config.Budget,
		BudgetOwner: func(ctx context.Context, ref bmtypes.NativeCheckpointRef, current, proposed bmtypes.DurableBudget) (bmtypes.DurableBudget, error) {
			out, err := config.BudgetOwner(ctx, checkpointReferenceFromNative(ref, current, ""), nativeBudgetFromBaymax(current), nativeBudgetFromBaymax(proposed))
			return nativeBudgetToBaymax(out), err
		},
	}))
	return options, nil
}

func (c *Client) ResumeAuthorized(ctx context.Context, req NativeResumeRequest) (ExecutionResult, error) {
	if c == nil || c.continuation == nil || req.Checkpoint.RunID == "" || req.Checkpoint.SessionID == "" || req.DecisionToken == "" || req.IdempotencyKey == "" {
		return ExecutionResult{}, ErrAuthorizedRequest
	}
	if err := validateResumeAuthority(req); err != nil {
		return ExecutionResult{}, err
	}
	ref := nativeReferenceFromCheckpoint(req.Checkpoint)
	source := bmtypes.SourceRef{Kind: bmtypes.HistorySourceRunner, Namespace: "danny.baymax.runner.history.v1", ID: ref.RunID}
	history, err := c.newSourceHistory(ref.RunID, source)
	if err != nil {
		return ExecutionResult{}, err
	}
	planVersion := req.Authority.PlanVersion
	if planVersion == "" {
		planVersion = req.Checkpoint.PlanVersion
	}
	if planVersion == "" {
		planVersion = req.Checkpoint.PlanDigest
	}
	authorized := AuthorizedRequest{RunID: ref.RunID, ParentRunID: ref.ParentRunID, CorrelationID: ref.SessionID, PlanVersion: planVersion, PlanDigest: req.Authority.PlanDigest, CapabilityDigest: req.Authority.CapabilityDigest, ProjectionDigest: req.Authority.ProjectionDigest, WriteScopeDigest: req.Authority.WriteScopeDigest, EffectNamespace: req.Authority.EffectNamespace, OccurrenceKey: req.Authority.OccurrenceKey}
	options := append([]runner.Option(nil), c.options...)
	options = append(options, runner.WithNativeEventHistory(history))
	options, err = c.continuationOptions(authorized, options)
	if err != nil {
		return ExecutionResult{}, err
	}
	engine := runner.New(c.model, options...)
	decision := bmtypes.DynamicActionDecision{Decision: bmtypes.DynamicActionDecisionConfirm, Token: req.DecisionToken, RunID: ref.RunID, SessionID: ref.SessionID, CheckpointID: ref.CheckpointID, CheckpointVersion: ref.SchemaVersion, CheckpointDigest: ref.Digest, IdempotencyKey: req.IdempotencyKey}
	host := bmtypes.HostResumeRequest{NativeCheckpoint: &ref, Decision: decision}
	if req.ProposedBudget != nil {
		budget := nativeBudgetToBaymax(*req.ProposedBudget)
		host.NativeBudget = &budget
	}
	result, runErr := engine.ResumeFromCheckpoint(ctx, host, c.eventHandlerFactory(), false)
	return c.projectContinuationResult(ctx, authorized, history, result, runErr)
}

func (c *Client) ResumeNative(ctx context.Context, req runtimeboundary.NativeResumeRequest) (runtimeboundary.NativeResumeOutcome, error) {
	out, err := c.ResumeAuthorized(ctx, req)
	result := runtimeboundary.NativeResumeOutcome{ExecutionID: req.Checkpoint.RunID, NativeRunID: req.Checkpoint.RunID, Status: out.Terminal.Status, Result: out.Terminal.Result, FailureClass: out.Terminal.FailureClass}
	if len(out.Events) > 0 {
		result.TerminalEvent = out.Events[len(out.Events)-1].NativeEventID
	}
	result.Checkpoint = out.NativeCheckpoint
	result.PendingActionToken = out.PendingActionToken
	return result, err
}

func validateResumeAuthority(req NativeResumeRequest) error {
	c := req.Checkpoint
	if c.Version == "" || c.CheckpointID == "" || c.Digest == "" || c.SourceComponent != "runner" || c.OwnerNamespace == "" || c.PauseReason == "" {
		return ErrAuthorizedRequest
	}
	if req.Authority.PlanDigest == "" || req.Authority.CapabilityDigest == "" || req.Authority.ProjectionDigest == "" || req.Authority.WriteScopeDigest == "" || req.Authority.EffectNamespace == "" {
		return ErrAuthorizedRequest
	}
	if c.PlanDigest != "" && c.PlanDigest != req.Authority.PlanDigest || c.AuthorityDigest != "" && c.AuthorityDigest != req.Authority.CapabilityDigest || c.WriteScopeDigest != "" && c.WriteScopeDigest != req.Authority.WriteScopeDigest || c.EffectNamespace != "" && c.EffectNamespace != req.Authority.EffectNamespace {
		return fmt.Errorf("%w: continuation authority mismatch", ErrAuthorizedRequest)
	}
	return nil
}

func (c *Client) projectContinuationResult(ctx context.Context, req AuthorizedRequest, history bmtypes.NativeEventHistoryReader, result bmtypes.RunResult, runErr error) (ExecutionResult, error) {
	terminal := Terminal{Status: "succeeded", Result: result.FinalAnswer}
	if result.TerminalOutcome != nil {
		terminal.Status = normalizeTerminalState(string(result.TerminalOutcome.State))
		terminal.FailureClass = string(result.TerminalOutcome.FailureFamily)
		if result.TerminalOutcome.PendingAction != nil {
			terminal.Status = "paused"
		}
	}
	if runErr != nil || result.Error != nil {
		terminal.Status = "failed"
		if runErr != nil {
			terminal.FailureClass = runErr.Error()
		} else {
			terminal.FailureClass = string(result.Error.Class)
		}
	}
	projected, cursor, durable, projectionErr := projectNativeHistory(ctx, history, reqSource(req), req, req.ParentRunID, terminal)
	if projectionErr != nil {
		return ExecutionResult{}, projectionErr
	}
	out := ExecutionResult{ParentRunID: req.ParentRunID, NativeRunID: req.RunID, PlanVersion: req.PlanVersion, PlanDigest: req.PlanDigest, CapabilityDigest: req.CapabilityDigest, ProjectionDigest: req.ProjectionDigest, NativeCursor: cursor, NativeHistoryDurable: durable, Events: projected, Terminal: terminal}
	if result.NativeCheckpoint != nil {
		var budget NativeBudget
		if loaded, err := c.continuation.store.Load(ctx, *result.NativeCheckpoint); err == nil {
			budget = nativeBudgetFromBaymax(loaded.Budget)
		}
		token := ""
		if result.TerminalOutcome != nil && result.TerminalOutcome.PendingAction != nil {
			token = result.TerminalOutcome.PendingAction.Token
		}
		ref := checkpointReferenceFromNative(*result.NativeCheckpoint, bmtypes.DurableBudget{}, token)
		out.NativeCheckpoint = &ref
		out.NativeCheckpoint.Budget = budget
		out.PendingActionToken = token
	}
	return out, runErr
}

func reqSource(req AuthorizedRequest) bmtypes.SourceRef {
	return bmtypes.SourceRef{Kind: bmtypes.HistorySourceRunner, Namespace: "danny.baymax.runner.history.v1", ID: req.RunID}
}

func checkpointReferenceFromNative(ref bmtypes.NativeCheckpointRef, budget bmtypes.DurableBudget, token string) CheckpointReference {
	return CheckpointReference{Version: ref.SchemaVersion, CheckpointID: ref.CheckpointID, Digest: ref.Digest, SourceComponent: ref.SourceComponent, OwnerNamespace: ref.OwnerNamespace, RunID: ref.RunID, SessionID: ref.SessionID, ParentRunID: ref.ParentRunID, PauseReason: string(ref.PauseReason), SourcePosition: ref.SourcePosition, AuthorityDigest: ref.AuthorityDigest, PlanDigest: ref.PlanDigest, OccurrenceKey: ref.OccurrenceID, WriteScopeDigest: ref.WriteScopeDigest, EffectNamespace: ref.EffectNamespace, CreatedAt: ref.CreatedAt, ExpiresAt: ref.ExpiresAt, PendingActionToken: token, Budget: nativeBudgetFromBaymax(budget)}
}

func nativeReferenceFromCheckpoint(ref CheckpointReference) bmtypes.NativeCheckpointRef {
	return bmtypes.NativeCheckpointRef{SchemaVersion: ref.Version, CheckpointID: ref.CheckpointID, Digest: ref.Digest, SourceComponent: ref.SourceComponent, OwnerNamespace: ref.OwnerNamespace, RunID: ref.RunID, SessionID: ref.SessionID, ParentRunID: ref.ParentRunID, PauseReason: bmtypes.PauseReason(ref.PauseReason), SourcePosition: ref.SourcePosition, AuthorityDigest: ref.AuthorityDigest, PlanDigest: ref.PlanDigest, OccurrenceID: ref.OccurrenceKey, WriteScopeDigest: ref.WriteScopeDigest, EffectNamespace: ref.EffectNamespace, CreatedAt: ref.CreatedAt, ExpiresAt: ref.ExpiresAt}
}

func nativeBudgetFromBaymax(b bmtypes.DurableBudget) NativeBudget {
	return NativeBudget{Revision: b.Revision, Limit: b.Limit, Consumed: b.Consumed, ToolLimit: b.ToolLimit, ToolConsumed: b.ToolConsumed, Deadline: b.Deadline}
}

func nativeBudgetToBaymax(b NativeBudget) bmtypes.DurableBudget {
	return bmtypes.DurableBudget{Revision: b.Revision, Limit: b.Limit, Consumed: b.Consumed, ToolLimit: b.ToolLimit, ToolConsumed: b.ToolConsumed, Deadline: b.Deadline}
}
