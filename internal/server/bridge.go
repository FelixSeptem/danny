package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/FelixSeptem/danny/internal/agentui"
	"github.com/FelixSeptem/danny/internal/storage"
)

// StorageRunSource projects the durable execution log through the AG-UI
// renderer. It reads stored records only: replay never starts, resumes, or
// mutates an execution.
type StorageRunSource struct {
	Store *storage.Store
}

var _ RunSource = (*StorageRunSource)(nil)

// EventsAfter returns the ordered page after the cursor and the closed flag.
func (s *StorageRunSource) EventsAfter(ctx context.Context, executionID string, afterSeq int64, limit int) ([]storage.AgentUIEvent, bool, error) {
	return s.Store.AgentUI().EventsAfter(ctx, executionID, afterSeq, limit)
}

// Render converts one stored event to AG-UI wire events through the adapter.
func (s *StorageRunSource) Render(event storage.AgentUIEvent) ([]agentui.AGUIEvent, error) {
	projection := agentui.ProjectionEvent{
		ContractVersion: event.Contract,
		ExecutionID:     event.ExecutionID,
		ThreadID:        event.ExecutionID,
		RunID:           event.ExecutionID,
		Seq:             event.Seq,
		Kind:            agentui.EventKind(event.Kind),
		Terminal:        event.Terminal,
		NativeSource:    event.NativeSource,
		NativeEventID:   event.NativeEventID,
		Payload:         agentui.Payload(event.Payload),
	}
	return agentui.RenderMaps(projection)
}

// Snapshot derives the refresh-reconciliation snapshot from execution
// records. A snapshot is a projection, never new evidence.
func (s *StorageRunSource) Snapshot(ctx context.Context, executionID string) (agentui.RefreshSnapshot, error) {
	repo := s.Store.Executions()
	execution, err := repo.GetExecution(ctx, executionID)
	if err != nil {
		return agentui.RefreshSnapshot{}, fmt.Errorf("snapshot execution: %w", err)
	}
	plans, err := repo.Plans(ctx, executionID)
	if err != nil {
		return agentui.RefreshSnapshot{}, fmt.Errorf("snapshot plans: %w", err)
	}
	replans, err := repo.Replans(ctx, executionID)
	if err != nil {
		return agentui.RefreshSnapshot{}, fmt.Errorf("snapshot replans: %w", err)
	}
	checkpoint, err := repo.GetCheckpoint(ctx, executionID)
	checkpointRecord := &agentui.CheckpointRecord{
		ExecutionID:         checkpoint.ExecutionID,
		RunID:               checkpoint.RunID,
		CurrentStep:         checkpoint.CurrentStep,
		NativeCheckpointID:  checkpoint.NativeCheckpointID,
		PlanVersion:         checkpoint.PlanVersion,
		PauseReason:         checkpoint.PauseReason,
		CompletedEffectKeys: checkpoint.CompletedEffectKeys,
	}
	if errors.Is(err, storage.ErrNotFound) {
		checkpointRecord = nil
	} else if err != nil {
		return agentui.RefreshSnapshot{}, fmt.Errorf("snapshot checkpoint: %w", err)
	}
	evidence, err := repo.Evidence(ctx, executionID)
	if err != nil {
		return agentui.RefreshSnapshot{}, fmt.Errorf("snapshot evidence: %w", err)
	}

	// Derivation doubles as a consistency gate: a snapshot over records that
	// cannot derive a faithful projection (typed gap, foreign checkpoint)
	// fails instead of rendering a wrong reconciliation.
	if _, err = agentui.DeriveExecutionEvents(agentui.ExecutionRecords{
		Execution: agentui.ExecutionRecord{
			ID:            execution.ID,
			TaskID:        execution.TaskID,
			ParentRunID:   execution.ParentRunID,
			CorrelationID: execution.CorrelationID,
			TaskRevision:  execution.TaskRevision,
			Status:        string(execution.Status),
			Mode:          execution.Mode,
			PlanVersion:   execution.PlanVersion,
			Result:        execution.Result,
			FailureClass:  execution.FailureClass,
			CreatedAt:     execution.CreatedAt,
		},
		Plans:      mapPlans(plans),
		Replans:    mapReplans(replans),
		Checkpoint: checkpointRecord,
		Evidence:   mapEvidence(evidence),
	}); err != nil {
		return agentui.RefreshSnapshot{}, fmt.Errorf("snapshot derivation: %w", err)
	}

	state := map[string]any{
		"execution_id":  execution.ID,
		"status":        string(execution.Status),
		"mode":          execution.Mode,
		"task_id":       execution.TaskID,
		"task_revision": execution.TaskRevision,
		"plan_version":  execution.PlanVersion,
	}
	if checkpointRecord != nil {
		state["pause_reason"] = string(checkpointRecord.PauseReason)
		state["current_step"] = checkpointRecord.CurrentStep
	}
	stateJSON, err := json.Marshal(state)
	if err != nil {
		return agentui.RefreshSnapshot{}, err
	}
	// The message transcription comes from the durable projection log so a
	// reconnecting client rebuilds exactly what was streamed.
	logEvents, _, err := s.Store.AgentUI().EventsAfter(ctx, executionID, 0, 1000)
	if err != nil {
		return agentui.RefreshSnapshot{}, fmt.Errorf("snapshot event log: %w", err)
	}
	messages := make([]map[string]any, 0, len(logEvents))
	for _, event := range logEvents {
		if event.Kind != string(agentui.KindTextMessage) {
			continue
		}
		messages = append(messages, map[string]any{
			"id":      fmt.Sprintf("%s:%d", execution.ID, event.Seq),
			"role":    "assistant",
			"content": jsonText(agentui.Payload(event.Payload)),
		})
	}
	messagesJSON, err := json.Marshal(messages)
	if err != nil {
		return agentui.RefreshSnapshot{}, err
	}
	return agentui.RefreshSnapshot{State: stateJSON, Messages: messagesJSON}, nil
}

