package baymax

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/FelixSeptem/baymax/context/assembler"
	"github.com/FelixSeptem/baymax/context/hostprojection"
	"github.com/FelixSeptem/baymax/core/runner"
	bmtypes "github.com/FelixSeptem/baymax/core/types"
	"github.com/FelixSeptem/baymax/orchestration/composer"
	"github.com/FelixSeptem/baymax/orchestration/mailbox"
	"github.com/FelixSeptem/baymax/orchestration/scheduler"
	"github.com/FelixSeptem/baymax/orchestration/teams"
	runtimeconfig "github.com/FelixSeptem/baymax/runtime/config"
	"github.com/FelixSeptem/baymax/tool/local"
	"github.com/FelixSeptem/danny/internal/agent/knowledge"
	"github.com/FelixSeptem/danny/internal/context/assembly"
	"github.com/FelixSeptem/danny/internal/runtimeboundary"
	"github.com/FelixSeptem/danny/internal/runtimeprobe"
)

const PinnedVersion = "v0.0.0-20261103091945-d2e08cd6dc8c"

var ErrCapabilityNotWired = errors.New("baymax capability is not wired into the runtime probe adapter")
var ErrAuthorizedRequest = errors.New("baymax: invalid authorized request")

type ControlAction string

const (
	ControlCancel    ControlAction = "cancel"
	ControlAccepted  ControlStatus = "accepted"
	ControlDuplicate ControlStatus = "duplicate"
)

type ControlStatus string

type AuthorizedControlRequest struct {
	RunID, SessionID, IdempotencyKey string
	Action                           ControlAction
}

type Adapter interface{ runtimeprobe.BaymaxAdapter }

type Inventory struct {
	Version      string
	Packages     []string
	Capabilities []string
}

// KnowledgeExecutor adapts the product KnowledgeAgent contract to Baymax's
// public runner and composer boundaries without reimplementing their loops.
type KnowledgeExecutor struct{ Client *Client }

func NewKnowledgeExecutor(client *Client) KnowledgeExecutor { return KnowledgeExecutor{Client: client} }

func (e KnowledgeExecutor) Run(ctx context.Context, req knowledge.ExecutionRequest) (knowledge.ExecutionOutcome, error) {
	if e.Client == nil {
		return knowledge.ExecutionOutcome{}, ErrAuthorizedRequest
	}
	out, err := e.Client.RunAuthorized(ctx, AuthorizedRequest{RunID: req.RunID, ParentRunID: req.ParentRunID, CorrelationID: req.CorrelationID, PlanVersion: req.PlanVersion, PlanDigest: req.PlanDigest, CapabilityDigest: req.CapabilityDigest, ProjectionDigest: req.ProjectionDigest, Role: req.Role, PerChildBudget: req.PerChildBudget, AggregationLimit: req.AggregationLimit, Input: req.Input, ToolPolicy: ToolPolicy{AllowedTools: append([]string(nil), req.Policy.Tools...), WriteScope: string(req.Policy.WriteScope), ApprovalGranted: !req.Policy.ConfirmationRequired}})
	if err != nil {
		return knowledge.ExecutionOutcome{}, err
	}
	return knowledge.ExecutionOutcome{Status: out.Terminal.Status, Result: out.Terminal.Result, FailureClass: out.Terminal.FailureClass, NativeRunID: req.RunID, CausationID: out.CausationID}, nil
}

func (e KnowledgeExecutor) RunChild(ctx context.Context, req knowledge.ExecutionRequest) (knowledge.ExecutionOutcome, error) {
	if e.Client == nil || req.RunID == "" || req.ParentRunID == "" || req.CapabilityDigest == "" {
		return knowledge.ExecutionOutcome{}, ErrAuthorizedRequest
	}
	out, err := e.Client.RunChildAuthorized(ctx, AuthorizedRequest{RunID: req.RunID, ParentRunID: req.ParentRunID, CorrelationID: req.CorrelationID, PlanVersion: req.PlanVersion, PlanDigest: req.PlanDigest, CapabilityDigest: req.CapabilityDigest, ProjectionDigest: req.ProjectionDigest, Role: req.Role, PerChildBudget: req.PerChildBudget, AggregationLimit: req.AggregationLimit, Input: req.Input, ToolPolicy: ToolPolicy{AllowedTools: append([]string(nil), req.Policy.Tools...), WriteScope: string(req.Policy.WriteScope), ApprovalGranted: !req.Policy.ConfirmationRequired}})
	if err != nil {
		return knowledge.ExecutionOutcome{}, err
	}
	return knowledge.ExecutionOutcome{Status: out.Terminal.Status, Result: out.Terminal.Result, FailureClass: out.Terminal.FailureClass, NativeRunID: req.RunID, CausationID: out.CausationID}, nil
}

// RunChildren keeps fanout assignment creation and bounded native dispatch at
// the Baymax boundary. The product harness receives terminal outcomes only;
// it does not decide how child admissions are scheduled.
func (e KnowledgeExecutor) RunChildren(ctx context.Context, req knowledge.ExecutionRequest, count int) ([]knowledge.ExecutionOutcome, error) {
	if e.Client == nil || req.RunID == "" || req.CapabilityDigest == "" || count <= 0 || count > 8 {
		return nil, ErrAuthorizedRequest
	}
	results, err := e.Client.RunChildrenAuthorized(ctx, AuthorizedRequest{
		RunID: req.RunID, ParentRunID: req.ParentRunID, CorrelationID: req.CorrelationID,
		PlanVersion: req.PlanVersion, PlanDigest: req.PlanDigest, CapabilityDigest: req.CapabilityDigest,
		ProjectionDigest: req.ProjectionDigest, Role: req.Role, PerChildBudget: req.PerChildBudget,
		AggregationLimit: req.AggregationLimit, Input: req.Input,
		ToolPolicy: ToolPolicy{AllowedTools: append([]string(nil), req.Policy.Tools...), WriteScope: string(req.Policy.WriteScope), ApprovalGranted: !req.Policy.ConfirmationRequired},
	}, count)
	out := make([]knowledge.ExecutionOutcome, 0, len(results))
	for _, result := range results {
		out = append(out, knowledge.ExecutionOutcome{Status: result.Terminal.Status, Result: result.Terminal.Result, FailureClass: result.Terminal.FailureClass, NativeRunID: result.NativeRunID, CausationID: result.CausationID})
	}
	return out, err
}

func NewInventory() Inventory {
	return Inventory{
		Version: PinnedVersion,
		Packages: []string{
			"github.com/FelixSeptem/baymax/core/runner",
			"github.com/FelixSeptem/baymax/core/types",
			"github.com/FelixSeptem/baymax/model/openai",
			"github.com/FelixSeptem/baymax/orchestration/composer",
			"github.com/FelixSeptem/baymax/orchestration/mailbox",
			"github.com/FelixSeptem/baymax/orchestration/scheduler",
			"github.com/FelixSeptem/baymax/orchestration/teams",
			"github.com/FelixSeptem/baymax/context/assembler",
		},
		Capabilities: []string{"run", "stream", "tools", "subagent", "team", "mailbox", "scheduler", "context-assembler"},
	}
}

// Client is the narrow boundary around Baymax's public runner. It accepts a
// ModelClient so deterministic probes can use Baymax's own fake model.
type Client struct {
	engine    *runner.Engine
	model     bmtypes.ModelClient
	assembler *assembler.Assembler
	registry  *local.Registry
	options   []runner.Option
	// eventHandlerFactory supplies the native runner observer used by
	// authorized host activation. Source history remains the projection source.
	eventHandlerFactory func() bmtypes.EventHandler
	activeMu            sync.Mutex
	activeEngines       map[string]*runner.Engine
	nativeHistories     map[string]bmtypes.NativeEventHistoryReader
	continuation        *continuationRuntime
}

