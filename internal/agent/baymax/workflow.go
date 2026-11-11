package baymax

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/FelixSeptem/baymax/core/history"
	bmtypes "github.com/FelixSeptem/baymax/core/types"
	"github.com/FelixSeptem/baymax/orchestration/workflow"
)

const (
	workflowContractVersion  = "danny.workflow.v1"
	maxWorkflowStepBudgetMS  = 10 * 60 * 1000
	maxWorkflowTotalBudgetMS = 30 * 60 * 1000
	maxWorkflowRetries       = 3
)

// WorkflowContractVersion is the versioned request contract callers must send
// in WorkflowRequest.Version. It is exported so harnesses and callers do not
// duplicate the literal.
const WorkflowContractVersion = workflowContractVersion

var (
	ErrWorkflowRequest = errors.New("baymax: invalid authorized workflow request")
	ErrWorkflowPaused  = workflow.ErrWorkflowPaused
)

// WorkflowAssignment is Danny's versioned projection of one native workflow
// step. Budget is expressed in milliseconds and is carried into the native
// step timeout and the authorized child request.
type WorkflowAssignment struct {
	ID               string
	Role             string
	Objective        string
	DependsOn        []string
	Budget           int
	Timeout          time.Duration
	RetryMaxAttempts int
	RetryBackoff     time.Duration
}

type WorkflowPause struct {
	Reason                 string
	PendingActionReference string
	ConsumedBudget         int64
	OriginalDeadline       time.Time
}

// WorkflowPauseBeforeStep is an application authority gate. It can pause a
// native boundary, but it does not own checkpoint creation or resume logic.
type WorkflowPauseBeforeStep func(context.Context, WorkflowAssignment) (WorkflowPause, bool)

type WorkflowResume struct {
	Checkpoint     WorkflowCheckpoint
	GraphDigest    string
	Generation     uint64
	IdempotencyKey string
}

type WorkflowRequest struct {
	Version string

	RunID, WorkflowID, SessionID, ParentRunID, CausationID, CorrelationID string
	PlanVersion, PlanDigest, CapabilityDigest, ProjectionDigest           string
	WriteScopeDigest, EffectNamespace, OccurrenceID                       string
	HistoryPath, ContinuationPath                                         string
	Assignments                                                           []WorkflowAssignment
	MaxSteps                                                              int
	ToolPolicy                                                            ToolPolicy
	PauseBeforeStep                                                       WorkflowPauseBeforeStep
	Resume                                                                *WorkflowResume
}

type WorkflowCheckpoint struct {
	CheckpointID, Digest, OwnerNamespace, GraphID, GraphVersion                  string
	RunID, SessionID, ParentRunID, CausationID                                   string
	AuthorityDigest, PlanDigest, WriteScopeDigest, EffectNamespace, OccurrenceID string
	PauseReason                                                                  string
	Generation                                                                   uint64
	CreatedAt, ExpiresAt                                                         time.Time
}

type WorkflowStepResult struct {
	ID, Role, NativeRunID, CorrelationID, Status, Result, FailureClass string
}

type WorkflowEvent struct {
	Sequence                         int
	ID, ParentID, CorrelationID      string
	Kind                             EventKind
	Status, FailureClass, NativeType string
	NativeSource                     NativeSource
	NativeEventID                    string
	NativeSequence                   uint64
}

type WorkflowExecutionResult struct {
	RunID, WorkflowID, Status, FailureClass, NativeCursor, GraphDigest string
	NativeHistoryDurable, ContinuationSupported                        bool
	ExecutionOrder                                                     []string
	Steps                                                              []WorkflowStepResult
	Events                                                             []WorkflowEvent
	Checkpoint                                                         *WorkflowCheckpoint
}