func mapPlans(plans []storage.ExecutionPlanRecord) []agentui.PlanRecord {
	out := make([]agentui.PlanRecord, 0, len(plans))
	for _, p := range plans {
		out = append(out, agentui.PlanRecord{
			Version: p.Version, ParentVersion: p.ParentVersion, Mode: p.Mode,
			Rationale: p.Rationale, ReplanReason: p.ReplanReason,
			Budget: p.Budget, WriteScope: p.WriteScope, AuthorityExpanded: p.AuthorityExpanded,
		})
	}
	return out
}

func mapReplans(replans []storage.ExecutionReplan) []agentui.ReplanRecord {
	out := make([]agentui.ReplanRecord, 0, len(replans))
	for _, r := range replans {
		out = append(out, agentui.ReplanRecord{
			PreviousPlanVersion: r.PreviousPlanVersion, NewPlanVersion: r.NewPlanVersion,
			Reason: r.Reason, AuthorityExpanded: r.AuthorityExpanded,
		})
	}
	return out
}

func mapEvidence(evidence []storage.ExecutionEvidenceRecord) []agentui.EvidenceRecord {
	out := make([]agentui.EvidenceRecord, 0, len(evidence))
	for _, e := range evidence {
		out = append(out, agentui.EvidenceRecord{
			EffectKey: e.EffectKey, ParentRunID: e.ParentRunID, ChildRunID: e.ChildRunID,
			PlanVersion: e.PlanVersion, NativeSourceKind: e.NativeSourceKind,
			NativeSourceNamespace: e.NativeSourceNamespace, NativeEventID: e.NativeEventID,
			Status: e.Status, FailureClass: e.FailureClass, Diagnostic: e.Diagnostic,
			Result: e.Result, ObservationPosition: e.ObservationPosition,
		})
	}
	return out
}