func New(model bmtypes.ModelClient, opts ...runner.Option) (*Client, error) {
	if model == nil {
		return nil, fmt.Errorf("baymax model client is required")
	}
	registry := local.NewRegistry()
	if _, err := registry.Register(probeTool{}); err != nil {
		return nil, err
	}
	opts = append(opts, runner.WithLocalRegistry(registry))
	cfg := runtimeconfig.DefaultConfig().ContextAssembler
	return &Client{engine: runner.New(model, opts...), model: model, assembler: assembler.New(func() runtimeconfig.ContextAssemblerConfig { return cfg }), registry: registry, options: append([]runner.Option(nil), opts...), eventHandlerFactory: func() bmtypes.EventHandler { return nativeEventObserver{} }, activeEngines: make(map[string]*runner.Engine), nativeHistories: make(map[string]bmtypes.NativeEventHistoryReader)}, nil
}

type AuthorizedRequest struct {
	RunID, ParentRunID, CorrelationID                           string
	PlanVersion, PlanDigest, CapabilityDigest, ProjectionDigest string
	WriteScopeDigest, EffectNamespace                           string
	OccurrenceKey                                               string
	Role                                                        string
	PerChildBudget, AggregationLimit                            int
	Prefix, Input                                               string
	HostContextProjection                                       *hostprojection.Descriptor
	HostContextResolver                                         hostprojection.Resolver
	ToolPolicy                                                  ToolPolicy
	LoopPolicy                                                  *bmtypes.LoopPolicy
}

// ToolPolicy is product-owned authority supplied by the server, never derived
// from model output or admitted context.
type ToolPolicy struct {
	AllowedTools    []string
	WriteScope      string
	TaskRevision    int64
	EffectNamespace string
	ApprovalGranted bool
	Authorize       func(context.Context, string, map[string]any, ToolPolicy) error
}

type TeamMember struct {
	ID, Role, Input string
}

type TeamRequest struct {
	RunID, TeamID, CorrelationID                                string
	PlanVersion, PlanDigest, CapabilityDigest, ProjectionDigest string
	WriteScopeDigest, EffectNamespace                           string
	Input                                                       string
	Strategy                                                    teams.Strategy
	VoteTieBreak                                                teams.VoteTieBreak
	TaskTimeout                                                 time.Duration
	ParallelMaxWorkers                                          int
	Backpressure                                                bmtypes.BackpressureMode
	FailurePolicy                                               teams.FailurePolicy
	Members                                                     []TeamMember
	ToolPolicy                                                  ToolPolicy
}

type TeamMemberResult struct {
	MemberID, RunID, ParentRunID, Status, Result, FailureClass string
	NativeCursor                                               string
	NativeHistoryDurable                                       bool
	Events                                                     []ExecutionEvent
}

type TeamExecutionResult struct {
	RunID, TeamID, NativeCursor, Status, FailureClass string
	Strategy, WinnerVote                              string
	NativeHistoryDurable                              bool
	Events                                            []ExecutionEvent
	Members                                           []TeamMemberResult
}

type EventKind string

const (
	EventParent     EventKind = "parent"
	EventChild      EventKind = "child"
	EventTool       EventKind = "tool"
	EventDiagnostic EventKind = "diagnostic"
	EventTerminal   EventKind = "terminal"
)

type ExecutionEvent struct {
	Sequence                                        int
	ID, ParentID, CorrelationID                     string
	Kind                                            EventKind
	PlanVersion, CapabilityDigest, ProjectionDigest string
	Status, FailureClass, Result                    string
	NativeSource                                    NativeSource `json:"native_source"`
	NativeEventID                                   string       `json:"native_event_id"`
	NativeSequence                                  uint64       `json:"native_sequence"`
	NativeType                                      string       `json:"native_type"`
}

type NativeSource struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	ID        string `json:"id"`
}

type Terminal struct{ Status, FailureClass, Result string }

type ExecutionResult struct {
	ParentRunID, PlanVersion, PlanDigest, CapabilityDigest, ProjectionDigest string
	NativeRunID                                                              string `json:"native_run_id"`
	NativeCursor                                                             string `json:"native_cursor"`
	NativeHistoryDurable                                                     bool   `json:"native_history_durable"`
	CausationID                                                              string `json:"causation_id,omitempty"`
	Events                                                                   []ExecutionEvent
	Terminal                                                                 Terminal
	NativeCheckpoint                                                         *CheckpointReference
	PendingActionToken                                                       string
}

func (c *Client) RunAuthorized(ctx context.Context, req AuthorizedRequest) (ExecutionResult, error) {
	return c.executeAuthorized(ctx, req, false)
}

func (c *Client) StreamAuthorized(ctx context.Context, req AuthorizedRequest) (ExecutionResult, error) {
	return c.executeAuthorized(ctx, req, true)
}

func (c *Client) ControlAuthorized(_ context.Context, req AuthorizedControlRequest) (ControlStatus, error) {
	if c == nil || req.RunID == "" || req.SessionID == "" || req.IdempotencyKey == "" || req.Action != ControlCancel {
		return "", ErrAuthorizedRequest
	}
	c.activeMu.Lock()
	engine := c.activeEngines[req.RunID]
	c.activeMu.Unlock()
	if engine == nil {
		return "", runner.ErrActiveRunUnknown
	}
	status, err := engine.CancelRun(req.RunID, req.SessionID)
	if err != nil {
		return "", err
	}
	if status == bmtypes.HostAdmissionStatusDuplicate {
		return ControlDuplicate, nil
	}
	return ControlAccepted, nil
}

// Control translates the product-neutral control request to Baymax's native
// control surface. Baymax currently exposes cancellation as the source
// control for both product cancellation and timeout; the action remains in
// the Danny evidence so those product intents are not conflated.
func (c *Client) Control(ctx context.Context, req runtimeboundary.ControlRequest) (runtimeboundary.ControlOutcome, error) {
	if err := req.Validate(); err != nil || req.NativeRunID == "" {
		return runtimeboundary.ControlOutcome{}, ErrAuthorizedRequest
	}
	if req.Action != string(ControlCancel) && req.Action != "timeout" {
		return runtimeboundary.ControlOutcome{}, fmt.Errorf("%w: unsupported control action %q", ErrAuthorizedRequest, req.Action)
	}
	sessionID := req.SessionID
	if sessionID == "" {
		sessionID = req.NativeRunID
	}
	status, err := c.ControlAuthorized(ctx, AuthorizedControlRequest{RunID: req.NativeRunID, SessionID: sessionID, IdempotencyKey: req.IdempotencyKey, Action: ControlCancel})
	if err != nil {
		return runtimeboundary.ControlOutcome{}, err
	}
	return runtimeboundary.ControlOutcome{Status: string(status), TerminalObserved: false}, nil
}

// Reconcile reads retained native history only. It never starts, resumes, or
// retries a run, so an uncertain dispatch cannot duplicate model/tool effects.
func (c *Client) Reconcile(ctx context.Context, req runtimeboundary.RecoveryRequest) (runtimeboundary.RecoveryOutcome, error) {
	if c == nil {
		return runtimeboundary.RecoveryOutcome{}, ErrAuthorizedRequest
	}
	if err := req.Validate(); err != nil {
		return runtimeboundary.RecoveryOutcome{}, ErrAuthorizedRequest
	}
	source := bmtypes.SourceRef{Kind: bmtypes.HistorySourceRunner, Namespace: req.SourceNamespace, ID: req.NativeRunID}
	c.activeMu.Lock()
	history := c.nativeHistories[req.NativeRunID]
	c.activeMu.Unlock()
	if history == nil {
		return runtimeboundary.RecoveryOutcome{Status: runtimeboundary.RecoveryMissing}, nil
	}
	caps, err := history.Capabilities(ctx, source)
	if err != nil {
		return runtimeboundary.RecoveryOutcome{Status: runtimeboundary.RecoveryGap}, nil
	}
	if !caps.Supported || !caps.SequenceDefined || caps.CursorVersion == "" || caps.TerminalRecovery == "" || caps.TerminalRecovery == "none" {
		return runtimeboundary.RecoveryOutcome{Status: runtimeboundary.RecoveryGap}, nil
	}
	page, err := history.ReadAfter(ctx, source, nil, bmtypes.MaxHistoryPageSize)
	if err != nil {
		return runtimeboundary.RecoveryOutcome{Status: runtimeboundary.RecoveryGap}, nil
	}
	if page.NextCursor == nil || page.NextCursor.Value == "" {
		return runtimeboundary.RecoveryOutcome{Status: runtimeboundary.RecoveryGap}, nil
	}
	var terminal *bmtypes.NativeExecutionEvent
	for index := range page.Events {
		event := page.Events[index]
		if event.IsTerminal() {
			copy := event
			terminal = &copy
		}
	}
	if terminal == nil || terminal.History == nil {
		return runtimeboundary.RecoveryOutcome{Status: runtimeboundary.RecoveryMissing, SourceCursor: page.NextCursor.Value}, nil
	}
	status := terminal.SourceState()
	if status == "" {
		return runtimeboundary.RecoveryOutcome{Status: runtimeboundary.RecoveryGap}, nil
	}
	sequence := int64(terminal.History.Sequence)
	return runtimeboundary.RecoveryOutcome{Status: runtimeboundary.RecoveryTerminal, NativeRunID: terminal.History.Source.ID, NativeEventID: terminal.History.EventID, NativeVersion: bmtypes.NativeExecutionEventVersionV1, NativeSequence: &sequence, SourceSequence: sequence, SourceCursor: page.NextCursor.Value, ObservationScope: req.ExecutionID, TerminalStatus: normalizeTerminalState(status)}, nil
}

