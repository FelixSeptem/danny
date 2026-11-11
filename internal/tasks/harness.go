package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/FelixSeptem/danny/internal/agent/knowledge"
	"github.com/FelixSeptem/danny/internal/runtimeboundary"
	"github.com/FelixSeptem/danny/internal/storage"
	"github.com/google/uuid"
)

var (
	ErrInvalidHarnessRequest   = errors.New("tasks: invalid harness request")
	ErrRecoveryMismatch        = errors.New("tasks: checkpoint recovery mismatch")
	ErrCommitBlocked           = errors.New("tasks: commit gate blocked")
	ErrDispatchUncertain       = errors.New("tasks: dispatch outcome is uncertain")
	ErrRecoveryRequired        = errors.New("tasks: recovery evidence is required")
	ErrRecoveryGap             = errors.New("tasks: recovery evidence gap")
	ErrProductRevisionConflict = errors.New("tasks: product task revision conflict")
)

type UnsupportedModeError struct{ Mode knowledge.ExecutionMode }

func (e UnsupportedModeError) Error() string {
	return fmt.Sprintf("tasks: unsupported execution mode %q", e.Mode)
}
func (e UnsupportedModeError) Unwrap() error { return knowledge.ErrUnsupportedMode }

type AdmissionRequest struct {
	ExecutionID, TaskID, ParentRunID, CorrelationID, IdempotencyKey string
	TaskRevision                                                    int64
	Profile                                                         knowledge.TaskProfile
	Policy                                                          knowledge.Policy
	ProjectionDigest                                                string
	RequestedMode                                                   knowledge.ExecutionMode
}

type ScheduledAdmissionRequest struct {
	Occurrence    storage.ScheduleOccurrence
	TaskID        string
	TaskRevision  int64
	Profile       knowledge.TaskProfile
	Policy        knowledge.Policy
	RequestedMode knowledge.ExecutionMode
}

type ScheduledRunRequest struct {
	Occurrence    storage.ScheduleOccurrence
	TaskID        string
	TaskRevision  int64
	Profile       knowledge.TaskProfile
	Policy        knowledge.Policy
	RequestedMode knowledge.ExecutionMode
}

type RunResult struct {
	Execution storage.TaskExecution
	Plan      knowledge.ExecutionPlan
	Outcome   knowledge.ExecutionOutcome
}

type Harness struct {
	Repository   storage.ExecutionRepository
	Planner      knowledge.Planner
	Executor     knowledge.Executor
	Continuation runtimeboundary.NativeContinuationAdapter
	Control      runtimeboundary.NativeControlAdapter
	RetryAdapter runtimeboundary.NativeRetryAdapter
	Recovery     runtimeboundary.NativeRecoveryAdapter
}

// Reconcile resolves a dispatch/checkpoint/projection crash window by asking
// the source owner for retained facts. It never calls the execution adapter.
// A terminal source fact is projected transactionally before the product
// execution is committed; a source checkpoint is bound before pausing. Any
// missing or gapped source history remains recovery-required.
func (h *Harness) Reconcile(ctx context.Context, executionID string) (RunResult, error) {
	if h == nil || h.Repository == nil || h.Recovery == nil || executionID == "" {
		return RunResult{}, ErrRecoveryRequired
	}
	execution, err := h.Repository.GetExecution(ctx, executionID)
	if err != nil {
		return RunResult{}, err
	}
	if execution.Status != storage.ExecutionRecoveryRequired && execution.Status != storage.ExecutionDispatching && execution.Status != storage.ExecutionRunning {
		return RunResult{}, fmt.Errorf("%w: execution status %s is not reconcilable", ErrRecoveryRequired, execution.Status)
	}
	plan, err := h.currentPlan(ctx, executionID)
	if err != nil {
		return RunResult{}, err
	}
	request := runtimeboundary.RecoveryRequest{Version: runtimeboundary.ExecutionContractV1, ExecutionID: executionID, NativeRunID: executionID, SourceKind: "runner", SourceNamespace: "danny.baymax.runner.history.v1", SourceID: executionID, Authority: runtimeboundary.Authority{TaskRevision: execution.TaskRevision, PlanVersion: execution.PlanVersion, PlanDigest: execution.PlanDigest, CapabilityDigest: execution.CapabilityDigest, ProjectionDigest: execution.ProjectionDigest, WriteScope: fmt.Sprintf("%d", plan.WriteScope), EffectNamespace: executionID + ":effects"}}
	if err := request.Validate(); err != nil {
		return RunResult{}, fmt.Errorf("%w: invalid recovery request: %v", ErrRecoveryRequired, err)
	}
	outcome, err := h.Recovery.Reconcile(ctx, request)
	if err != nil {
		if errors.Is(err, storage.ErrReplayGap) || errors.Is(err, runtimeboundary.ErrCapabilityMissing) {
			return RunResult{}, fmt.Errorf("%w: %v", ErrRecoveryGap, err)
		}
		return RunResult{}, fmt.Errorf("%w: %v", ErrRecoveryRequired, err)
	}
	switch outcome.Status {
	case runtimeboundary.RecoveryGap:
		return RunResult{}, ErrRecoveryGap
	case runtimeboundary.RecoveryMissing, "":
		return RunResult{}, ErrRecoveryRequired
	case runtimeboundary.RecoveryCheckpoint:
		if outcome.Checkpoint == nil {
			return RunResult{}, ErrRecoveryRequired
		}
		binding := runtimeboundary.CheckpointBinding{Version: runtimeboundary.ExecutionContractV1, ExecutionID: executionID, NativeID: outcome.Checkpoint.CheckpointID, NativeVersion: outcome.Checkpoint.Version, NativeDigest: outcome.Checkpoint.Digest, SourceCursor: outcome.Checkpoint.SourcePosition, OwnerNamespace: outcome.Checkpoint.OwnerNamespace, PauseReason: outcome.Checkpoint.PauseReason, Authority: request.Authority}
		if err := h.PauseNative(ctx, binding, "reconciled", nil); err != nil {
			return RunResult{}, fmt.Errorf("%w: bind native checkpoint: %v", ErrRecoveryRequired, err)
		}
		stored, err := h.Repository.GetExecution(ctx, executionID)
		if err != nil {
			return RunResult{}, err
		}
		return RunResult{Execution: stored, Plan: plan, Outcome: knowledge.ExecutionOutcome{Status: "paused"}}, nil
	case runtimeboundary.RecoveryTerminal:
		return h.reconcileTerminal(ctx, execution, plan, request, outcome)
	default:
		return RunResult{}, fmt.Errorf("%w: unknown recovery outcome %q", ErrRecoveryRequired, outcome.Status)
	}
}