func (c *Client) RunWorkflowAuthorized(ctx context.Context, req WorkflowRequest) (output WorkflowExecutionResult, returnErr error) {
	if err := validateWorkflowRequest(req); err != nil {
		return WorkflowExecutionResult{}, err
	}
	assignments := make(map[string]WorkflowAssignment, len(req.Assignments))
	steps := make([]workflow.Step, 0, len(req.Assignments))
	for _, assignment := range req.Assignments {
		assignment.DependsOn = append([]string(nil), assignment.DependsOn...)
		assignments[assignment.ID] = assignment
		timeout := assignment.Timeout
		if timeout <= 0 {
			timeout = time.Duration(assignment.Budget) * time.Millisecond
		}
		condition := workflow.ConditionAlways
		if len(assignment.DependsOn) > 0 {
			// A dependent assignment is required work by default. Native
			// workflow condition semantics then skip it after a failed parent.
			condition = workflow.ConditionOnSuccess
		}
		steps = append(steps, workflow.Step{
			StepID: assignment.ID, TaskID: req.RunID + ":task:" + assignment.ID,
			AgentID: assignment.ID, Kind: workflow.StepKindRunner, DependsOn: assignment.DependsOn,
			Condition: condition, Timeout: timeout,
			Retry:   workflow.Retry{MaxAttempts: assignment.RetryMaxAttempts, Backoff: assignment.RetryBackoff},
			Payload: map[string]any{"role": assignment.Role, "objective": assignment.Objective, "correlation_id": req.CorrelationID + ":" + assignment.ID, "authority_digest": req.CapabilityDigest, "plan_digest": req.PlanDigest, "write_scope_digest": req.WriteScopeDigest, "effect_namespace": req.EffectNamespace},
		})
	}
	definition := workflow.Definition{WorkflowID: req.WorkflowID, Steps: steps}

	source := bmtypes.SourceRef{Kind: bmtypes.HistorySourceWorkflow, Namespace: "danny.baymax.workflow.history.v1", ID: req.WorkflowID}
	journal, err := workflow.OpenNativeEventHistory(req.HistoryPath, source, history.WithRetention(maxWorkflowHistoryRetention))
	if err != nil {
		return WorkflowExecutionResult{}, err
	}

	options := []workflow.Option{
		workflow.WithNativeEventHistory(journal),
		workflow.WithStepAdapter(workflow.DispatchAdapter{Runner: func(stepCtx context.Context, workflowID string, step workflow.Step, _ int) (workflow.StepOutput, error) {
			assignment, ok := assignments[step.StepID]
			if !ok {
				return workflow.StepOutput{}, fmt.Errorf("workflow assignment %q is missing", step.StepID)
			}
			stepRunID := req.RunID + ":step:" + assignment.ID
			out, runErr := c.RunAuthorized(stepCtx, AuthorizedRequest{
				RunID: stepRunID, ParentRunID: req.RunID, CorrelationID: req.CorrelationID + ":" + assignment.ID,
				PlanVersion: req.PlanVersion, PlanDigest: req.PlanDigest, CapabilityDigest: req.CapabilityDigest, ProjectionDigest: req.ProjectionDigest,
				Role: assignment.Role, PerChildBudget: assignment.Budget, Input: assignment.Objective, ToolPolicy: req.ToolPolicy,
			})
			if runErr != nil {
				return workflow.StepOutput{}, runErr
			}
			if out.Terminal.Status != "succeeded" {
				return workflow.StepOutput{}, fmt.Errorf("workflow assignment %s terminal status %s: %s", assignment.ID, out.Terminal.Status, out.Terminal.FailureClass)
			}
			return workflow.StepOutput{Payload: map[string]any{
				"native_run_id": stepRunID, "correlation_id": req.CorrelationID + ":" + assignment.ID,
				"status": out.Terminal.Status, "result": out.Terminal.Result,
			}, EffectReference: req.EffectNamespace + ":" + assignment.ID}, nil
		}}),
	}
	if req.ContinuationPath != "" {
		store, storeErr := workflow.NewFileWorkflowContinuationStore(req.ContinuationPath, "workflow")
		if storeErr != nil {
			return WorkflowExecutionResult{}, storeErr
		}
		defer func() {
			if closeErr := store.Close(); closeErr != nil && returnErr == nil {
				output = WorkflowExecutionResult{}
				returnErr = closeErr
			}
		}()
		options = append(options, workflow.WithWorkflowContinuationStore(store, "workflow"))
	}
	if req.PauseBeforeStep != nil {
		options = append(options, workflow.WithPauseController(workflow.WorkflowPauseControllerFunc(func(pauseCtx context.Context, boundary workflow.WorkflowPauseBoundary) error {
			assignment, ok := assignments[boundary.Step.StepID]
			if !ok {
				return fmt.Errorf("workflow pause assignment %q is missing", boundary.Step.StepID)
			}
			pause, shouldPause := req.PauseBeforeStep(pauseCtx, assignment)
			if !shouldPause {
				return nil
			}
			if !validWorkflowPauseReason(pause.Reason) {
				return fmt.Errorf("%w: unsupported pause reason %q", ErrWorkflowRequest, pause.Reason)
			}
			return workflow.PauseError{Reason: workflowPauseReason(pause.Reason), PendingActionReference: pause.PendingActionReference, ConsumedBudget: pause.ConsumedBudget, OriginalDeadline: pause.OriginalDeadline}
		})))
	}
	engine := workflow.New(options...)
	var native workflow.RunResult
	if req.Resume != nil {
		native, err = engine.ResumeContinuation(ctx, workflowResumeRequest(req), nil)
	} else {
		native, err = engine.Run(ctx, workflow.RunRequest{
			RunID: req.RunID, SessionID: req.SessionID, ParentRunID: req.ParentRunID, CausationID: req.CausationID,
			AuthorityDigest: req.CapabilityDigest, PlanDigest: req.PlanDigest, WriteScopeDigest: req.WriteScopeDigest,
			EffectNamespace: req.EffectNamespace, OccurrenceID: req.OccurrenceID, DSL: definition,
		})
	}
	continuationSupported := false
	if req.ContinuationPath != "" {
		if _, capabilityErr := engine.ContinuationCapabilities(ctx); capabilityErr != nil {
			return WorkflowExecutionResult{}, capabilityErr
		}
		continuationSupported = true
	}
	result, projectionErr := projectWorkflowResult(ctx, req, native, journal, source, continuationSupported)
	if projectionErr != nil {
		return WorkflowExecutionResult{}, projectionErr
	}
	if native.WorkflowCheckpoint != nil {
		checkpoint := workflowCheckpointFromNative(*native.WorkflowCheckpoint, native.WorkflowCheckpointGeneration)
		result.Checkpoint = &checkpoint
	}
	if err != nil {
		return result, err
	}
	if native.WorkflowStatus == string(workflow.StepStatusFailed) || native.WorkflowStatus == "failed" {
		return result, fmt.Errorf("workflow terminal status %q", native.WorkflowStatus)
	}
	return result, nil
}

