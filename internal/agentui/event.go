// Package agentui projects durable execution records into a browser-facing
// event stream and validates dynamic-surface actions at Danny's authorization
// boundary. The projection models are Danny-owned; AG-UI and A2UI are wire
// renderings behind adapters. Neither protocol is a source of truth for
// Danny tasks, memory, schedules, or business side effects.
package agentui

import (
	"encoding/json"
	"fmt"
	"time"
)

// ProjectionContractV1 is the current projection envelope contract version.
const ProjectionContractV1 = "danny.agent-ui.projection.v1"

// maxPayloadBytes bounds one sanitized projection payload.
const maxPayloadBytes = 64 << 10

// EventKind enumerates the typed projection kinds. Kinds map onto AG-UI wire
// events through the renderer; app-specific kinds render as documented custom
// events so unknown payloads never break the stream.
type EventKind string

const (
	KindRunStarted          EventKind = "run_started"
	KindRunFinished         EventKind = "run_finished"
	KindRunError            EventKind = "run_error"
	KindRunInterrupted      EventKind = "run_interrupted"
	KindStepStarted         EventKind = "step_started"
	KindStepFinished        EventKind = "step_finished"
	KindTextMessage         EventKind = "text_message"
	KindToolCall            EventKind = "tool_call"
	KindPlannerDecision     EventKind = "planner_decision"
	KindCheckpoint          EventKind = "checkpoint"
	KindChildObservation    EventKind = "child_observation"
	KindTeamObservation     EventKind = "team_observation"
	KindWorkflowObservation EventKind = "workflow_observation"
	KindMailboxObservation  EventKind = "mailbox_observation"
	KindCitation            EventKind = "citation"
	KindObservation         EventKind = "observation"
	KindCancellationStatus  EventKind = "cancellation_status"
	KindFailure             EventKind = "failure"
)

// customPrefix is the documented name prefix for custom-rendered kinds.
const customPrefix = "danny."

// ProjectionEvent is the Danny-owned versioned projection envelope. Native
// identity stays inside the envelope for deduplication and diagnostics; the
// wire rendering exposes only Danny correlation.
type ProjectionEvent struct {
	ContractVersion string    `json:"contract_version"`
	ExecutionID     string    `json:"execution_id"`
	ThreadID        string    `json:"thread_id"`
	RunID           string    `json:"run_id"`
	ParentRunID     string    `json:"parent_run_id,omitempty"`
	Seq             int64     `json:"seq"`
	Kind            EventKind `json:"kind"`
	Terminal        bool      `json:"terminal"`
	NativeSource    string    `json:"native_source,omitempty"`
	NativeEventID   string    `json:"native_event_id,omitempty"`
	Payload         Payload   `json:"payload"`
	CreatedAt       time.Time `json:"created_at"`
}

// Payload is sanitized, size-bounded JSON. Only structurally valid payloads
// built from stored record fields are accepted; raw blobs never pass through.
type Payload []byte

// NewPayload validates and bounds a JSON payload.
func NewPayload(raw []byte) (Payload, error) {
	if len(raw) == 0 {
		return Payload(`{}`), nil
	}
	if len(raw) > maxPayloadBytes {
		return nil, fmt.Errorf("agentui: payload exceeds %d bytes", maxPayloadBytes)
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("agentui: payload is not valid JSON: %w", err)
	}
	normalized, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("agentui: payload normalize: %w", err)
	}
	return Payload(normalized), nil
}

// Clone returns a copy safe from caller mutation.
func (p Payload) Clone() Payload {
	out := make(Payload, len(p))
	copy(out, p)
	return out
}

// Validate enforces envelope invariants before persistence.
func (e *ProjectionEvent) Validate() error {
	if e.ContractVersion != ProjectionContractV1 {
		return fmt.Errorf("agentui: unsupported projection contract %q", e.ContractVersion)
	}
	if e.ExecutionID == "" || e.RunID == "" || e.ThreadID == "" {
		return fmt.Errorf("agentui: projection event requires execution, run and thread identity")
	}
	if e.Seq <= 0 {
		return fmt.Errorf("agentui: projection sequence must be positive")
	}
	switch e.Kind {
	case KindRunStarted, KindRunFinished, KindRunError, KindRunInterrupted, KindStepStarted,
		KindStepFinished, KindTextMessage, KindToolCall, KindPlannerDecision, KindCheckpoint,
		KindChildObservation, KindTeamObservation, KindWorkflowObservation, KindMailboxObservation,
		KindCitation, KindObservation, KindCancellationStatus, KindFailure:
	default:
		return fmt.Errorf("agentui: unknown projection kind %q", e.Kind)
	}
	if e.Terminal {
		switch e.Kind {
		case KindRunFinished, KindRunError, KindRunInterrupted:
		default:
			return fmt.Errorf("agentui: kind %q cannot be terminal", e.Kind)
		}
	}
	if e.NativeSource == "" && e.NativeEventID != "" {
		return fmt.Errorf("agentui: native event id requires native source")
	}
	return nil
}