func (h *Harness) reconcileTerminal(ctx context.Context, execution storage.TaskExecution, plan knowledge.ExecutionPlan, request runtimeboundary.RecoveryRequest, outcome runtimeboundary.RecoveryOutcome) (RunResult, error) {
	if outcome.NativeRunID == "" || outcome.NativeEventID == "" || outcome.NativeVersion == "" || outcome.SourceCursor == "" || outcome.ObservationScope == "" || outcome.TerminalStatus == "" {
		return RunResult{}, ErrRecoveryRequired
	}
	sourceSequence := outcome.NativeSequence
	if sourceSequence == nil && outcome.SourceSequence > 0 {
		value := outcome.SourceSequence
		sourceSequence = &value
	}
	evidence, err := h.Repository.ProjectNativeEvidence(ctx, storage.ExecutionEvidenceRecord{ExecutionID: execution.ID, EffectKey: execution.ID + ":recovery:" + outcome.NativeEventID, ParentRunID: execution.ParentRunID, PlanVersion: plan.Version, CapabilityDigest: plan.CapabilityDigest, ProjectionDigest: plan.ProjectionDigest, ContractVersion: storage.NativeEvidenceContractV1, NativeSourceKind: request.SourceKind, NativeSourceNamespace: request.SourceNamespace, NativeSourceID: outcome.NativeRunID, NativeEventID: outcome.NativeEventID, NativeVersion: outcome.NativeVersion, NativeSequence: sourceSequence, ObservationScope: outcome.ObservationScope, Status: []byte(outcome.TerminalStatus), FailureClass: []byte(outcome.FailureClass), Result: []byte(outcome.Result)}, storage.ExecutionSourceCursor{ExecutionID: execution.ID, SourceKind: request.SourceKind, SourceNamespace: request.SourceNamespace, SourceID: outcome.NativeRunID, CursorVersion: runtimeboundary.ExecutionContractV1, CursorValue: outcome.SourceCursor, SourceSequence: sourceSequenceValue(sourceSequence), ContractVersion: storage.NativeEvidenceContractV1})
	if err != nil {
		if errors.Is(err, storage.ErrReplayGap) || errors.Is(err, storage.ErrSourceOrderConflict) {
			return RunResult{}, fmt.Errorf("%w: %v", ErrRecoveryGap, err)
		}
		return RunResult{}, fmt.Errorf("%w: project source evidence: %v", ErrRecoveryRequired, err)
	}
	status := storage.ExecutionSucceeded
	if strings.ToLower(outcome.TerminalStatus) != "succeeded" && strings.ToLower(outcome.TerminalStatus) != "completed" {
		status = storage.ExecutionFailed
	}
	result := []byte(outcome.Result)
	stored, err := h.Repository.CommitExecutionIfTaskRevisionCurrent(ctx, execution.ID, execution.Revision, execution.TaskRevision, status, result, []byte(outcome.FailureClass))
	if err != nil {
		return RunResult{}, err
	}
	return RunResult{Execution: stored, Plan: plan, Outcome: knowledge.ExecutionOutcome{Status: outcome.TerminalStatus, Result: string(evidence.Result), FailureClass: outcome.FailureClass, NativeRunID: outcome.NativeRunID}}, nil
}

func sourceSequenceValue(sequence *int64) int64 {
	if sequence == nil {
		return 0
	}
	return *sequence
}

// Cancel submits a source-owned cancellation request. Acceptance is recorded
// as product evidence, but it never advances the application execution to a
// terminal state; that transition requires later native terminal evidence.
func (h *Harness) Cancel(ctx context.Context, executionID, nativeRunID, idempotencyKey string) (runtimeboundary.ControlOutcome, error) {
	return h.control(ctx, executionID, nativeRunID, "cancel", idempotencyKey)
}

// Timeout routes an application timeout through the same verified native
// control boundary while preserving timeout as the product-level reason.
func (h *Harness) Timeout(ctx context.Context, executionID, nativeRunID, idempotencyKey string) (runtimeboundary.ControlOutcome, error) {
	return h.control(ctx, executionID, nativeRunID, "timeout", idempotencyKey)
}

func (h *Harness) control(ctx context.Context, executionID, nativeRunID, action, idempotencyKey string) (runtimeboundary.ControlOutcome, error) {
	if h == nil || h.Repository == nil || h.Control == nil || executionID == "" || nativeRunID == "" || idempotencyKey == "" {
		return runtimeboundary.ControlOutcome{}, ErrInvalidHarnessRequest
	}
	execution, err := h.Repository.GetExecution(ctx, executionID)
	if err != nil {
		return runtimeboundary.ControlOutcome{}, err
	}
	request := runtimeboundary.ControlRequest{Version: runtimeboundary.ExecutionContractV1, ExecutionID: executionID, NativeRunID: nativeRunID, SessionID: execution.CorrelationID, Action: action, IdempotencyKey: idempotencyKey}
	if err := request.Validate(); err != nil {
		return runtimeboundary.ControlOutcome{}, fmt.Errorf("%w: invalid control request: %v", ErrInvalidHarnessRequest, err)
	}
	effectKey := executionID + ":control:" + action + ":" + idempotencyKey
	if existing, found, err := h.evidenceByEffect(ctx, executionID, effectKey); err != nil {
		return runtimeboundary.ControlOutcome{}, err
	} else if found {
		status := string(existing.Status)
		if status == "control_accepted" {
			status = "accepted"
		}
		return runtimeboundary.ControlOutcome{Status: status, TerminalObserved: false}, nil
	}
	outcome, err := h.Control.Control(ctx, request)
	if err != nil {
		return runtimeboundary.ControlOutcome{}, err
	}
	if strings.TrimSpace(outcome.Status) == "" {
		return runtimeboundary.ControlOutcome{}, ErrInvalidHarnessRequest
	}
	diagnostic, _ := json.Marshal(struct {
		Action      string `json:"action"`
		NativeRunID string `json:"native_run_id"`
		Idempotency string `json:"idempotency_key"`
	}{action, nativeRunID, idempotencyKey})
	if _, err := h.Repository.RecordEvidence(ctx, storage.ExecutionEvidenceRecord{ExecutionID: executionID, EffectKey: effectKey, ParentRunID: nativeRunID, PlanVersion: execution.PlanVersion, CapabilityDigest: execution.CapabilityDigest, ProjectionDigest: execution.ProjectionDigest, Status: []byte("control_accepted"), Diagnostic: diagnostic}); err != nil {
		return runtimeboundary.ControlOutcome{}, err
	}
	return outcome, nil
}