func (c *Client) rememberNativeHistory(runID string, history bmtypes.NativeEventHistoryReader) {
	if c == nil || runID == "" || history == nil {
		return
	}
	c.activeMu.Lock()
	if c.nativeHistories == nil {
		c.nativeHistories = make(map[string]bmtypes.NativeEventHistoryReader)
	}
	c.nativeHistories[runID] = history
	c.activeMu.Unlock()
}

func (c *Client) RetryAuthorized(ctx context.Context, previous bmtypes.RunRef, req AuthorizedRequest) (ExecutionResult, error) {
	if c == nil || req.RunID == "" || req.CorrelationID == "" || req.PlanVersion == "" || req.CapabilityDigest == "" {
		return ExecutionResult{}, ErrAuthorizedRequest
	}
	source := bmtypes.SourceRef{Kind: bmtypes.HistorySourceRunner, Namespace: "danny.baymax.runner.history.v1", ID: req.RunID}
	history, err := runner.NewNativeEventHistory(source)
	if err != nil {
		return ExecutionResult{}, err
	}
	c.rememberNativeHistory(req.RunID, history)
	engine := runner.New(c.model, append(append([]runner.Option(nil), c.options...), runner.WithNativeEventHistory(history), runner.WithToolMiddlewares(toolPolicyMiddleware(req.ToolPolicy)))...)
	request := bmtypes.RunRequest{RunID: req.RunID, SessionID: req.CorrelationID, Input: req.Input}
	result, runErr := engine.RetryRun(ctx, previous, request, c.eventHandlerFactory(), false)
	terminal := Terminal{Status: "succeeded", Result: result.FinalAnswer}
	if result.TerminalOutcome != nil {
		terminal.Status = normalizeTerminalState(string(result.TerminalOutcome.State))
		terminal.FailureClass = string(result.TerminalOutcome.FailureFamily)
	}
	if runErr != nil || result.Error != nil {
		terminal.Status = "failed"
		if runErr != nil {
			terminal.FailureClass = runErr.Error()
		} else {
			terminal.FailureClass = string(result.Error.Class)
		}
	}
	projected, cursor, durable, projectionErr := projectNativeHistory(ctx, history, source, req, req.ParentRunID, terminal)
	if projectionErr != nil {
		return ExecutionResult{}, projectionErr
	}
	causationID := previous.RunID
	if result.TerminalOutcome != nil && result.TerminalOutcome.CausationID != "" {
		causationID = result.TerminalOutcome.CausationID
	}
	out := ExecutionResult{ParentRunID: req.ParentRunID, NativeRunID: req.RunID, PlanVersion: req.PlanVersion, PlanDigest: req.PlanDigest, CapabilityDigest: req.CapabilityDigest, ProjectionDigest: req.ProjectionDigest, NativeCursor: cursor, NativeHistoryDurable: durable, CausationID: causationID, Events: projected, Terminal: terminal}
	return out, runErr
}

// Retry translates a product retry into Baymax's causal RetryRun operation.
// The requested native run ID is the new source identity; the old run is
// retained solely as the causation parent.
func (c *Client) Retry(ctx context.Context, req runtimeboundary.RetryRequest) (runtimeboundary.RetryOutcome, error) {
	if err := req.Validate(); err != nil {
		return runtimeboundary.RetryOutcome{}, ErrAuthorizedRequest
	}
	sessionID := req.SessionID
	if sessionID == "" {
		sessionID = req.ExecutionID
	}
	previous := bmtypes.RunRef{RunID: req.CausationID, SessionID: sessionID, State: bmtypes.RunStateFailed, Source: bmtypes.ProtocolSourceRunner}
	authorized := AuthorizedRequest{RunID: req.NativeRunID, CorrelationID: sessionID, PlanVersion: req.Authority.PlanVersion, PlanDigest: req.Authority.PlanDigest, CapabilityDigest: req.Authority.CapabilityDigest, ProjectionDigest: req.Authority.ProjectionDigest, Input: req.Input, EffectNamespace: req.Authority.EffectNamespace, WriteScopeDigest: req.Authority.WriteScope}
	result, err := c.RetryAuthorized(ctx, previous, authorized)
	status := result.Terminal.Status
	if status == "" {
		status = "failed"
	}
	causationID := result.CausationID
	if causationID == "" {
		causationID = req.CausationID
	}
	out := runtimeboundary.RetryOutcome{ExecutionID: req.ExecutionID, NativeRunID: req.NativeRunID, CausationID: causationID, Status: status, Result: result.Terminal.Result, FailureClass: result.Terminal.FailureClass}
	return out, err
}

// RegisterTool exposes the narrow Baymax local tool registry to product-owned
// adapters. Every authorized call is still checked by toolPolicyMiddleware.
func (c *Client) RegisterTool(tool bmtypes.Tool) error {
	if c == nil || c.registry == nil || tool == nil {
		return ErrAuthorizedRequest
	}
	_, err := c.registry.Register(tool)
	return err
}

// RunChildAuthorized dispatches one product-authorized assignment through
// Baymax's native composer scheduler. The scheduler owns child admission,
// bounded execution and terminal commit; the local runner only activates the
// already-authorized host execution.
func (c *Client) RunChildAuthorized(ctx context.Context, req AuthorizedRequest) (ExecutionResult, error) {
	if c == nil || req.RunID == "" || req.ParentRunID == "" || req.CorrelationID == "" || req.PlanVersion == "" || req.CapabilityDigest == "" {
		return ExecutionResult{}, ErrAuthorizedRequest
	}
	comp, err := composer.NewBuilder(c.model).WithLocalRegistry(c.registry).Build()
	if err != nil {
		return ExecutionResult{}, err
	}
	var result ExecutionResult
	var runErr error
	childBudget := time.Second
	if req.PerChildBudget > 0 {
		childBudget = time.Duration(req.PerChildBudget) * time.Millisecond
	}
	dispatch, err := comp.DispatchChild(ctx, composer.ChildDispatchRequest{
		Task:                  scheduler.Task{TaskID: req.RunID, RunID: req.RunID, ParentRunID: req.ParentRunID, AgentID: req.RunID, Payload: map[string]any{"correlation_id": req.CorrelationID, "plan_version": req.PlanVersion, "capability_digest": req.CapabilityDigest, "projection_digest": req.ProjectionDigest, "role": req.Role, "aggregation_limit": req.AggregationLimit}},
		Target:                composer.ChildTargetLocal,
		WorkerID:              "danny-authorized-child",
		ParentDepth:           0,
		ParentActiveChildren:  0,
		ParentRemainingBudget: childBudget,
		ChildTimeout:          childBudget,
		AuthorityDigest:       req.CapabilityDigest,
		PlanDigest:            req.PlanVersion,
		WriteScopeDigest:      req.ProjectionDigest,
		EffectNamespace:       req.RunID,
		LocalRunner: composer.LocalChildRunnerFunc(func(childCtx context.Context, task scheduler.Task) (map[string]any, error) {
			if task.TaskID != req.RunID || task.ParentRunID != req.ParentRunID {
				return nil, fmt.Errorf("native child assignment identity mismatch")
			}
			boundedCtx, cancel := context.WithTimeout(childCtx, childBudget)
			defer cancel()
			result, runErr = c.RunAuthorized(boundedCtx, req)
			if runErr != nil {
				return nil, runErr
			}
			return map[string]any{"status": result.Terminal.Status, "result": result.Terminal.Result, "native_cursor": result.NativeCursor}, nil
		}),
	})
	if err != nil {
		return result, err
	}
	if runErr != nil {
		return result, runErr
	}
	if dispatch.Commit.Status != scheduler.TaskStateSucceeded {
		return result, fmt.Errorf("native child terminal status %q", dispatch.Commit.Status)
	}
	return result, nil
}