const maxWorkflowHistoryRetention = 2048

func validateWorkflowRequest(req WorkflowRequest) error {
	if req.Version != workflowContractVersion || req.RunID == "" || req.WorkflowID == "" || req.SessionID == "" || req.CorrelationID == "" || req.PlanVersion == "" || req.PlanDigest == "" || req.CapabilityDigest == "" || req.ProjectionDigest == "" || req.WriteScopeDigest == "" || req.EffectNamespace == "" || req.HistoryPath == "" || len(req.Assignments) == 0 {
		return ErrWorkflowRequest
	}
	maxSteps := req.MaxSteps
	if maxSteps == 0 {
		maxSteps = 64
	}
	if maxSteps < 1 || maxSteps > 64 || len(req.Assignments) > maxSteps {
		return fmt.Errorf("%w: workflow step limit", ErrWorkflowRequest)
	}
	seen := make(map[string]struct{}, len(req.Assignments))
	totalBudget := 0
	for _, assignment := range req.Assignments {
		if assignment.ID == "" || assignment.Role == "" || assignment.Objective == "" || assignment.Budget <= 0 || assignment.Budget > maxWorkflowStepBudgetMS || assignment.RetryMaxAttempts < 0 || assignment.RetryMaxAttempts > maxWorkflowRetries || assignment.RetryBackoff < 0 || assignment.Timeout < 0 || assignment.Timeout > time.Duration(maxWorkflowStepBudgetMS)*time.Millisecond {
			return ErrWorkflowRequest
		}
		totalBudget += assignment.Budget
		if totalBudget > maxWorkflowTotalBudgetMS {
			return fmt.Errorf("%w: workflow aggregate budget limit", ErrWorkflowRequest)
		}
		if _, ok := seen[assignment.ID]; ok {
			return fmt.Errorf("%w: duplicate assignment %q", ErrWorkflowRequest, assignment.ID)
		}
		seen[assignment.ID] = struct{}{}
	}
	for _, assignment := range req.Assignments {
		for _, dep := range assignment.DependsOn {
			if _, ok := seen[dep]; !ok || dep == assignment.ID {
				return fmt.Errorf("%w: invalid dependency %q", ErrWorkflowRequest, dep)
			}
		}
	}
	if req.Resume != nil && req.ContinuationPath == "" {
		return fmt.Errorf("%w: continuation path is required for resume", ErrWorkflowRequest)
	}
	if req.PauseBeforeStep != nil && req.ContinuationPath == "" {
		return fmt.Errorf("%w: continuation path is required for pause", ErrWorkflowRequest)
	}
	return nil
}