// Retry starts a new native run causally linked to the supplied terminal run.
// The product execution remains the same logical execution and its previous
// terminal evidence is never overwritten.
func (h *Harness) Retry(ctx context.Context, executionID, causationID, input, idempotencyKey string) (RunResult, error) {
	if h == nil || h.Repository == nil || h.RetryAdapter == nil || executionID == "" || causationID == "" || idempotencyKey == "" {
		return RunResult{}, ErrInvalidHarnessRequest
	}
	execution, err := h.Repository.GetExecution(ctx, executionID)
	if err != nil {
		return RunResult{}, err
	}
	effectKey := executionID + ":retry:" + idempotencyKey
	if existing, found, err := h.evidenceByEffect(ctx, executionID, effectKey); err != nil {
		return RunResult{}, err
	} else if found {
		plan, planErr := h.currentPlan(ctx, executionID)
		if planErr != nil {
			return RunResult{}, planErr
		}
		return RunResult{Execution: execution, Plan: plan, Outcome: knowledge.ExecutionOutcome{Status: string(existing.Status), Result: string(existing.Result), FailureClass: string(existing.FailureClass)}}, nil
	}
	if execution.Status != storage.ExecutionFailed && execution.Status != storage.ExecutionRetryable && execution.Status != storage.ExecutionRecoveryRequired {
		return RunResult{}, fmt.Errorf("%w: retry from %s", ErrInvalidHarnessRequest, execution.Status)
	}
	if execution.Status == storage.ExecutionFailed {
		execution, err = h.Repository.UpdateExecution(ctx, execution.ID, execution.Revision, storage.ExecutionRetryable, nil, nil)
		if err != nil {
			return RunResult{}, err
		}
	}
	execution, err = h.Repository.UpdateExecution(ctx, execution.ID, execution.Revision, storage.ExecutionDispatching, nil, nil)
	if err != nil {
		return RunResult{}, err
	}
	execution, err = h.Repository.UpdateExecution(ctx, execution.ID, execution.Revision, storage.ExecutionRunning, nil, nil)
	if err != nil {
		return RunResult{}, err
	}
	plan, err := h.currentPlan(ctx, executionID)
	if err != nil {
		return RunResult{}, err
	}
	nativeRunID := executionID + ":retry:" + idempotencyKey
	request := runtimeboundary.RetryRequest{Version: runtimeboundary.ExecutionContractV1, ExecutionID: executionID, NativeRunID: nativeRunID, SessionID: execution.CorrelationID, CausationID: causationID, IdempotencyKey: idempotencyKey, Input: input, Authority: runtimeboundary.Authority{TaskRevision: execution.TaskRevision, PlanVersion: execution.PlanVersion, PlanDigest: execution.PlanDigest, CapabilityDigest: execution.CapabilityDigest, ProjectionDigest: execution.ProjectionDigest, WriteScope: "none", EffectNamespace: executionID + ":effects"}}
	if err := request.Validate(); err != nil {
		return RunResult{}, fmt.Errorf("%w: invalid retry request: %v", ErrInvalidHarnessRequest, err)
	}
	outcome, err := h.RetryAdapter.Retry(ctx, request)
	if err != nil {
		_, _ = h.Repository.UpdateExecution(ctx, execution.ID, execution.Revision, storage.ExecutionFailed, nil, []byte(err.Error()))
		return RunResult{}, err
	}
	if outcome.ExecutionID == "" {
		outcome.ExecutionID = executionID
	}
	if outcome.NativeRunID == "" {
		outcome.NativeRunID = nativeRunID
	}
	if outcome.CausationID == "" {
		outcome.CausationID = causationID
	}
	status := storage.ExecutionSucceeded
	if strings.ToLower(outcome.Status) != "succeeded" {
		status = storage.ExecutionFailed
	}
	resultBytes := []byte(outcome.Result)
	diagnostic, _ := json.Marshal(struct {
		CausationID string `json:"causation_id"`
	}{outcome.CausationID})
	if _, err := h.Repository.RecordEvidence(ctx, storage.ExecutionEvidenceRecord{ExecutionID: executionID, EffectKey: effectKey, ParentRunID: execution.ParentRunID, PlanVersion: execution.PlanVersion, CapabilityDigest: execution.CapabilityDigest, ProjectionDigest: execution.ProjectionDigest, NativeSourceKind: "runner", NativeSourceNamespace: "danny.baymax.runner.history.v1", NativeSourceID: outcome.NativeRunID, NativeEventID: outcome.NativeRunID + ":terminal", NativeVersion: runtimeboundary.ExecutionContractV1, Status: []byte(outcome.Status), FailureClass: []byte(outcome.FailureClass), Diagnostic: diagnostic, Result: resultBytes}); err != nil {
		return RunResult{}, err
	}
	stored, err := h.Repository.CommitExecutionIfTaskRevisionCurrent(ctx, execution.ID, execution.Revision, execution.TaskRevision, status, resultBytes, []byte(outcome.FailureClass))
	if err != nil {
		return RunResult{}, err
	}
	return RunResult{Execution: stored, Plan: plan, Outcome: knowledge.ExecutionOutcome{Status: outcome.Status, Result: outcome.Result, FailureClass: outcome.FailureClass}}, nil
}

func (h *Harness) currentPlan(ctx context.Context, executionID string) (knowledge.ExecutionPlan, error) {
	plans, err := h.Repository.Plans(ctx, executionID)
	if err != nil || len(plans) == 0 {
		if err != nil {
			return knowledge.ExecutionPlan{}, fmt.Errorf("load plan: %w", err)
		}
		return knowledge.ExecutionPlan{}, ErrInvalidHarnessRequest
	}
	return knowledgePlan(plans[len(plans)-1]), nil
}