// RunChildrenAuthorized admits the planned child assignments through the
// native composer boundary. Assignment IDs are deterministic and derived from
// the parent execution; a cancellation or dispatch failure stops admission of
// later assignments and returns all terminal evidence already observed.
func (c *Client) RunChildrenAuthorized(ctx context.Context, req AuthorizedRequest, count int) ([]ExecutionResult, error) {
	if c == nil || req.RunID == "" || req.CorrelationID == "" || req.CapabilityDigest == "" || count <= 0 || count > 8 {
		return nil, ErrAuthorizedRequest
	}
	results := make([]ExecutionResult, 0, count)
	for index := 0; index < count; index++ {
		if err := ctx.Err(); err != nil {
			return results, err
		}
		child := req
		child.RunID = fmt.Sprintf("%s:child:%d", req.RunID, index+1)
		child.ParentRunID = req.RunID
		child.CorrelationID = fmt.Sprintf("%s:child:%d", req.CorrelationID, index+1)
		child.Role = "subagent"
		result, err := c.RunChildAuthorized(ctx, child)
		results = append(results, result)
		if err != nil {
			return results, err
		}
		if strings.ToLower(result.Terminal.Status) != "succeeded" {
			return results, fmt.Errorf("native child %s terminal status %q", child.RunID, result.Terminal.Status)
		}
	}
	return results, nil
}

// RunTeamAuthorized delegates scheduling and aggregation to Baymax's native
// teams engine. Member executions retain their own runner evidence while the
// team journal records native aggregate lifecycle facts.
func (c *Client) RunTeamAuthorized(ctx context.Context, req TeamRequest) (TeamExecutionResult, error) {
	if c == nil || req.RunID == "" || req.TeamID == "" || req.CorrelationID == "" || req.PlanVersion == "" || req.PlanDigest == "" || req.CapabilityDigest == "" || req.ProjectionDigest == "" || req.WriteScopeDigest == "" || req.EffectNamespace == "" || len(req.Members) == 0 || len(req.Members) > 8 {
		return TeamExecutionResult{}, ErrAuthorizedRequest
	}
	source := bmtypes.SourceRef{Kind: bmtypes.HistorySourceTeam, Namespace: "danny.baymax.team.history.v1", ID: req.TeamID}
	history, err := teams.NewNativeEventHistory(source)
	if err != nil {
		return TeamExecutionResult{}, err
	}
	strategy := req.Strategy
	if strategy == "" {
		strategy = teams.StrategyParallel
	}
	if strategy != teams.StrategyParallel && strategy != teams.StrategyVote {
		return TeamExecutionResult{}, fmt.Errorf("unsupported authorized team strategy %q", strategy)
	}
	tasks := make([]teams.Task, 0, len(req.Members))
	seen := make(map[string]struct{}, len(req.Members))
	for _, member := range req.Members {
		memberID := strings.TrimSpace(member.ID)
		if memberID == "" || member.Role == "" {
			return TeamExecutionResult{}, ErrAuthorizedRequest
		}
		if _, ok := seen[memberID]; ok {
			return TeamExecutionResult{}, fmt.Errorf("duplicate team member %q", memberID)
		}
		seen[memberID] = struct{}{}
		member := member
		tasks = append(tasks, teams.Task{TaskID: req.TeamID + ":" + memberID, AgentID: memberID, Role: teams.Role(member.Role), Target: teams.TaskTargetLocal, Runner: teams.TaskRunnerFunc(func(memberCtx context.Context, task teams.Task) (teams.TaskResult, error) {
			memberRunID := req.TeamID + ":" + member.ID
			out, runErr := c.RunAuthorized(memberCtx, AuthorizedRequest{RunID: memberRunID, ParentRunID: req.RunID, CorrelationID: req.CorrelationID + ":" + member.ID, PlanVersion: req.PlanVersion, PlanDigest: req.PlanDigest, CapabilityDigest: req.CapabilityDigest, ProjectionDigest: req.ProjectionDigest, WriteScopeDigest: req.WriteScopeDigest, EffectNamespace: req.EffectNamespace + ":" + member.ID, Role: member.Role, Input: member.Input, ToolPolicy: req.ToolPolicy})
			if runErr != nil {
				return teams.TaskResult{Output: TeamMemberResult{MemberID: member.ID, RunID: memberRunID, ParentRunID: req.RunID, Status: "failed", FailureClass: runErr.Error(), NativeCursor: out.NativeCursor, NativeHistoryDurable: out.NativeHistoryDurable, Events: out.Events}}, runErr
			}
			if out.Terminal.Status != "succeeded" {
				return teams.TaskResult{Output: TeamMemberResult{MemberID: member.ID, RunID: memberRunID, ParentRunID: req.RunID, Status: out.Terminal.Status, Result: out.Terminal.Result, FailureClass: out.Terminal.FailureClass, NativeCursor: out.NativeCursor, NativeHistoryDurable: out.NativeHistoryDurable, Events: out.Events}}, fmt.Errorf("team member %s terminal status %s", member.ID, out.Terminal.Status)
			}
			memberResult := TeamMemberResult{MemberID: member.ID, RunID: memberRunID, ParentRunID: req.RunID, Status: out.Terminal.Status, Result: out.Terminal.Result, FailureClass: out.Terminal.FailureClass, NativeCursor: out.NativeCursor, NativeHistoryDurable: out.NativeHistoryDurable, Events: out.Events}
			if strategy == teams.StrategyVote {
				vote, ok := normalizeMemberVote(out.Terminal.Result)
				if !ok {
					return teams.TaskResult{Output: memberResult}, fmt.Errorf("team member %s did not provide approve/reject decision evidence", member.ID)
				}
				return teams.TaskResult{Vote: vote, Output: memberResult}, nil
			}
			return teams.TaskResult{Output: memberResult}, nil
		})})
	}
	engine := teams.New(teams.WithNativeEventHistory(history))
	failurePolicy := req.FailurePolicy
	if failurePolicy == "" {
		failurePolicy = teams.FailurePolicyFailFast
	}
	workers := req.ParallelMaxWorkers
	if workers <= 0 {
		workers = len(tasks)
	}
	aggregate, runErr := engine.Run(ctx, teams.Plan{RunID: req.RunID, TeamID: req.TeamID, Strategy: strategy, Tasks: tasks, TaskTimeout: req.TaskTimeout, ParallelMaxWorkers: workers, Backpressure: req.Backpressure, FailurePolicy: failurePolicy, VoteTieBreak: req.VoteTieBreak})
	page, pageErr := history.ReadAfter(ctx, source, nil, bmtypes.MaxHistoryPageSize)
	if pageErr != nil {
		return TeamExecutionResult{}, pageErr
	}
	out := TeamExecutionResult{RunID: req.RunID, TeamID: aggregate.TeamID, Strategy: string(strategy), WinnerVote: aggregate.WinnerVote, Status: "succeeded", Members: make([]TeamMemberResult, 0, len(aggregate.Tasks))}
	if page.NextCursor != nil {
		out.NativeCursor = page.NextCursor.Value
	}
	out.Events = projectTeamHistory(page.Events, req)
	if caps, capsErr := history.Capabilities(ctx, source); capsErr == nil {
		out.NativeHistoryDurable = caps.Durable
	}
	if runErr == nil {
		for _, task := range aggregate.Tasks {
			if task.Status == teams.TaskStatusFailed || task.Status == teams.TaskStatusCanceled {
				if task.Error != "" {
					runErr = errors.New(task.Error)
				} else {
					runErr = fmt.Errorf("team member %s ended %s", task.TaskID, task.Status)
				}
				break
			}
		}
	}
	if aggregate.TeamTaskFailed > 0 || runErr != nil {
		out.Status = "failed"
		if runErr != nil {
			out.FailureClass = runErr.Error()
		}
	}
	for _, task := range aggregate.Tasks {
		if member, ok := task.Output.(TeamMemberResult); ok {
			out.Members = append(out.Members, member)
		}
	}
	if runErr != nil {
		return out, runErr
	}
	return out, nil
}

