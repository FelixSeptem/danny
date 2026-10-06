package baymax

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/FelixSeptem/baymax/context/assembler"
	"github.com/FelixSeptem/baymax/core/runner"
	bmtypes "github.com/FelixSeptem/baymax/core/types"
	"github.com/FelixSeptem/baymax/orchestration/composer"
	"github.com/FelixSeptem/baymax/orchestration/mailbox"
	"github.com/FelixSeptem/baymax/orchestration/scheduler"
	"github.com/FelixSeptem/baymax/orchestration/teams"
	runtimeconfig "github.com/FelixSeptem/baymax/runtime/config"
	"github.com/FelixSeptem/baymax/tool/local"
	"github.com/FelixSeptem/danny/internal/agent/knowledge"
	"github.com/FelixSeptem/danny/internal/runtimeprobe"
)

const PinnedVersion = "v0.0.0-20261003094000-553e07625efb"

var ErrCapabilityNotWired = errors.New("baymax capability is not wired into the runtime probe adapter")
var ErrAuthorizedRequest = errors.New("baymax: invalid authorized request")

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
	out, err := e.Client.RunAuthorized(ctx, AuthorizedRequest{RunID: req.RunID, ParentRunID: req.ParentRunID, CorrelationID: req.CorrelationID, PlanVersion: req.PlanVersion, CapabilityDigest: req.CapabilityDigest, ProjectionDigest: req.ProjectionDigest, Input: req.Input})
	if err != nil {
		return knowledge.ExecutionOutcome{}, err
	}
	return knowledge.ExecutionOutcome{Status: out.Terminal.Status, Result: out.Terminal.Result, FailureClass: out.Terminal.FailureClass}, nil
}

func (e KnowledgeExecutor) RunChild(ctx context.Context, req knowledge.ExecutionRequest) (knowledge.ExecutionOutcome, error) {
	if e.Client == nil || req.RunID == "" || req.ParentRunID == "" || req.CapabilityDigest == "" {
		return knowledge.ExecutionOutcome{}, ErrAuthorizedRequest
	}
	out, err := e.Client.Child(ctx, runtimeprobe.ProbeRequest{CorrelationID: req.CorrelationID, ParentID: req.ParentRunID, Input: req.Input, Capability: req.CapabilityDigest})
	if err != nil {
		return knowledge.ExecutionOutcome{}, err
	}
	status := "succeeded"
	if len(out.Events) > 0 && out.Events[len(out.Events)-1].Status != runtimeprobe.StatusPassed {
		status = "failed"
	}
	return knowledge.ExecutionOutcome{Status: status}, nil
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
	return &Client{engine: runner.New(model, opts...), model: model, assembler: assembler.New(func() runtimeconfig.ContextAssemblerConfig { return cfg })}, nil
}

type AuthorizedRequest struct {
	RunID, ParentRunID, CorrelationID               string
	PlanVersion, CapabilityDigest, ProjectionDigest string
	Prefix, Input                                   string
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
}

type Terminal struct{ Status, FailureClass, Result string }

type ExecutionResult struct {
	ParentRunID, PlanVersion, CapabilityDigest, ProjectionDigest string
	Events                                                       []ExecutionEvent
	Terminal                                                     Terminal
}

func (c *Client) RunAuthorized(ctx context.Context, req AuthorizedRequest) (ExecutionResult, error) {
	return c.executeAuthorized(ctx, req, false)
}

func (c *Client) StreamAuthorized(ctx context.Context, req AuthorizedRequest) (ExecutionResult, error) {
	return c.executeAuthorized(ctx, req, true)
}

func (c *Client) executeAuthorized(ctx context.Context, req AuthorizedRequest, stream bool) (ExecutionResult, error) {
	if c == nil || c.engine == nil || req.RunID == "" || req.CorrelationID == "" || req.CapabilityDigest == "" || req.PlanVersion == "" {
		return ExecutionResult{}, ErrAuthorizedRequest
	}
	parent := req.ParentRunID
	if parent == "" {
		parent = req.RunID
	}
	assembleReq := bmtypes.ContextAssembleRequest{RunID: req.RunID, SessionID: req.CorrelationID, PrefixVersion: req.Prefix, Input: req.Input}
	modelReq := bmtypes.ModelRequest{RunID: req.RunID, Input: req.Input}
	if c.assembler != nil {
		var err error
		modelReq, _, err = c.assembler.Assemble(ctx, assembleReq, modelReq)
		if err != nil {
			return ExecutionResult{}, err
		}
	}
	request := bmtypes.RunRequest{RunID: req.RunID, Input: modelReq.Input}
	var result bmtypes.RunResult
	var err error
	if stream {
		result, err = c.engine.Stream(ctx, request, nil)
	} else {
		result, err = c.engine.Run(ctx, request, nil)
	}
	terminal := Terminal{Status: "succeeded", Result: result.FinalAnswer}
	if err != nil || result.Error != nil {
		terminal.Status = "failed"
		if err != nil {
			terminal.FailureClass = err.Error()
		} else {
			terminal.FailureClass = string(result.Error.Class)
		}
	}
	events := []ExecutionEvent{{Sequence: 1, ID: parent, Kind: EventParent, CorrelationID: req.CorrelationID, PlanVersion: req.PlanVersion, CapabilityDigest: req.CapabilityDigest, ProjectionDigest: req.ProjectionDigest, Status: terminal.Status}, {Sequence: 2, ID: req.RunID + ":terminal", ParentID: parent, Kind: EventTerminal, CorrelationID: req.CorrelationID, PlanVersion: req.PlanVersion, CapabilityDigest: req.CapabilityDigest, ProjectionDigest: req.ProjectionDigest, Status: terminal.Status, FailureClass: terminal.FailureClass, Result: terminal.Result}}
	out := ExecutionResult{ParentRunID: parent, PlanVersion: req.PlanVersion, CapabilityDigest: req.CapabilityDigest, ProjectionDigest: req.ProjectionDigest, Events: events, Terminal: terminal}
	return out, err
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

func (c *Client) unsupported(name string) (runtimeprobe.ProbeOutcome, error) {
	return runtimeprobe.ProbeOutcome{}, fmt.Errorf("%w: %s", ErrCapabilityNotWired, name)
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