func (h *Harness) evidenceByEffect(ctx context.Context, executionID, effectKey string) (storage.ExecutionEvidenceRecord, bool, error) {
	evidence, err := h.Repository.Evidence(ctx, executionID)
	if err != nil {
		return storage.ExecutionEvidenceRecord{}, false, err
	}
	for _, item := range evidence {
		if item.EffectKey == effectKey {
			return item, true, nil
		}
	}
	return storage.ExecutionEvidenceRecord{}, false, nil
}

func (h *Harness) Admit(ctx context.Context, req AdmissionRequest) (RunResult, error) {
	if h == nil || h.Repository == nil || h.Executor == nil || req.TaskID == "" || req.CorrelationID == "" || req.Profile.Objective == "" {
		return RunResult{}, ErrInvalidHarnessRequest
	}
	plan, err := h.Planner.PlanMode(ctx, req.Profile, req.RequestedMode)
	if err != nil {
		if errors.Is(err, knowledge.ErrUnsupportedMode) {
			return RunResult{}, UnsupportedModeError{Mode: req.RequestedMode}
		}
		return RunResult{}, err
	}
	plan.CapabilityDigest = req.Policy.CapabilityDigest()
	plan.ProjectionDigest = req.ProjectionDigest
	plan.Digest = knowledge.DigestExecutionPlan(plan)
	if req.ExecutionID == "" {
		req.ExecutionID = uuid.NewString()
	}
	if req.IdempotencyKey == "" {
		req.IdempotencyKey = req.ExecutionID
	}
	execution := storage.TaskExecution{ID: req.ExecutionID, TaskID: req.TaskID, TaskRevision: req.TaskRevision, Status: storage.ExecutionAdmitted, Mode: string(plan.Mode), PlanVersion: plan.Version, PlanDigest: plan.Digest, CapabilityDigest: plan.CapabilityDigest, ProjectionDigest: req.ProjectionDigest, ParentRunID: req.ParentRunID, CorrelationID: req.CorrelationID, IdempotencyKey: req.IdempotencyKey, Input: req.Profile.Objective}
	planRecord := planRecord(execution.ID, plan, "", "")
	planRecord.ProjectionDigest = req.ProjectionDigest
	stored, err := h.Repository.AdmitIfTaskRevisionCurrent(ctx, storage.ExecutionAdmission{Execution: execution, Plan: planRecord})
	if err != nil {
		if errors.Is(err, storage.ErrRevisionConflict) {
			return RunResult{}, fmt.Errorf("%w: %w", ErrProductRevisionConflict, err)
		}
		return RunResult{}, err
	}
	return RunResult{Execution: stored, Plan: plan}, nil
}

// AdmitScheduled binds the harness admission to the persisted occurrence
// authority. Retries must present the same policy, capability, projection,
// plan, and effect namespace values that were admitted originally.
func (h *Harness) AdmitScheduled(ctx context.Context, req ScheduledAdmissionRequest) (RunResult, error) {
	o := req.Occurrence
	if o.ScheduleID == "" || o.OccurrenceKey == "" || o.CorrelationID == "" || o.PolicyDigest == "" || o.CapabilityDigest == "" || o.PlanVersion == "" || o.ProjectionDigest == "" || o.EffectNamespace == "" {
		return RunResult{}, ErrInvalidHarnessRequest
	}
	if req.Policy.CapabilityDigest() != o.CapabilityDigest {
		return RunResult{}, fmt.Errorf("%w: scheduled capability digest mismatch", ErrRecoveryMismatch)
	}
	return h.Admit(ctx, AdmissionRequest{ExecutionID: o.ExecutionID, TaskID: req.TaskID, ParentRunID: o.CorrelationID, CorrelationID: o.CorrelationID, IdempotencyKey: o.IdempotencyKey, Profile: req.Profile, Policy: req.Policy, ProjectionDigest: o.ProjectionDigest, RequestedMode: req.RequestedMode, TaskRevision: req.TaskRevision})
}

func (h *Harness) RunScheduled(ctx context.Context, req ScheduledRunRequest) (RunResult, error) {
	admitted, err := h.AdmitScheduled(ctx, ScheduledAdmissionRequest(req))
	if err != nil {
		return RunResult{}, err
	}
	if req.Occurrence.PlanVersion != "" && admitted.Plan.Version != req.Occurrence.PlanVersion {
		return RunResult{}, fmt.Errorf("%w: planner version changed from %s to %s", ErrRecoveryMismatch, req.Occurrence.PlanVersion, admitted.Plan.Version)
	}
	return h.Execute(ctx, admitted.Execution.ID)
}

func (h *Harness) recordChildOutcome(ctx context.Context, execution storage.TaskExecution, plan knowledge.ExecutionPlan, parentID string, index int, outcome knowledge.ExecutionOutcome, dispatchErr error) error {
	childID := outcome.NativeRunID
	if childID == "" {
		childID = fmt.Sprintf("%s:child:%d", execution.ID, index+1)
	}
	status := outcome.Status
	if status == "" {
		status = "failed"
	}
	childEvidence := storage.ExecutionEvidenceRecord{ExecutionID: execution.ID, EffectKey: fmt.Sprintf("%s:child:%d", execution.ID, index+1), ParentRunID: parentID, ChildRunID: childID, PlanVersion: plan.Version, CapabilityDigest: plan.CapabilityDigest, ProjectionDigest: plan.ProjectionDigest, NativeSourceKind: "runner", NativeSourceNamespace: "danny.baymax.runner.history.v1", NativeSourceID: childID, NativeEventID: childID + ":terminal", NativeVersion: runtimeboundary.ExecutionContractV1, Status: []byte(status), Result: []byte(outcome.Result), FailureClass: []byte(outcome.FailureClass)}
	if _, err := h.Repository.RecordEvidence(ctx, childEvidence); err != nil {
		return err
	}
	if dispatchErr != nil {
		return fmt.Errorf("%w: %v", ErrCommitBlocked, dispatchErr)
	}
	if strings.ToLower(status) != "succeeded" {
		failure := outcome.FailureClass
		if failure == "" {
			failure = "child execution failed"
		}
		return fmt.Errorf("%w: %s", ErrCommitBlocked, failure)
	}
	return nil
}