func (c *Client) executeAuthorized(ctx context.Context, req AuthorizedRequest, stream bool) (ExecutionResult, error) {
	if c == nil || c.engine == nil || req.RunID == "" || req.CorrelationID == "" || req.CapabilityDigest == "" || req.PlanVersion == "" {
		return ExecutionResult{}, ErrAuthorizedRequest
	}
	if len(req.ToolPolicy.AllowedTools) == 0 {
		// A missing product policy is fail-closed for tools. The deterministic
		// probe is the only adapter-owned capability that may run by default.
		req.ToolPolicy.AllowedTools = []string{"probe"}
	}
	parent := req.ParentRunID
	if parent == "" {
		parent = req.RunID
	}
	if req.HostContextProjection == nil {
		projection := assembly.Projection{Role: assembly.Role(req.Role), Prefix: req.Prefix, CapabilityDigest: req.CapabilityDigest, Digest: req.ProjectionDigest, References: []assembly.Reference{{ID: "projection:" + req.ProjectionDigest, Kind: "product_reference", Locator: req.ProjectionDigest}}}
		descriptor, descriptorErr := projectionDescriptor(projection, req)
		if descriptorErr != nil {
			return ExecutionResult{}, descriptorErr
		}
		req.HostContextProjection = &descriptor
		if req.HostContextResolver == nil {
			req.HostContextResolver = referenceProjectionResolver{}
		}
	}
	assembleReq := bmtypes.ContextAssembleRequest{RunID: req.RunID, SessionID: req.CorrelationID, PrefixVersion: req.Prefix, Input: req.Input, HostContextProjection: req.HostContextProjection, HostContextResolver: req.HostContextResolver}
	modelReq := bmtypes.ModelRequest{RunID: req.RunID, Input: req.Input}
	if c.assembler != nil {
		var err error
		modelReq, _, _, err = c.assembler.AssembleWithHandoff(ctx, assembleReq, modelReq)
		if err != nil {
			return ExecutionResult{}, err
		}
	}
	request := bmtypes.RunRequest{RunID: req.RunID, SessionID: req.CorrelationID, Input: modelReq.Input, Policy: req.LoopPolicy}
	mode := bmtypes.HostRunExecutionModeRun
	if stream {
		mode = bmtypes.HostRunExecutionModeStream
	}
	source := bmtypes.SourceRef{Kind: bmtypes.HistorySourceRunner, Namespace: "danny.baymax.runner.history.v1", ID: req.RunID}
	history, err := runner.NewNativeEventHistory(source)
	if err != nil {
		return ExecutionResult{}, err
	}
	c.rememberNativeHistory(req.RunID, history)
	options := append([]runner.Option(nil), c.options...)
	options = append(options, runner.WithNativeEventHistory(history), runner.WithToolMiddlewares(toolPolicyMiddleware(req.ToolPolicy)))
	options, err = c.continuationOptions(req, options)
	if err != nil {
		return ExecutionResult{}, err
	}
	engine := runner.New(c.model, options...)
	admission, err := engine.AdmitHostRun(ctx, request, mode)
	if err != nil {
		return ExecutionResult{}, err
	}
	if admission.Status != bmtypes.HostAdmissionStatusAccepted || admission.Execution == nil {
		return ExecutionResult{}, fmt.Errorf("baymax host admission rejected: %s", admission.ReasonCode)
	}
	c.activeMu.Lock()
	c.activeEngines[req.RunID] = engine
	c.activeMu.Unlock()
	defer func() {
		c.activeMu.Lock()
		delete(c.activeEngines, req.RunID)
		c.activeMu.Unlock()
	}()
	watchDone := make(chan struct{})
	defer close(watchDone)
	go func() {
		select {
		case <-ctx.Done():
			// Host admission intentionally detaches request cancellation. Route
			// expiry through the source-owned control so its terminal history is
			// authoritative rather than abandoning an uncertain active run.
			_, _ = engine.CancelRun(req.RunID, req.CorrelationID)
		case <-watchDone:
		}
	}()
	handler := bmtypes.EventHandler(nativeEventObserver{})
	if c.eventHandlerFactory != nil {
		if candidate := c.eventHandlerFactory(); candidate != nil {
			handler = candidate
		}
	}
	result, err := admission.Execution.Execute(handler)
	if err != nil && result.Error == nil {
		admission.Execution.Release()
	}
	terminal := Terminal{Status: "succeeded", Result: result.FinalAnswer}
	if result.TerminalOutcome != nil {
		terminal.Status = normalizeTerminalState(string(result.TerminalOutcome.State))
		terminal.FailureClass = string(result.TerminalOutcome.FailureFamily)
		if result.TerminalOutcome.PendingAction != nil {
			terminal.Status = "paused"
		}
	}
	if err != nil || result.Error != nil {
		terminal.Status = "failed"
		if errors.Is(err, context.Canceled) || (result.Error != nil && result.Error.Class == bmtypes.ErrPolicyTimeout && strings.Contains(strings.ToLower(result.Error.Message), "cancel")) {
			terminal.Status = "canceled"
		}
		if err != nil {
			terminal.FailureClass = err.Error()
		} else {
			terminal.FailureClass = string(result.Error.Class)
		}
	}
	projected, cursor, durable, projectionErr := projectNativeHistory(ctx, history, source, req, parent, terminal)
	if projectionErr != nil {
		return ExecutionResult{}, projectionErr
	}
	out := ExecutionResult{ParentRunID: parent, NativeRunID: req.RunID, PlanVersion: req.PlanVersion, PlanDigest: req.PlanDigest, CapabilityDigest: req.CapabilityDigest, ProjectionDigest: req.ProjectionDigest, NativeCursor: cursor, NativeHistoryDurable: durable, Events: projected, Terminal: terminal}
	if result.NativeCheckpoint != nil {
		token := ""
		if result.TerminalOutcome != nil && result.TerminalOutcome.PendingAction != nil {
			token = result.TerminalOutcome.PendingAction.Token
		}
		var budget bmtypes.DurableBudget
		if c.continuation != nil {
			if persisted, loadErr := c.continuation.store.Load(ctx, *result.NativeCheckpoint); loadErr == nil {
				budget = persisted.Budget
			}
		}
		ref := checkpointReferenceFromNative(*result.NativeCheckpoint, budget, token)
		out.NativeCheckpoint = &ref
		out.PendingActionToken = token
	}
	return out, err
}

// nativeEventObserver deliberately has no product-side write behavior. Baymax
// owns native event recording; the adapter projects that retained source
// history after activation instead of synthesizing or dual-writing events.
type nativeEventObserver struct{}

func (nativeEventObserver) OnEvent(context.Context, bmtypes.Event) {}

