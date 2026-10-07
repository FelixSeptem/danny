package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/FelixSeptem/danny/internal/agent/knowledge"
	"github.com/FelixSeptem/danny/internal/storage"
	"github.com/google/uuid"
)

var (
	ErrInvalidHarnessRequest = errors.New("tasks: invalid harness request")
	ErrRecoveryMismatch      = errors.New("tasks: checkpoint recovery mismatch")
	ErrCommitBlocked         = errors.New("tasks: commit gate blocked")
	ErrDispatchUncertain     = errors.New("tasks: dispatch outcome is uncertain")
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
	Profile       knowledge.TaskProfile
	Policy        knowledge.Policy
	RequestedMode knowledge.ExecutionMode
}

type ScheduledRunRequest struct {
	Occurrence    storage.ScheduleOccurrence
	TaskID        string
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
	Repository storage.ExecutionRepository
	Planner    knowledge.Planner
	Executor   knowledge.Executor
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
	stored, err := h.Repository.Admit(ctx, storage.ExecutionAdmission{Execution: execution, Plan: planRecord})
	if err != nil {
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
	return h.Admit(ctx, AdmissionRequest{ExecutionID: o.ExecutionID, TaskID: req.TaskID, ParentRunID: o.CorrelationID, CorrelationID: o.CorrelationID, IdempotencyKey: o.IdempotencyKey, Profile: req.Profile, Policy: req.Policy, ProjectionDigest: o.ProjectionDigest, RequestedMode: req.RequestedMode, TaskRevision: o.ScheduleRevision})
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
	base := knowledge.ExecutionRequest{RunID: execution.ID, ParentRunID: execution.ParentRunID, CorrelationID: execution.CorrelationID, PlanVersion: plan.Version, PlanDigest: plan.Digest, CapabilityDigest: plan.CapabilityDigest, ProjectionDigest: plan.ProjectionDigest, Input: execution.Input}
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
	if _, err := h.Repository.RecordEvidence(ctx, storage.ExecutionEvidenceRecord{ExecutionID: execution.ID, EffectKey: execution.ID + ":parent", ParentRunID: execution.ParentRunID, PlanVersion: plan.Version, CapabilityDigest: plan.CapabilityDigest, ProjectionDigest: plan.ProjectionDigest, Status: []byte(parent.Status), Result: []byte(parent.Result), FailureClass: []byte(parent.FailureClass)}); err != nil {
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
		for i := 0; i < plan.ChildLimit; i++ {
			child := base
			child.RunID = fmt.Sprintf("%s:child:%d", execution.ID, i+1)
			child.ParentRunID = execution.ID
			outcome, childErr := h.Executor.RunChild(ctx, child)
			if childErr != nil || strings.ToLower(outcome.Status) != "succeeded" {
				failure := "child execution failed"
				if childErr != nil {
					failure = childErr.Error()
				}
				_, _ = h.Repository.UpdateExecution(ctx, execution.ID, execution.Revision, storage.ExecutionBlocked, nil, []byte(failure))
				return RunResult{}, fmt.Errorf("%w: %s", ErrCommitBlocked, failure)
			}
			if _, err := h.Repository.RecordEvidence(ctx, storage.ExecutionEvidenceRecord{ExecutionID: execution.ID, EffectKey: fmt.Sprintf("%s:child:%d", execution.ID, i+1), ParentRunID: execution.ID, ChildRunID: child.RunID, PlanVersion: plan.Version, CapabilityDigest: plan.CapabilityDigest, ProjectionDigest: plan.ProjectionDigest, Status: []byte(outcome.Status), Result: []byte(outcome.Result)}); err != nil {
				return RunResult{}, err
			}
		}
	}
	result, _ := json.Marshal(parent.Result)
	stored, err := h.Repository.UpdateExecution(ctx, execution.ID, execution.Revision, storage.ExecutionSucceeded, result, []byte(parent.FailureClass))
	if err != nil {
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

func (h *Harness) Resume(ctx context.Context, executionID string, request knowledge.ResumeRequest) (RunResult, error) {
	execution, err := h.Repository.GetExecution(ctx, executionID)
	if err != nil {
		return RunResult{}, err
	}
	checkpoint, err := h.Repository.GetCheckpoint(ctx, executionID)
	if err != nil {
		return RunResult{}, err
	}
	checkpointModel := knowledge.Checkpoint{RunID: checkpoint.RunID, PlanVersion: checkpoint.PlanVersion, PlanDigest: checkpoint.PlanDigest, CapabilityDigest: checkpoint.CapabilityDigest, ProjectionDigest: checkpoint.ProjectionDigest, CurrentStep: checkpoint.CurrentStep}
	if err := checkpointModel.ValidateResume(request); err != nil {
		return RunResult{}, fmt.Errorf("%w: %v", ErrRecoveryMismatch, err)
	}
	if _, err := h.Repository.UpdateExecution(ctx, execution.ID, execution.Revision, storage.ExecutionRunning, nil, nil); err != nil {
		return RunResult{}, err
	}
	return h.Execute(ctx, execution.ID)
}

func (h *Harness) Replan(ctx context.Context, executionID string, profile knowledge.TaskProfile, reason string) (knowledge.ExecutionPlan, error) {
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
	next.ProjectionDigest = execution.ProjectionDigest
	next.Digest = knowledge.DigestExecutionPlan(next)
	comparison := knowledge.CompareAuthority(previous, next)
	record := planRecord(execution.ID, next, previous.Version, reason)
	record.ProjectionDigest = execution.ProjectionDigest
	record.AuthorityExpanded = comparison.Expanded()
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
	if _, err := h.Repository.UpdateExecutionPlan(ctx, execution.ID, execution.Revision, string(next.Mode), next.Version, next.Digest, next.CapabilityDigest, execution.ProjectionDigest, profile.Objective); err != nil {
		return knowledge.ExecutionPlan{}, err
	}
	updated, err := h.Repository.GetExecution(ctx, execution.ID)
	if err != nil {
		return knowledge.ExecutionPlan{}, err
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
	return h.Replan(ctx, executionID, profile, reason)
}

func planRecord(executionID string, plan knowledge.ExecutionPlan, parent, reason string) storage.ExecutionPlanRecord {
	return storage.ExecutionPlanRecord{ExecutionID: executionID, Version: plan.Version, ParentVersion: parent, Mode: string(plan.Mode), ProfileDigest: plan.ProfileDigest, PlanDigest: plan.Digest, CapabilityDigest: plan.CapabilityDigest, ProjectionDigest: plan.ProjectionScope, Rationale: plan.Rationale, ReplanReason: reason, Budget: plan.Budget, WriteScope: plan.WriteScope, ChildLimit: plan.ChildLimit, PerChildBudget: plan.PerChildBudget, AggregationLimit: plan.AggregationLimit, CompletedEffectKeys: []byte("[]"), Diagnostics: []byte("[]")}
}

func knowledgePlan(record storage.ExecutionPlanRecord) knowledge.ExecutionPlan {
	return knowledge.ExecutionPlan{Version: record.Version, Digest: record.PlanDigest, Mode: knowledge.ExecutionMode(record.Mode), ProfileDigest: record.ProfileDigest, CapabilityDigest: record.CapabilityDigest, ProjectionDigest: record.ProjectionDigest, ProjectionScope: record.ProjectionDigest, Budget: record.Budget, WriteScope: record.WriteScope, ChildLimit: record.ChildLimit, PerChildBudget: record.PerChildBudget, AggregationLimit: record.AggregationLimit, Rationale: record.Rationale}
}