func (h *Harness) Execute(ctx context.Context, executionID string) (RunResult, error) {
	if h == nil || h.Repository == nil || h.Executor == nil || executionID == "" {
		return RunResult{}, ErrInvalidHarnessRequest
	}
	execution, err := h.Repository.GetExecution(ctx, executionID)
	if err != nil {
		return RunResult{}, err
	}
	plans, err := h.Repository.Plans(ctx, executionID)
	if err != nil || len(plans) == 0 {
		if err != nil {
			return RunResult{}, fmt.Errorf("load plan: %w", err)
		}
		return RunResult{}, ErrInvalidHarnessRequest
	}
	plan := knowledgePlan(plans[len(plans)-1])
	if execution.Status == storage.ExecutionSucceeded || execution.Status == storage.ExecutionFailed || execution.Status == storage.ExecutionBlocked {
		return RunResult{Execution: execution, Plan: plan, Outcome: knowledge.ExecutionOutcome{Status: string(execution.Status), Result: string(execution.Result), FailureClass: string(execution.FailureClass)}}, nil
	}
	if plan.Mode != knowledge.ModeDirect && plan.Mode != knowledge.ModeSubagentFanout {
		return RunResult{}, UnsupportedModeError{Mode: plan.Mode}
	}
	dispatched, err := h.Repository.UpdateExecution(ctx, execution.ID, execution.Revision, storage.ExecutionDispatching, nil, nil)
	if err != nil {
		return RunResult{}, err
	}
	execution, err = h.Repository.UpdateExecution(ctx, execution.ID, dispatched.Revision, storage.ExecutionRunning, nil, nil)
	if err != nil {
		return RunResult{}, err
	}
	base := knowledge.ExecutionRequest{RunID: execution.ID, ParentRunID: execution.ParentRunID, CorrelationID: execution.CorrelationID, PlanVersion: plan.Version, PlanDigest: plan.Digest, CapabilityDigest: plan.CapabilityDigest, ProjectionDigest: plan.ProjectionDigest, Role: "coordinator", PerChildBudget: plan.PerChildBudget, AggregationLimit: plan.AggregationLimit, Input: execution.Input}
	parent, runErr := h.Executor.Run(ctx, base)
	if runErr != nil {
		status := storage.ExecutionFailed
		if errors.Is(runErr, ErrDispatchUncertain) {
			status = storage.ExecutionRecoveryRequired
		}
		_, _ = h.Repository.UpdateExecution(ctx, execution.ID, execution.Revision, status, nil, []byte(runErr.Error()))
		return RunResult{}, runErr
	}
	if parent.Status == "" {
		parent.Status = "succeeded"
	}
	parentEvidence := storage.ExecutionEvidenceRecord{ExecutionID: execution.ID, EffectKey: execution.ID + ":parent", ParentRunID: execution.ParentRunID, PlanVersion: plan.Version, CapabilityDigest: plan.CapabilityDigest, ProjectionDigest: plan.ProjectionDigest, Status: []byte(parent.Status), Result: []byte(parent.Result), FailureClass: []byte(parent.FailureClass)}
	if parent.NativeRunID != "" {
		parentEvidence.NativeSourceKind = "runner"
		parentEvidence.NativeSourceNamespace = "danny.baymax.runner.history.v1"
		parentEvidence.NativeSourceID = parent.NativeRunID
		parentEvidence.NativeEventID = parent.NativeRunID + ":terminal"
		parentEvidence.NativeVersion = runtimeboundary.ExecutionContractV1
	}
	if _, err := h.Repository.RecordEvidence(ctx, parentEvidence); err != nil {
		return RunResult{}, err
	}
	if strings.ToLower(parent.Status) != "succeeded" {
		failure := parent.FailureClass
		if failure == "" {
			failure = "parent execution failed"
		}
		_, _ = h.Repository.UpdateExecution(ctx, execution.ID, execution.Revision, storage.ExecutionFailed, []byte(parent.Result), []byte(failure))
		return RunResult{}, fmt.Errorf("%w: %s", ErrCommitBlocked, failure)
	}
	if plan.Mode == knowledge.ModeSubagentFanout {
		if batcher, ok := h.Executor.(knowledge.BoundedChildExecutor); ok {
			outcomes, batchErr := batcher.RunChildren(ctx, base, plan.ChildLimit)
			for i, outcome := range outcomes {
				if childErr := h.recordChildOutcome(ctx, execution, plan, execution.ID, i, outcome, nil); childErr != nil {
					_, _ = h.Repository.UpdateExecution(ctx, execution.ID, execution.Revision, storage.ExecutionBlocked, nil, []byte(childErr.Error()))
					return RunResult{}, childErr
				}
			}
			if batchErr != nil {
				_, _ = h.Repository.UpdateExecution(ctx, execution.ID, execution.Revision, storage.ExecutionBlocked, nil, []byte(batchErr.Error()))
				return RunResult{}, fmt.Errorf("%w: %s", ErrCommitBlocked, batchErr)
			}
		} else {
			for i := 0; i < plan.ChildLimit; i++ {
				child := base
				child.RunID = fmt.Sprintf("%s:child:%d", execution.ID, i+1)
				child.ParentRunID = execution.ID
				child.Role = "subagent"
				outcome, childErr := h.Executor.RunChild(ctx, child)
				if childErr := h.recordChildOutcome(ctx, execution, plan, execution.ID, i, outcome, childErr); childErr != nil {
					_, _ = h.Repository.UpdateExecution(ctx, execution.ID, execution.Revision, storage.ExecutionBlocked, nil, []byte(childErr.Error()))
					return RunResult{}, childErr
				}
			}
		}
	}
	result, _ := json.Marshal(parent.Result)
	stored, err := h.Repository.CommitExecutionIfTaskRevisionCurrent(ctx, execution.ID, execution.Revision, execution.TaskRevision, storage.ExecutionSucceeded, result, []byte(parent.FailureClass))
	if err != nil {
		if errors.Is(err, storage.ErrRevisionConflict) {
			if current, getErr := h.Repository.GetExecution(ctx, execution.ID); getErr == nil && current.Status == storage.ExecutionRunning {
				_, _ = h.Repository.UpdateExecution(ctx, execution.ID, current.Revision, storage.ExecutionBlocked, nil, []byte(ErrProductRevisionConflict.Error()))
			}
			return RunResult{}, fmt.Errorf("%w: %w", ErrProductRevisionConflict, err)
		}
		return RunResult{}, err
	}
	return RunResult{Execution: stored, Plan: plan, Outcome: parent}, nil
}