func toolPolicyMiddleware(policy ToolPolicy) bmtypes.ToolMiddleware {
	enforced := len(policy.AllowedTools) > 0 || policy.WriteScope != "" || policy.TaskRevision != 0 || policy.EffectNamespace != "" || policy.ApprovalGranted || policy.Authorize != nil
	allowed := make(map[string]struct{}, len(policy.AllowedTools))
	for _, name := range policy.AllowedTools {
		if name = strings.TrimSpace(name); name != "" {
			allowed[normalizeToolPolicyName(name)] = struct{}{}
		}
	}
	return bmtypes.ToolMiddlewareFunc(func(ctx context.Context, call bmtypes.ToolCall, next bmtypes.ToolInvokeFunc) (bmtypes.ToolResult, error) {
		if !enforced {
			return next(ctx, call)
		}
		if _, ok := allowed[normalizeToolPolicyName(call.Name)]; !ok {
			return bmtypes.ToolResult{}, fmt.Errorf("tool %q is outside server-authorized capabilities", call.Name)
		}
		if isMutationTool(call.Name) {
			if policy.WriteScope == "" || policy.WriteScope == "none" || policy.TaskRevision <= 0 || policy.EffectNamespace == "" || !policy.ApprovalGranted {
				return bmtypes.ToolResult{}, fmt.Errorf("mutation tool %q lacks write, revision, effect, or approval authority", call.Name)
			}
			if policy.Authorize == nil {
				return bmtypes.ToolResult{}, fmt.Errorf("mutation tool %q has no live product authorization gate", call.Name)
			}
			if err := policy.Authorize(ctx, call.Name, call.Args, policy); err != nil {
				return bmtypes.ToolResult{}, err
			}
		}
		return next(ctx, call)
	})
}

func normalizeToolPolicyName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.TrimPrefix(name, "local.")
	return name
}

func isMutationTool(name string) bool {
	switch normalizeToolPolicyName(name) {
	case "save_memory", "create_task", "create_schedule", "send_webhook", "commit_compilation":
		return true
	default:
		return false
	}
}

func normalizeMemberVote(result string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(result)) {
	case "approve", "approved", "yes":
		return "approve", true
	case "reject", "rejected", "no":
		return "reject", true
	default:
		return "", false
	}
}

func projectTeamHistory(events []bmtypes.NativeExecutionEvent, req TeamRequest) []ExecutionEvent {
	out := make([]ExecutionEvent, 0, len(events))
	for index, native := range events {
		if native.History == nil {
			continue
		}
		kind := EventDiagnostic
		if native.IsTerminal() {
			kind = EventTerminal
		} else if strings.HasSuffix(native.NativeType, ".running") || strings.HasSuffix(native.NativeType, ".pending") {
			kind = EventParent
		}
		status, _ := native.Event.Payload["status"].(string)
		failure, _ := native.Event.Payload["error"].(string)
		result, _ := native.Event.Payload["result"].(string)
		parentID, _ := native.Event.Payload["task_id"].(string)
		out = append(out, ExecutionEvent{
			Sequence: index + 1, ID: native.History.EventID, ParentID: parentID, CorrelationID: req.CorrelationID,
			Kind: kind, PlanVersion: req.PlanVersion, CapabilityDigest: req.CapabilityDigest, ProjectionDigest: req.ProjectionDigest,
			Status: status, FailureClass: failure, Result: result,
			NativeSource:  NativeSource{Kind: string(native.History.Source.Kind), Namespace: native.History.Source.Namespace, ID: native.History.Source.ID},
			NativeEventID: native.History.EventID, NativeSequence: native.History.Sequence, NativeType: native.NativeType,
		})
	}
	return out
}

func projectNativeHistory(ctx context.Context, history bmtypes.NativeEventHistoryReader, source bmtypes.SourceRef, req AuthorizedRequest, parent string, terminal Terminal) ([]ExecutionEvent, string, bool, error) {
	caps, err := history.Capabilities(ctx, source)
	if err != nil {
		return nil, "", false, err
	}
	if !caps.Supported || !caps.SequenceDefined {
		return nil, "", caps.Durable, fmt.Errorf("baymax native history does not define source sequence")
	}
	page, err := history.ReadAfter(ctx, source, nil, bmtypes.MaxHistoryPageSize)
	if err != nil {
		return nil, "", caps.Durable, err
	}
	events := make([]ExecutionEvent, 0, len(page.Events))
	for index, native := range page.Events {
		if native.History == nil {
			continue
		}
		kind := EventDiagnostic
		if native.IsTerminal() {
			kind = EventTerminal
		} else if native.NativeType == "run.started" {
			kind = EventParent
		} else if strings.HasPrefix(native.NativeType, "tool.dispatch.") || native.NativeType == bmtypes.EventTypeToolLifecycleFinalized {
			kind = EventTool
		}
		status := native.SourceState()
		if status == "" {
			status = native.NativeType
		}
		events = append(events, ExecutionEvent{
			Sequence: index + 1, ID: native.History.EventID, ParentID: parent, CorrelationID: req.CorrelationID,
			Kind: kind, PlanVersion: req.PlanVersion, CapabilityDigest: req.CapabilityDigest, ProjectionDigest: req.ProjectionDigest,
			Status: status, FailureClass: terminal.FailureClass, NativeSource: NativeSource{Kind: string(native.History.Source.Kind), Namespace: native.History.Source.Namespace, ID: native.History.Source.ID},
			NativeEventID: native.History.EventID, NativeSequence: native.History.Sequence, NativeType: native.NativeType,
		})
	}
	if page.NextCursor == nil || page.NextCursor.Value == "" {
		return nil, "", caps.Durable, fmt.Errorf("baymax native history returned no cursor")
	}
	return events, page.NextCursor.Value, caps.Durable, nil
}

func normalizeTerminalState(state string) string {
	switch strings.ToLower(state) {
	case "completed":
		return "succeeded"
	case "canceled":
		return "canceled"
	case "input_required":
		return "paused"
	default:
		return "failed"
	}
}

type probeTool struct{}

func (probeTool) Name() string               { return "probe" }
func (probeTool) Description() string        { return "deterministic runtime probe tool" }
func (probeTool) JSONSchema() map[string]any { return map[string]any{"type": "object"} }
func (probeTool) Invoke(context.Context, map[string]any) (bmtypes.ToolResult, error) {
	return bmtypes.ToolResult{Content: "probe-tool-ok"}, nil
}

func (c *Client) Version() string { return PinnedVersion }

func (c *Client) Run(ctx context.Context, req runtimeprobe.ProbeRequest) (runtimeprobe.ProbeOutcome, error) {
	return c.execute(ctx, req, false)
}

func (c *Client) Stream(ctx context.Context, req runtimeprobe.ProbeRequest) (runtimeprobe.ProbeOutcome, error) {
	return c.execute(ctx, req, true)
}

func (c *Client) execute(ctx context.Context, req runtimeprobe.ProbeRequest, stream bool) (runtimeprobe.ProbeOutcome, error) {
	if c == nil || c.engine == nil {
		return runtimeprobe.ProbeOutcome{}, fmt.Errorf("baymax adapter is nil")
	}
	if req.CorrelationID == "" {
		return runtimeprobe.ProbeOutcome{}, fmt.Errorf("correlation id is required")
	}
	now := time.Unix(0, 0).UTC()
	parentID := req.ParentID
	if parentID == "" {
		parentID = req.CorrelationID + ":parent"
	}
	events := []runtimeprobe.Event{{Sequence: 1, ID: parentID, Kind: runtimeprobe.KindParent, CorrelationID: req.CorrelationID, Capability: req.Capability, Status: runtimeprobe.StatusPassed, At: now}}
	request := bmtypes.RunRequest{RunID: parentID, Input: req.Input}
	var result bmtypes.RunResult
	var err error
	if stream {
		result, err = c.engine.Stream(ctx, request, nil)
	} else {
		result, err = c.engine.Run(ctx, request, nil)
	}
	status := runtimeprobe.StatusPassed
	payload := map[string]string{"run_id": result.RunID, "iterations": fmt.Sprint(result.Iterations)}
	if err != nil || result.Error != nil {
		status = runtimeprobe.StatusFailedContract
		if err != nil {
			payload["error"] = err.Error()
		} else {
			payload["error_class"] = string(result.Error.Class)
		}
	}
	events[0].Status = status
	events = append(events, runtimeprobe.Event{Sequence: 2, ID: parentID + ":terminal", Kind: runtimeprobe.KindTerminal, ParentID: parentID, CorrelationID: req.CorrelationID, Capability: req.Capability, Status: status, At: now.Add(time.Nanosecond), Payload: payload})
	out := runtimeprobe.ProbeOutcome{Events: events}
	if err != nil {
		return out, err
	}
	return out, nil
}

