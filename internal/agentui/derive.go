package agentui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ExecutionRecord is the narrow execution view a derivation reads. Callers
// map it from the storage layer; the projection package stays independent of
// storage internals.
type ExecutionRecord struct {
	ID, TaskID, ParentRunID, CorrelationID string
	TaskRevision                           int64
	Status                                 string
	Mode, PlanVersion                      string
	Result, FailureClass                   []byte
	CreatedAt                              time.Time
}

// PlanRecord is the narrow plan-notebook view.
type PlanRecord struct {
	Version, ParentVersion, Mode, Rationale, ReplanReason string
	Budget, WriteScope                                    int
	AuthorityExpanded                                     bool
}

// ReplanRecord is the narrow replan view.
type ReplanRecord struct {
	PreviousPlanVersion, NewPlanVersion, Reason string
	AuthorityExpanded                           bool
}

// CheckpointRecord is the narrow checkpoint view.
type CheckpointRecord struct {
	ExecutionID, RunID, CurrentStep  string
	NativeCheckpointID, PlanVersion  string
	PauseReason, CompletedEffectKeys []byte
}

// EvidenceRecord is the narrow evidence view.
type EvidenceRecord struct {
	EffectKey, ParentRunID, ChildRunID       string
	PlanVersion                              string
	NativeSourceKind, NativeSourceNamespace  string
	NativeEventID                            string
	Status, FailureClass, Diagnostic, Result []byte
	ObservationPosition                      int64
}

// ExecutionRecords carries the durable execution records a derivation reads.
// Derivation is read-only: it never starts, resumes, or mutates an execution.
type ExecutionRecords struct {
	Execution  ExecutionRecord
	Plans      []PlanRecord
	Replans    []ReplanRecord
	Checkpoint *CheckpointRecord
	Evidence   []EvidenceRecord
}

// Terminal execution statuses the derivation maps to stream terminals.
const (
	statusSucceeded        = "succeeded"
	statusFailed           = "failed"
	statusBlocked          = "blocked"
	statusRecoveryRequired = "recovery_required"
	statusPaused           = "paused"
)

// DeriveExecutionEvents maps durable execution records to ordered projection
// events. Every event corresponds to a stored record or a stored status;
// missing or conflicting records fail typed instead of emitting synthetic
// lifecycle boundaries. Sequence numbers start at 1 and follow record order:
// run start, planner decisions, replans, evidence in given order, checkpoint
// interrupt, then the terminal.
func DeriveExecutionEvents(rec ExecutionRecords) ([]ProjectionEvent, error) {
	ex := rec.Execution
	if ex.ID == "" || ex.CorrelationID == "" {
		return nil, fmt.Errorf("agentui: derivation requires execution and correlation identity")
	}
	events := make([]ProjectionEvent, 0, len(rec.Plans)+len(rec.Evidence)+4)
	seq := int64(0)
	next := func(kind EventKind, terminal bool, source, eventID string, payload any, at time.Time) error {
		seq++
		raw, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("agentui: marshal %s payload: %w", kind, err)
		}
		p, err := NewPayload(raw)
		if err != nil {
			return err
		}
		events = append(events, ProjectionEvent{
			ContractVersion: ProjectionContractV1,
			ExecutionID:     ex.ID,
			ThreadID:        ex.CorrelationID,
			RunID:           ex.ID,
			ParentRunID:     ex.ParentRunID,
			Seq:             seq,
			Kind:            kind,
			Terminal:        terminal,
			NativeSource:    source,
			NativeEventID:   eventID,
			Payload:         p,
			CreatedAt:       at,
		})
		return nil
	}

	if err := next(KindRunStarted, false, "", "", map[string]any{
		"status": ex.Status, "mode": ex.Mode, "task_id": ex.TaskID, "task_revision": ex.TaskRevision, "plan_version": ex.PlanVersion,
	}, ex.CreatedAt); err != nil {
		return nil, err
	}
	for _, plan := range rec.Plans {
		if err := next(KindPlannerDecision, false, "", "", map[string]any{
			"plan_version": plan.Version, "parent_version": plan.ParentVersion, "mode": plan.Mode,
			"rationale": plan.Rationale, "replan_reason": plan.ReplanReason, "budget": plan.Budget,
			"write_scope": plan.WriteScope, "authority_expanded": plan.AuthorityExpanded,
		}, ex.CreatedAt); err != nil {
			return nil, err
		}
	}
	for _, replan := range rec.Replans {
		if err := next(KindPlannerDecision, false, "", "", map[string]any{
			"plan_version": replan.NewPlanVersion, "parent_version": replan.PreviousPlanVersion,
			"replan_reason": replan.Reason, "authority_expanded": replan.AuthorityExpanded,
		}, ex.CreatedAt); err != nil {
			return nil, err
		}
	}
	for _, ev := range rec.Evidence {
		kind := classifyEvidence(ev)
		if err := next(kind, false, ev.NativeSourceKind, ev.NativeEventID, map[string]any{
			"effect_key": ev.EffectKey, "plan_version": ev.PlanVersion, "status": jsonString(ev.Status),
			"failure_class": jsonString(ev.FailureClass), "diagnostic": jsonString(ev.Diagnostic),
			"result": jsonString(ev.Result), "child_run_id": ev.ChildRunID, "parent_run_id": ev.ParentRunID,
			"observation_position": ev.ObservationPosition,
		}, ex.CreatedAt); err != nil {
			return nil, err
		}
	}
	if rec.Checkpoint != nil {
		if rec.Checkpoint.ExecutionID != ex.ID {
			return nil, fmt.Errorf("agentui: checkpoint %q bound to execution %q, not %q", rec.Checkpoint.RunID, rec.Checkpoint.ExecutionID, ex.ID)
		}
		if err := next(KindRunInterrupted, true, "danny.checkpoint", rec.Checkpoint.NativeCheckpointID, map[string]any{
			"pause_reason": jsonString(rec.Checkpoint.PauseReason), "current_step": rec.Checkpoint.CurrentStep,
			"checkpoint_id": rec.Checkpoint.NativeCheckpointID, "plan_version": rec.Checkpoint.PlanVersion,
			"completed_effect_keys": jsonCount(rec.Checkpoint.CompletedEffectKeys),
		}, ex.CreatedAt); err != nil {
			return nil, err
		}
	}
	if err := appendTerminal(next, ex, rec.Checkpoint != nil); err != nil {
		return nil, err
	}
	return events, nil
}