func jsonText(payload agentui.Payload) string {
	var m map[string]any
	if err := json.Unmarshal(payload, &m); err != nil {
		return ""
	}
	if s, ok := m["text"].(string); ok {
		return s
	}
	return ""
}

// SurfaceService is the concrete SurfaceBoundary: it validates submissions
// against the restricted catalog and current revisions, converges idempotent
// replays through the shared effect ledger, and executes accepted actions
// through the injected executor — which is wired to Danny's existing
// authorization and commit gates, never around them.
type SurfaceService struct {
	Store    *storage.Store
	Catalog  *agentui.Catalog
	Executor func(ctx context.Context, surface agentui.DynamicSurface, action agentui.SurfaceAction) ([]byte, error)
}

var _ SurfaceBoundary = (*SurfaceService)(nil)

// Submit validates and executes one submission. Typed validation failures map
// to HTTP status codes; accepted actions run inside the effect ledger so a
// replayed idempotency key returns the recorded result.
func (s *SurfaceService) Submit(ctx context.Context, submission agentui.SurfaceSubmission) (int, []byte, error) {
	surface, taskRevision, err := s.Store.AgentUI().GetSurface(ctx, submission.SurfaceID)
	if errors.Is(err, storage.ErrNotFound) {
		return 404, nil, fmt.Errorf("surface %q is unknown", submission.SurfaceID)
	}
	if err != nil {
		return 500, nil, err
	}
	if err := agentui.ValidateSubmission(s.Catalog, surface, taskRevision, submission); err != nil {
		switch {
		case errors.Is(err, agentui.ErrStaleSurface), errors.Is(err, agentui.ErrStaleTask):
			return 409, nil, err
		case errors.Is(err, agentui.ErrUnknownAction), errors.Is(err, agentui.ErrSchemaViolation),
			errors.Is(err, agentui.ErrMissingActivation), errors.Is(err, agentui.ErrMalformedSubmission):
			return 400, nil, err
		case errors.Is(err, agentui.ErrUnauthorized):
			return 403, nil, err
		default:
			return 400, nil, err
		}
	}
	action := submission.Actions[0]
	key := storage.EffectKey{Namespace: "agentui.surface-action", Subject: submission.SurfaceID, Key: submission.IdempotencyKey}
	// Converge replays before touching the executor: a completed key returns
	// the recorded result, an in-progress key is a concurrent conflict, and a
	// failed key is retried by beginning a fresh effect.
	if existing, found, err := s.Store.Effects().Lookup(ctx, key); err != nil {
		return 500, nil, err
	} else if found {
		switch existing.Status {
		case storage.EffectCompleted:
			return 200, existing.Result, nil
		case storage.EffectInProgress:
			return 409, nil, fmt.Errorf("submission %q is already in progress", submission.IdempotencyKey)
		}
	}
	record, err := s.Store.Effects().Begin(ctx, key, mustJSON(submission))
	if err != nil {
		return 500, nil, err
	}
	if record.Status == storage.EffectCompleted {
		return 200, record.Result, nil
	}
	if s.Executor == nil {
		_ = s.Store.Effects().Fail(ctx, record.ID, []byte("no executor configured"))
		return 503, nil, fmt.Errorf("surface action executor is not configured")
	}
	result, err := s.Executor(ctx, surface, action)
	if err != nil {
		_ = s.Store.Effects().Fail(ctx, record.ID, []byte(err.Error()))
		return 500, nil, fmt.Errorf("surface action %q failed: %w", action.ActionID, err)
	}
	if err := s.Store.Effects().Complete(ctx, record.ID, result); err != nil {
		return 500, nil, err
	}
	return 200, result, nil
}

func mustJSON(v any) []byte {
	raw, err := json.Marshal(v)
	if err != nil {
		return []byte("{}")
	}
	return raw
}