func (h *Harness) Pause(ctx context.Context, executionID string, reason knowledge.PauseReason, currentStep string, completed []string) error {
	execution, err := h.Repository.GetExecution(ctx, executionID)
	if err != nil {
		return err
	}
	if reason != knowledge.PauseInputRequired && reason != knowledge.PauseApprovalRequired && reason != knowledge.PauseBudgetExhausted {
		return ErrInvalidHarnessRequest
	}
	completedJSON, _ := json.Marshal(completed)
	checkpoint := storage.ExecutionCheckpoint{ExecutionID: execution.ID, RunID: execution.ID, PlanVersion: execution.PlanVersion, PlanDigest: execution.PlanDigest, CapabilityDigest: execution.CapabilityDigest, ProjectionDigest: execution.ProjectionDigest, CurrentStep: currentStep, PauseReason: []byte(reason), CompletedEffectKeys: completedJSON}
	if err := h.Repository.SaveCheckpoint(ctx, checkpoint); err != nil {
		return err
	}
	_, err = h.Repository.UpdateExecution(ctx, execution.ID, execution.Revision, storage.ExecutionPaused, nil, nil)
	return err
}

// PauseNative binds a source-owned checkpoint to the product authority record
// and pauses the execution in one storage transaction. The native adapter must
// provide the checkpoint identity and authority digests; the harness does not
// synthesize a checkpoint or infer missing source facts.
func (h *Harness) PauseNative(ctx context.Context, binding runtimeboundary.CheckpointBinding, currentStep string, completed []string) error {
	if h == nil || h.Repository == nil {
		return ErrInvalidHarnessRequest
	}
	if err := binding.Validate(); err != nil {
		return fmt.Errorf("%w: invalid native checkpoint binding: %v", ErrRecoveryMismatch, err)
	}
	execution, err := h.Repository.GetExecution(ctx, binding.ExecutionID)
	if err != nil {
		return err
	}
	if execution.TaskRevision != binding.Authority.TaskRevision || execution.PlanVersion != binding.Authority.PlanVersion || execution.PlanDigest != binding.Authority.PlanDigest || execution.CapabilityDigest != binding.Authority.CapabilityDigest || execution.ProjectionDigest != binding.Authority.ProjectionDigest {
		return fmt.Errorf("%w: native checkpoint authority does not match execution", ErrRecoveryMismatch)
	}
	completedJSON, err := json.Marshal(completed)
	if err != nil {
		return err
	}
	sourceKind, sourceNamespace, sourceID := "runner", binding.OwnerNamespace, binding.NativeID
	if sourceNamespace == "" {
		sourceNamespace = "danny.baymax.runner.history.v1"
	}
	diagnostics, err := json.Marshal(struct {
		DecisionToken string `json:"decision_token,omitempty"`
	}{DecisionToken: binding.DecisionToken})
	if err != nil {
		return err
	}
	checkpoint := storage.ExecutionCheckpoint{ExecutionID: execution.ID, RunID: binding.NativeID, PlanVersion: binding.Authority.PlanVersion, PlanDigest: binding.Authority.PlanDigest, CapabilityDigest: binding.Authority.CapabilityDigest, ProjectionDigest: binding.Authority.ProjectionDigest, CurrentStep: currentStep, PauseReason: []byte(binding.PauseReason), CompletedEffectKeys: completedJSON, Diagnostics: diagnostics, ContractVersion: storage.NativeCheckpointContract, TaskRevision: binding.Authority.TaskRevision, WriteScope: binding.Authority.WriteScope, EffectNamespace: binding.Authority.EffectNamespace, NativeCheckpointID: binding.NativeID, NativeCheckpointVersion: binding.NativeVersion, NativeCheckpointDigest: binding.NativeDigest, SourceKind: sourceKind, SourceNamespace: sourceNamespace, SourceID: sourceID, SourceCursor: binding.SourceCursor}
	authority := storage.ExecutionAuthorityBinding{ExecutionID: execution.ID, ContractVersion: storage.NativeCheckpointContract, TaskRevision: binding.Authority.TaskRevision, PlanVersion: binding.Authority.PlanVersion, PlanDigest: binding.Authority.PlanDigest, CapabilityDigest: binding.Authority.CapabilityDigest, ProjectionDigest: binding.Authority.ProjectionDigest, WriteScope: binding.Authority.WriteScope, EffectNamespace: binding.Authority.EffectNamespace, CompletedEffectKeys: completedJSON, NativeCheckpointID: binding.NativeID, NativeCheckpointVersion: binding.NativeVersion, NativeCheckpointDigest: binding.NativeDigest, SourceCursor: binding.SourceCursor}
	_, err = h.Repository.SaveNativeCheckpointAndPause(ctx, checkpoint, authority, execution.Revision)
	return err
}