func (c *Client) Child(ctx context.Context, req runtimeprobe.ProbeRequest) (runtimeprobe.ProbeOutcome, error) {
	if req.CorrelationID == "" {
		return runtimeprobe.ProbeOutcome{}, fmt.Errorf("correlation id is required")
	}
	comp, err := composer.NewBuilder(c.model).Build()
	if err != nil {
		return runtimeprobe.ProbeOutcome{}, err
	}
	result, err := comp.DispatchChild(ctx, composer.ChildDispatchRequest{
		Task:                  scheduler.Task{TaskID: req.CorrelationID + ":child", RunID: req.CorrelationID, ParentRunID: req.ParentID, Payload: map[string]any{"input": req.Input}},
		Target:                composer.ChildTargetLocal,
		WorkerID:              "probe-child",
		ParentDepth:           0,
		ParentRemainingBudget: time.Second,
		ChildTimeout:          time.Second,
		LocalRunner: composer.LocalChildRunnerFunc(func(context.Context, scheduler.Task) (map[string]any, error) {
			return map[string]any{"result": "child-complete"}, nil
		}),
	})
	status := runtimeprobe.StatusPassed
	if err != nil {
		status = runtimeprobe.StatusFailedContract
	}
	now := time.Unix(0, 0).UTC()
	payload := map[string]string{"task_id": result.Record.Task.TaskID, "child_status": string(result.Commit.Status)}
	digest := sha256.Sum256([]byte(req.Capability))
	payload["capability_digest"] = hex.EncodeToString(digest[:])
	payload["budget_ms"] = "1000"
	if err != nil {
		payload["error"] = err.Error()
	}
	events := []runtimeprobe.Event{{Sequence: 1, ID: req.CorrelationID + ":parent", Kind: runtimeprobe.KindParent, CorrelationID: req.CorrelationID, Capability: req.Capability, Status: status, At: now}, {Sequence: 2, ID: req.CorrelationID + ":child", Kind: runtimeprobe.KindChild, ParentID: req.CorrelationID + ":parent", CorrelationID: req.CorrelationID, Capability: req.Capability, Status: status, At: now.Add(time.Nanosecond), Payload: payload}, {Sequence: 3, ID: req.CorrelationID + ":terminal", Kind: runtimeprobe.KindTerminal, ParentID: req.CorrelationID + ":child", CorrelationID: req.CorrelationID, Capability: req.Capability, Status: status, At: now.Add(2 * time.Nanosecond)}}
	return runtimeprobe.ProbeOutcome{Events: events}, err
}
func (c *Client) TeamParallel(ctx context.Context, req runtimeprobe.ProbeRequest) (runtimeprobe.ProbeOutcome, error) {
	return c.team(ctx, req, teams.StrategyParallel)
}
func (c *Client) TeamVote(ctx context.Context, req runtimeprobe.ProbeRequest) (runtimeprobe.ProbeOutcome, error) {
	return c.team(ctx, req, teams.StrategyVote)
}