// appendTerminal maps the stored execution status to the stream terminal. A
// paused status is closed by its checkpoint interrupt; a paused status
// without a checkpoint record is a typed gap, not a synthetic event.
// In-flight statuses leave the stream without a terminal.
func appendTerminal(next func(EventKind, bool, string, string, any, time.Time) error, ex ExecutionRecord, hasCheckpoint bool) error {
	at := ex.CreatedAt
	switch ex.Status {
	case statusSucceeded:
		return next(KindRunFinished, true, "", "", map[string]any{"result": jsonString(ex.Result), "status": ex.Status}, at)
	case statusFailed, statusBlocked, statusRecoveryRequired:
		return next(KindRunError, true, "", "", map[string]any{"message": jsonString(ex.FailureClass), "code": ex.Status, "status": ex.Status}, at)
	case statusPaused:
		if hasCheckpoint {
			return nil
		}
		return fmt.Errorf("agentui: execution %q is paused without a stored checkpoint record", ex.ID)
	case "admitted", "dispatching", "running", "retryable", "replanning":
		return nil
	default:
		return fmt.Errorf("agentui: unsupported execution status %q", ex.Status)
	}
}

// classifyEvidence maps an evidence record to an observation kind from its
// native source identity and child linkage. Unknown scopes stay generic
// observations; nothing is inferred from error text.
func classifyEvidence(ev EvidenceRecord) EventKind {
	marker := strings.ToLower(ev.NativeSourceKind + " " + ev.NativeSourceNamespace + " " + ev.EffectKey)
	switch {
	case ev.ChildRunID != "" || strings.Contains(marker, "child"):
		return KindChildObservation
	case strings.Contains(marker, "team"):
		return KindTeamObservation
	case strings.Contains(marker, "workflow"):
		return KindWorkflowObservation
	case strings.Contains(marker, "mailbox"):
		return KindMailboxObservation
	case strings.Contains(marker, "citation") || strings.Contains(marker, "reference"):
		return KindCitation
	default:
		return KindObservation
	}
}

// jsonString decodes stored JSON-string bytes; non-JSON bytes are wrapped as
// data strings so untrusted stored content stays data on the wire.
func jsonString(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return string(raw)
}

func jsonCount(raw []byte) int {
	var items []any
	if err := json.Unmarshal(raw, &items); err != nil {
		return 0
	}
	return len(items)
}