func (h *Harness) Resume(ctx context.Context, executionID string, request knowledge.ResumeRequest) (RunResult, error) {
	execution, err := h.Repository.GetExecution(ctx, executionID)
	if err != nil {
		return RunResult{}, err
	}
	checkpoint, err := h.Repository.GetCheckpoint(ctx, executionID)
	if err != nil {
		return RunResult{}, err
	}
	if checkpoint.ContractVersion == storage.NativeCheckpointContract {
		if h.Continuation == nil {
			return RunResult{}, fmt.Errorf("%w: native restore adapter is required", ErrRecoveryMismatch)
		}
		if execution.TaskRevision != checkpoint.TaskRevision || execution.PlanVersion != checkpoint.PlanVersion || execution.PlanDigest != checkpoint.PlanDigest || execution.CapabilityDigest != checkpoint.CapabilityDigest || execution.ProjectionDigest != checkpoint.ProjectionDigest {
			return RunResult{}, fmt.Errorf("%w: native checkpoint authority does not match execution", ErrRecoveryMismatch)
		}
		if request.PlanVersion != "" && request.PlanVersion != checkpoint.PlanVersion || request.PlanDigest != "" && request.PlanDigest != checkpoint.PlanDigest || request.CapabilityDigest != "" && request.CapabilityDigest != checkpoint.CapabilityDigest || request.ProjectionDigest != "" && request.ProjectionDigest != checkpoint.ProjectionDigest || request.CurrentStep != "" && request.CurrentStep != checkpoint.CurrentStep {
			return RunResult{}, fmt.Errorf("%w: resume request authority does not match checkpoint", ErrRecoveryMismatch)
		}
		var metadata struct {
			DecisionToken string `json:"decision_token"`
		}
		if len(checkpoint.Diagnostics) == 0 || json.Unmarshal(checkpoint.Diagnostics, &metadata) != nil || strings.TrimSpace(metadata.DecisionToken) == "" {
			return RunResult{}, fmt.Errorf("%w: native decision token is unavailable", ErrRecoveryMismatch)
		}
		if request.DecisionToken != "" && request.DecisionToken != metadata.DecisionToken {
			return RunResult{}, fmt.Errorf("%w: native decision token mismatch", ErrRecoveryMismatch)
		}
		idempotencyKey := request.IdempotencyKey
		if idempotencyKey == "" {
			idempotencyKey = execution.ID + ":resume:" + checkpoint.NativeCheckpointID
		}
		nativeRequest := runtimeboundary.NativeResumeRequest{Checkpoint: runtimeboundary.CheckpointReference{Version: checkpoint.NativeCheckpointVersion, CheckpointID: checkpoint.NativeCheckpointID, Digest: checkpoint.NativeCheckpointDigest, SourceComponent: checkpoint.SourceKind, OwnerNamespace: checkpoint.SourceNamespace, RunID: checkpoint.RunID, SessionID: execution.CorrelationID, PauseReason: string(checkpoint.PauseReason), SourcePosition: checkpoint.SourceCursor, AuthorityDigest: checkpoint.CapabilityDigest, PlanVersion: checkpoint.PlanVersion, PlanDigest: checkpoint.PlanDigest, CapabilityDigest: checkpoint.CapabilityDigest, ProjectionDigest: checkpoint.ProjectionDigest, WriteScopeDigest: checkpoint.WriteScope, EffectNamespace: checkpoint.EffectNamespace, PendingActionToken: metadata.DecisionToken}, DecisionToken: metadata.DecisionToken, IdempotencyKey: idempotencyKey, Authority: runtimeboundary.AuthorityBinding{TaskRevision: checkpoint.TaskRevision, PlanVersion: checkpoint.PlanVersion, PlanDigest: checkpoint.PlanDigest, CapabilityDigest: checkpoint.CapabilityDigest, ProjectionDigest: checkpoint.ProjectionDigest, WriteScopeDigest: checkpoint.WriteScope, EffectNamespace: checkpoint.EffectNamespace}, ProposedBudget: request.ProposedBudget}
		outcome, resumeErr := h.Continuation.ResumeNative(ctx, nativeRequest)
		if resumeErr != nil {
			return RunResult{}, resumeErr
		}
		if outcome.Status == "paused" && outcome.Checkpoint != nil {
			binding := runtimeboundary.CheckpointBinding{Version: runtimeboundary.ExecutionContractV1, ExecutionID: execution.ID, NativeID: outcome.Checkpoint.CheckpointID, NativeVersion: outcome.Checkpoint.Version, NativeDigest: outcome.Checkpoint.Digest, SourceCursor: outcome.Checkpoint.SourcePosition, DecisionToken: outcome.PendingActionToken, OwnerNamespace: outcome.Checkpoint.OwnerNamespace, PauseReason: outcome.Checkpoint.PauseReason, Authority: runtimeboundary.Authority{TaskRevision: checkpoint.TaskRevision, PlanVersion: checkpoint.PlanVersion, PlanDigest: checkpoint.PlanDigest, CapabilityDigest: checkpoint.CapabilityDigest, ProjectionDigest: checkpoint.ProjectionDigest, WriteScope: checkpoint.WriteScope, EffectNamespace: checkpoint.EffectNamespace}}
			if pauseErr := h.PauseNative(ctx, binding, checkpoint.CurrentStep, nil); pauseErr != nil {
				return RunResult{}, pauseErr
			}
			return RunResult{Execution: execution, Outcome: knowledge.ExecutionOutcome{Status: "paused"}}, nil
		}
		if _, updateErr := h.Repository.UpdateExecution(ctx, execution.ID, execution.Revision, storage.ExecutionRunning, nil, nil); updateErr != nil {
			return RunResult{}, updateErr
		}
		status := storage.ExecutionSucceeded
		if strings.ToLower(outcome.Status) != "succeeded" {
			status = storage.ExecutionFailed
		}
		result, _ := json.Marshal(outcome.Result)
		if _, evidenceErr := h.Repository.RecordEvidence(ctx, storage.ExecutionEvidenceRecord{ExecutionID: execution.ID, EffectKey: execution.ID + ":resume:" + checkpoint.NativeCheckpointID, ParentRunID: execution.ParentRunID, PlanVersion: checkpoint.PlanVersion, CapabilityDigest: checkpoint.CapabilityDigest, ProjectionDigest: checkpoint.ProjectionDigest, Status: []byte(outcome.Status), FailureClass: []byte(outcome.FailureClass), Result: result}); evidenceErr != nil {
			return RunResult{}, evidenceErr
		}
		stored, commitErr := h.Repository.CommitExecutionIfTaskRevisionCurrent(ctx, execution.ID, execution.Revision+1, execution.TaskRevision, status, result, []byte(outcome.FailureClass))
		if commitErr != nil {
			return RunResult{}, commitErr
		}
		plans, plansErr := h.Repository.Plans(ctx, execution.ID)
		if plansErr != nil || len(plans) == 0 {
			return RunResult{}, fmt.Errorf("load plan: %w", plansErr)
		}
		return RunResult{Execution: stored, Plan: knowledgePlan(plans[len(plans)-1]), Outcome: knowledge.ExecutionOutcome{Status: outcome.Status, Result: outcome.Result, FailureClass: outcome.FailureClass}}, nil
	}
	if checkpoint.NativeCheckpointID != "" || checkpoint.NativeCheckpointVersion != "" || checkpoint.NativeCheckpointDigest != "" || checkpoint.ContractVersion != storage.LegacyCheckpointContract {
		return RunResult{}, fmt.Errorf("%w: checkpoint binding is not a verified native restore", ErrRecoveryMismatch)
	}
	return RunResult{}, fmt.Errorf("%w: application-only checkpoint cannot resume", ErrRecoveryMismatch)
}