func workflowPauseReason(reason string) workflow.WorkflowPauseReason {
	switch strings.TrimSpace(reason) {
	case string(workflow.PauseInputRequired):
		return workflow.PauseInputRequired
	case string(workflow.PauseBudgetExhausted):
		return workflow.PauseBudgetExhausted
	default:
		return workflow.PauseApprovalRequired
	}
}

func validWorkflowPauseReason(reason string) bool {
	switch strings.TrimSpace(reason) {
	case string(workflow.PauseInputRequired), string(workflow.PauseApprovalRequired), string(workflow.PauseBudgetExhausted):
		return true
	default:
		return false
	}
}

func workflowResumeRequest(req WorkflowRequest) workflow.WorkflowResumeRequest {
	checkpoint := req.Resume.Checkpoint
	return workflow.WorkflowResumeRequest{
		Ref: workflow.WorkflowContinuationRef{
			SchemaVersion: workflow.WorkflowContinuationVersion, CheckpointID: checkpoint.CheckpointID, Digest: checkpoint.Digest,
			OwnerNamespace: checkpoint.OwnerNamespace, GraphID: checkpoint.GraphID, GraphVersion: checkpoint.GraphVersion,
			RunID: checkpoint.RunID, SessionID: checkpoint.SessionID, ParentRunID: checkpoint.ParentRunID, CausationID: checkpoint.CausationID,
			AuthorityDigest: checkpoint.AuthorityDigest, PlanDigest: checkpoint.PlanDigest, WriteScopeDigest: checkpoint.WriteScopeDigest,
			EffectNamespace: checkpoint.EffectNamespace, OccurrenceID: checkpoint.OccurrenceID, PauseReason: workflowPauseReason(checkpoint.PauseReason),
			CreatedAt: checkpoint.CreatedAt, ExpiresAt: checkpoint.ExpiresAt,
		},
		GraphDigest: req.Resume.GraphDigest, Generation: req.Resume.Generation, IdempotencyKey: req.Resume.IdempotencyKey,
		AuthorityDigest: req.CapabilityDigest, PlanDigest: req.PlanDigest, WriteScopeDigest: req.WriteScopeDigest,
		EffectNamespace: req.EffectNamespace, OccurrenceID: req.OccurrenceID,
	}
}