func (c *Client) team(ctx context.Context, req runtimeprobe.ProbeRequest, strategy teams.Strategy) (runtimeprobe.ProbeOutcome, error) {
	if req.CorrelationID == "" {
		return runtimeprobe.ProbeOutcome{}, fmt.Errorf("correlation id is required")
	}
	engine := teams.New()
	runner := teams.TaskRunnerFunc(func(context.Context, teams.Task) (teams.TaskResult, error) {
		return teams.TaskResult{Output: req.Input, Vote: "accept"}, nil
	})
	plan := teams.Plan{RunID: req.CorrelationID, TeamID: req.CorrelationID + ":team", Strategy: strategy, Tasks: []teams.Task{
		{TaskID: "task-a", AgentID: "agent-a", Role: teams.RoleWorker, Target: teams.TaskTargetLocal, Runner: runner},
		{TaskID: "task-b", AgentID: "agent-b", Role: teams.RoleWorker, Target: teams.TaskTargetLocal, Runner: runner},
	}, VoteTieBreak: teams.VoteTieBreakFirstTaskID}
	result, err := engine.Run(ctx, plan)
	status := runtimeprobe.StatusPassed
	if err != nil {
		status = runtimeprobe.StatusFailedContract
	}
	now := time.Unix(0, 0).UTC()
	payload := map[string]string{"team_id": result.TeamID, "strategy": string(strategy), "tasks": fmt.Sprint(result.TeamTaskTotal)}
	if err != nil {
		payload["error"] = err.Error()
	}
	events := []runtimeprobe.Event{{Sequence: 1, ID: req.CorrelationID + ":parent", Kind: runtimeprobe.KindParent, CorrelationID: req.CorrelationID, Capability: req.Capability, Status: status, At: now}, {Sequence: 2, ID: req.CorrelationID + ":team", Kind: runtimeprobe.KindTeam, ParentID: req.CorrelationID + ":parent", CorrelationID: req.CorrelationID, Capability: req.Capability, Status: status, At: now.Add(time.Nanosecond), Payload: payload}, {Sequence: 3, ID: req.CorrelationID + ":terminal", Kind: runtimeprobe.KindTerminal, ParentID: req.CorrelationID + ":team", CorrelationID: req.CorrelationID, Capability: req.Capability, Status: status, At: now.Add(2 * time.Nanosecond)}}
	return runtimeprobe.ProbeOutcome{Events: events}, err
}
func (c *Client) Mailbox(ctx context.Context, req runtimeprobe.ProbeRequest) (runtimeprobe.ProbeOutcome, error) {
	if req.CorrelationID == "" {
		return runtimeprobe.ProbeOutcome{}, fmt.Errorf("correlation id is required")
	}
	now := time.Unix(0, 0).UTC()
	clockNow := now
	clock := func() time.Time { return clockNow }
	mb, err := mailbox.New(mailbox.NewMemoryStore(mailbox.Policy{MaxAttempts: 2, DLQEnabled: true}), mailbox.WithClock(clock))
	if err != nil {
		return runtimeprobe.ProbeOutcome{}, err
	}
	_, err = mb.Publish(ctx, mailbox.Envelope{MessageID: "message-1", IdempotencyKey: req.CorrelationID, CorrelationID: req.CorrelationID, Kind: mailbox.KindCommand, Payload: map[string]any{"input": req.Input}})
	if err != nil {
		return runtimeprobe.ProbeOutcome{}, err
	}
	record, ok, err := mb.ConsumeWithLease(ctx, "consumer-a", time.Minute, false)
	if err != nil || !ok {
		return runtimeprobe.ProbeOutcome{}, fmt.Errorf("consume: %w", err)
	}
	_, invalidErr := mb.Ack(ctx, record.Envelope.MessageID, "consumer-b")
	if invalidErr == nil {
		return runtimeprobe.ProbeOutcome{}, fmt.Errorf("invalid lease transition was accepted")
	}
	if _, err = mb.Requeue(ctx, record.Envelope.MessageID, "consumer-a", "retry"); err != nil {
		return runtimeprobe.ProbeOutcome{}, err
	}
	clockNow = clockNow.Add(time.Second)
	record, ok, err = mb.ConsumeWithLease(ctx, "consumer-a", time.Minute, false)
	if err != nil || !ok {
		return runtimeprobe.ProbeOutcome{}, fmt.Errorf("reconsume: %w", err)
	}
	if _, err = mb.Ack(ctx, record.Envelope.MessageID, "consumer-a"); err != nil {
		return runtimeprobe.ProbeOutcome{}, err
	}
	_, err = mb.Publish(ctx, mailbox.Envelope{MessageID: "message-dlq", IdempotencyKey: req.CorrelationID + ":dlq", CorrelationID: req.CorrelationID, Kind: mailbox.KindCommand})
	if err != nil {
		return runtimeprobe.ProbeOutcome{}, err
	}
	dlq, ok, err := mb.ConsumeWithLease(ctx, "consumer-a", time.Minute, false)
	if err != nil || !ok {
		return runtimeprobe.ProbeOutcome{}, fmt.Errorf("dead-letter consume: %v", err)
	}
	if _, err = mb.Nack(ctx, dlq.Envelope.MessageID, "consumer-a", "failure"); err != nil {
		return runtimeprobe.ProbeOutcome{}, err
	}
	if _, err = mb.Requeue(ctx, dlq.Envelope.MessageID, "consumer-a", "retry"); err != nil {
		return runtimeprobe.ProbeOutcome{}, err
	}
	clockNow = clockNow.Add(time.Second)
	dlq, ok, err = mb.ConsumeWithLease(ctx, "consumer-a", time.Minute, false)
	if err != nil || !ok {
		return runtimeprobe.ProbeOutcome{}, fmt.Errorf("dead-letter retry consume: %v", err)
	}
	if _, err = mb.Nack(ctx, dlq.Envelope.MessageID, "consumer-a", "failure"); err != nil {
		return runtimeprobe.ProbeOutcome{}, err
	}
	stats, err := mb.Stats(ctx)
	if err != nil {
		return runtimeprobe.ProbeOutcome{}, err
	}
	status := runtimeprobe.StatusPassed
	payload := map[string]string{"published": "true", "consumed": "true", "acked": "true", "requeued": "true", "invalid_lease_rejected": "true", "ack_total": fmt.Sprint(stats.AckTotal)}
	payload["dead_letter_total"] = fmt.Sprint(stats.DeadLetterTotal)
	events := []runtimeprobe.Event{{Sequence: 1, ID: req.CorrelationID + ":parent", Kind: runtimeprobe.KindParent, CorrelationID: req.CorrelationID, Capability: req.Capability, Status: status, At: now}, {Sequence: 2, ID: req.CorrelationID + ":mailbox", Kind: runtimeprobe.KindMailbox, ParentID: req.CorrelationID + ":parent", CorrelationID: req.CorrelationID, Capability: req.Capability, Status: status, At: now.Add(time.Nanosecond), Payload: payload}, {Sequence: 3, ID: req.CorrelationID + ":terminal", Kind: runtimeprobe.KindTerminal, ParentID: req.CorrelationID + ":mailbox", CorrelationID: req.CorrelationID, Capability: req.Capability, Status: status, At: now.Add(2 * time.Nanosecond)}}
	return runtimeprobe.ProbeOutcome{Events: events}, nil
}
func (c *Client) Scheduler(ctx context.Context, req runtimeprobe.ProbeRequest) (runtimeprobe.ProbeOutcome, error) {
	if req.CorrelationID == "" {
		return runtimeprobe.ProbeOutcome{}, fmt.Errorf("correlation id is required")
	}
	now := time.Unix(0, 0).UTC()
	store := scheduler.NewMemoryStore()
	queue, err := scheduler.New(store)
	if err != nil {
		return runtimeprobe.ProbeOutcome{}, err
	}
	record, err := queue.Enqueue(ctx, scheduler.Task{
		TaskID:    req.CorrelationID + ":scheduled",
		RunID:     req.CorrelationID,
		NotBefore: time.Now().UTC().Add(40 * time.Millisecond),
		Payload:   map[string]any{"input": req.Input},
	})
	if err != nil {
		return runtimeprobe.ProbeOutcome{}, err
	}
	_, preDispatchClaim, err := queue.Claim(ctx, "probe-worker")
	if err != nil {
		return runtimeprobe.ProbeOutcome{}, fmt.Errorf("scheduler pre-dispatch claim: %w", err)
	}
	if preDispatchClaim {
		return runtimeprobe.ProbeOutcome{}, fmt.Errorf("scheduler claimed task before not_before")
	}
	time.Sleep(100 * time.Millisecond)
	claimed, ok, err := queue.Claim(ctx, "probe-worker")
	if err != nil || !ok {
		return runtimeprobe.ProbeOutcome{}, fmt.Errorf("scheduler delayed claim: %v", err)
	}
	if _, err = queue.Complete(ctx, scheduler.TerminalCommit{TaskID: record.Task.TaskID, AttemptID: claimed.Attempt.AttemptID, Status: scheduler.TaskStateSucceeded, Result: map[string]any{"ok": true}, CommittedAt: now}); err != nil {
		return runtimeprobe.ProbeOutcome{}, err
	}
	status := runtimeprobe.StatusPassed
	payload := map[string]string{
		"task_id":            record.Task.TaskID,
		"delayed":            "true",
		"pre_dispatch_claim": "false",
		"lease":              "claimed",
		"terminal":           "succeeded",
	}
	events := []runtimeprobe.Event{{Sequence: 1, ID: req.CorrelationID + ":parent", Kind: runtimeprobe.KindParent, CorrelationID: req.CorrelationID, Capability: req.Capability, Status: status, At: now}, {Sequence: 2, ID: req.CorrelationID + ":scheduler", Kind: runtimeprobe.KindScheduler, ParentID: req.CorrelationID + ":parent", CorrelationID: req.CorrelationID, Capability: req.Capability, Status: status, At: now.Add(time.Nanosecond), Payload: payload}, {Sequence: 3, ID: req.CorrelationID + ":terminal", Kind: runtimeprobe.KindTerminal, ParentID: req.CorrelationID + ":scheduler", CorrelationID: req.CorrelationID, Capability: req.Capability, Status: status, At: now.Add(2 * time.Nanosecond)}}
	return runtimeprobe.ProbeOutcome{Events: events}, nil
}
func (c *Client) ContextAssembly(ctx context.Context, req runtimeprobe.ProbeRequest) (runtimeprobe.ProbeOutcome, error) {
	if req.CorrelationID == "" {
		return runtimeprobe.ProbeOutcome{}, fmt.Errorf("correlation id is required")
	}
	cfg := runtimeconfig.DefaultConfig().ContextAssembler
	cfg.Enabled = false
	a := assembler.New(func() runtimeconfig.ContextAssemblerConfig { return cfg })
	assembleReq := bmtypes.ContextAssembleRequest{RunID: req.CorrelationID, SessionID: req.CorrelationID + ":session", PrefixVersion: "runtime-probe.v1", Input: req.Input}
	modelReq, result, err := a.Assemble(ctx, assembleReq, bmtypes.ModelRequest{RunID: req.CorrelationID, Input: req.Input})
	var handoffDigest string
	if err == nil {
		handoff, handoffErr := a.BuildHandoff(assembleReq, modelReq, result)
		if handoffErr != nil {
			err = handoffErr
		} else if encoded, marshalErr := json.Marshal(handoff); marshalErr != nil {
			err = marshalErr
		} else {
			digest := sha256.Sum256(encoded)
			handoffDigest = hex.EncodeToString(digest[:])
		}
	}
	status := runtimeprobe.StatusPassed
	if err != nil {
		status = runtimeprobe.StatusFailedContract
	}
	now := time.Unix(0, 0).UTC()
	payload := map[string]string{
		"prefix_version": result.Prefix.PrefixVersion,
		"prefix_hash":    result.Prefix.PrefixHash,
		"prefix_guard":   result.Prefix.PrefixVersion,
		"handoff_digest": handoffDigest,
		"derived_budget": "2048",
		"stage":          string(result.Stage.Status),
		"input_bytes":    fmt.Sprint(len(modelReq.Input)),
	}
	if err != nil {
		payload["error"] = err.Error()
	}
	events := []runtimeprobe.Event{{Sequence: 1, ID: req.CorrelationID + ":parent", Kind: runtimeprobe.KindParent, CorrelationID: req.CorrelationID, Capability: req.Capability, Status: status, At: now}, {Sequence: 2, ID: req.CorrelationID + ":context", Kind: runtimeprobe.KindContext, ParentID: req.CorrelationID + ":parent", CorrelationID: req.CorrelationID, Capability: req.Capability, Status: status, At: now.Add(time.Nanosecond), Payload: payload}, {Sequence: 3, ID: req.CorrelationID + ":terminal", Kind: runtimeprobe.KindTerminal, ParentID: req.CorrelationID + ":context", CorrelationID: req.CorrelationID, Capability: req.Capability, Status: status, At: now.Add(2 * time.Nanosecond)}}
	return runtimeprobe.ProbeOutcome{Events: events}, err
}

var _ Adapter = (*Client)(nil)
var _ runtimeboundary.NativeControlAdapter = (*Client)(nil)
var _ runtimeboundary.NativeRetryAdapter = (*Client)(nil)
var _ runtimeboundary.NativeRecoveryAdapter = (*Client)(nil)