func (h *Harness) Replan(ctx context.Context, executionID string, profile knowledge.TaskProfile, reason string) (knowledge.ExecutionPlan, error) {
	return h.replan(ctx, executionID, profile, reason, nil, "")
}

// ReplanWithProjection records a new authorized context projection binding
// alongside the new plan. The caller must persist and authorize the projection
// before invoking this method; the harness only binds its digest to product
// execution state.
func (h *Harness) ReplanWithProjection(ctx context.Context, executionID string, profile knowledge.TaskProfile, projectionDigest, reason string) (knowledge.ExecutionPlan, error) {
	if strings.TrimSpace(projectionDigest) == "" {
		return knowledge.ExecutionPlan{}, ErrInvalidHarnessRequest
	}
	return h.replan(ctx, executionID, profile, reason, nil, projectionDigest)
}

func (h *Harness) replan(ctx context.Context, executionID string, profile knowledge.TaskProfile, reason string, taskRevision *int64, projectionDigest string) (knowledge.ExecutionPlan, error) {
	execution, err := h.Repository.GetExecution(ctx, executionID)
	if err != nil {
		return knowledge.ExecutionPlan{}, err
	}
	plans, err := h.Repository.Plans(ctx, executionID)
	if err != nil || len(plans) == 0 {
		return knowledge.ExecutionPlan{}, fmt.Errorf("load plan: %w", err)
	}
	previous := knowledgePlan(plans[len(plans)-1])
	next, err := h.Planner.Plan(ctx, profile)
	if err != nil {
		return knowledge.ExecutionPlan{}, err
	}
	replans, err := h.Repository.Replans(ctx, executionID)
	if err != nil {
		return knowledge.ExecutionPlan{}, err
	}
	next.Version = fmt.Sprintf("%s.replan.%d", previous.Version, len(replans)+1)
	next.CapabilityDigest = previous.CapabilityDigest
	if projectionDigest == "" {
		projectionDigest = execution.ProjectionDigest
	}
	next.ProjectionDigest = projectionDigest
	next.ProjectionScope = projectionDigest
	next.Digest = knowledge.DigestExecutionPlan(next)
	comparison := knowledge.CompareAuthority(previous, next)
	record := planRecord(execution.ID, next, previous.Version, reason)
	record.ProjectionDigest = projectionDigest
	record.AuthorityExpanded = comparison.Expanded()
	record.CompletedEffectKeys = append([]byte(nil), plans[len(plans)-1].CompletedEffectKeys...)
	if err := h.Repository.AppendPlan(ctx, record); err != nil {
		return knowledge.ExecutionPlan{}, err
	}
	if err := h.Repository.RecordReplan(ctx, storage.ExecutionReplan{ExecutionID: execution.ID, PreviousPlanVersion: previous.Version, NewPlanVersion: next.Version, Reason: reason, PreviousProfileDigest: previous.ProfileDigest, NewProfileDigest: next.ProfileDigest, AuthorityExpanded: comparison.Expanded()}); err != nil {
		return knowledge.ExecutionPlan{}, err
	}
	status := storage.ExecutionReplanning
	if comparison.Expanded() {
		status = storage.ExecutionPaused
	}
	if _, err := h.Repository.UpdateExecutionPlan(ctx, execution.ID, execution.Revision, string(next.Mode), next.Version, next.Digest, next.CapabilityDigest, projectionDigest, profile.Objective); err != nil {
		return knowledge.ExecutionPlan{}, err
	}
	updated, err := h.Repository.GetExecution(ctx, execution.ID)
	if err != nil {
		return knowledge.ExecutionPlan{}, err
	}
	if taskRevision != nil {
		updated, err = h.Repository.BindExecutionTaskRevision(ctx, executionID, updated.Revision, *taskRevision)
		if err != nil {
			return knowledge.ExecutionPlan{}, err
		}
	}
	_, err = h.Repository.UpdateExecution(ctx, execution.ID, updated.Revision, status, nil, nil)
	return next, err
}

// EnsureTaskRevision turns a task revision change into an explicit replan. A
// caller that observes the same revision receives the current plan unchanged.
func (h *Harness) EnsureTaskRevision(ctx context.Context, executionID string, currentRevision int64, profile knowledge.TaskProfile, reason string) (knowledge.ExecutionPlan, error) {
	execution, err := h.Repository.GetExecution(ctx, executionID)
	if err != nil {
		return knowledge.ExecutionPlan{}, err
	}
	plans, err := h.Repository.Plans(ctx, executionID)
	if err != nil || len(plans) == 0 {
		if err != nil {
			return knowledge.ExecutionPlan{}, err
		}
		return knowledge.ExecutionPlan{}, ErrInvalidHarnessRequest
	}
	if currentRevision == execution.TaskRevision {
		return knowledgePlan(plans[len(plans)-1]), nil
	}
	return h.replan(ctx, executionID, profile, reason, &currentRevision, "")
}

func planRecord(executionID string, plan knowledge.ExecutionPlan, parent, reason string) storage.ExecutionPlanRecord {
	return storage.ExecutionPlanRecord{ExecutionID: executionID, Version: plan.Version, ParentVersion: parent, Mode: string(plan.Mode), ProfileDigest: plan.ProfileDigest, PlanDigest: plan.Digest, CapabilityDigest: plan.CapabilityDigest, ProjectionDigest: plan.ProjectionScope, Rationale: plan.Rationale, ReplanReason: reason, Budget: plan.Budget, WriteScope: plan.WriteScope, ChildLimit: plan.ChildLimit, PerChildBudget: plan.PerChildBudget, AggregationLimit: plan.AggregationLimit, CompletedEffectKeys: []byte("[]"), Diagnostics: []byte("[]")}
}

func knowledgePlan(record storage.ExecutionPlanRecord) knowledge.ExecutionPlan {
	return knowledge.ExecutionPlan{Version: record.Version, Digest: record.PlanDigest, Mode: knowledge.ExecutionMode(record.Mode), ProfileDigest: record.ProfileDigest, CapabilityDigest: record.CapabilityDigest, ProjectionDigest: record.ProjectionDigest, ProjectionScope: record.ProjectionDigest, Budget: record.Budget, WriteScope: record.WriteScope, ChildLimit: record.ChildLimit, PerChildBudget: record.PerChildBudget, AggregationLimit: record.AggregationLimit, Rationale: record.Rationale}
}