func workflowCheckpointFromNative(ref workflow.WorkflowContinuationRef, generation uint64) WorkflowCheckpoint {
	return WorkflowCheckpoint{
		CheckpointID: ref.CheckpointID, Digest: ref.Digest, OwnerNamespace: ref.OwnerNamespace, GraphID: ref.GraphID, GraphVersion: ref.GraphVersion,
		RunID: ref.RunID, SessionID: ref.SessionID, ParentRunID: ref.ParentRunID, CausationID: ref.CausationID,
		AuthorityDigest: ref.AuthorityDigest, PlanDigest: ref.PlanDigest, WriteScopeDigest: ref.WriteScopeDigest,
		EffectNamespace: ref.EffectNamespace, OccurrenceID: ref.OccurrenceID, PauseReason: string(ref.PauseReason), Generation: generation,
		CreatedAt: ref.CreatedAt, ExpiresAt: ref.ExpiresAt,
	}
}

func projectWorkflowResult(ctx context.Context, req WorkflowRequest, native workflow.RunResult, journal bmtypes.NativeEventHistoryReader, source bmtypes.SourceRef, continuationSupported bool) (WorkflowExecutionResult, error) {
	caps, err := journal.Capabilities(ctx, source)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	if !caps.Supported || !caps.SequenceDefined {
		return WorkflowExecutionResult{}, fmt.Errorf("baymax native workflow history is not cursor-addressable")
	}
	page, err := journal.ReadAfter(ctx, source, nil, bmtypes.MaxHistoryPageSize)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	result := WorkflowExecutionResult{
		RunID: req.RunID, WorkflowID: req.WorkflowID, Status: native.WorkflowStatus, GraphDigest: native.WorkflowGraphDigest,
		NativeHistoryDurable: caps.Durable, ContinuationSupported: continuationSupported, ExecutionOrder: workflowExecutionOrder(native),
		Steps: make([]WorkflowStepResult, 0, len(native.Steps)), Events: make([]WorkflowEvent, 0, len(page.Events)),
	}
	for _, step := range native.Steps {
		assignment, ok := findWorkflowAssignment(req.Assignments, step.StepID)
		if !ok {
			continue
		}
		item := WorkflowStepResult{ID: step.StepID, Role: assignment.Role, NativeRunID: req.RunID + ":step:" + step.StepID, CorrelationID: req.CorrelationID + ":" + step.StepID, Status: string(step.Status), FailureClass: step.Error}
		if step.Output.Payload != nil {
			if value, ok := step.Output.Payload["result"].(string); ok {
				item.Result = value
			}
		}
		result.Steps = append(result.Steps, item)
		if item.FailureClass != "" && result.FailureClass == "" {
			result.FailureClass = item.FailureClass
		}
	}
	if page.NextCursor != nil {
		result.NativeCursor = page.NextCursor.Value
	}
	for index, event := range page.Events {
		if event.History == nil {
			continue
		}
		kind := EventDiagnostic
		if event.IsTerminal() {
			kind = EventTerminal
		}
		status := event.SourceState()
		if status == "" {
			status = event.NativeType
		}
		result.Events = append(result.Events, WorkflowEvent{Sequence: index + 1, ID: event.History.EventID, CorrelationID: req.CorrelationID, Kind: kind, Status: status, NativeType: event.NativeType, NativeSource: NativeSource{Kind: string(event.History.Source.Kind), Namespace: event.History.Source.Namespace, ID: event.History.Source.ID}, NativeEventID: event.History.EventID, NativeSequence: event.History.Sequence})
	}
	return result, nil
}

func findWorkflowAssignment(assignments []WorkflowAssignment, id string) (WorkflowAssignment, bool) {
	for _, assignment := range assignments {
		if assignment.ID == id {
			return assignment, true
		}
	}
	return WorkflowAssignment{}, false
}

func workflowExecutionOrder(native workflow.RunResult) []string {
	status := make(map[string]workflow.StepStatus, len(native.Steps))
	for _, step := range native.Steps {
		status[step.StepID] = step.Status
	}
	out := make([]string, 0, len(native.ExecutionOrder))
	for _, id := range native.ExecutionOrder {
		if status[id] != workflow.StepStatusSkipped {
			out = append(out, id)
		}
	}
	return out
}
