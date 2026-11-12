package agentui

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// AGUIWireVersion documents the AG-UI compatibility level this renderer
// targets. Danny claims AG-UI compatibility as implemented and verified by
// the wire fixtures; it does not embed any AG-UI SDK.
const AGUIWireVersion = "ag-ui.compatible/2026-11"

// AGUIEvent is the AG-UI-compatible wire event. Field names follow the
// protocol's camelCase wire format. App-specific kinds render as Custom
// events with the documented "danny." name prefix.
type AGUIEvent struct {
	Type         string          `json:"type"`
	Timestamp    *int64          `json:"timestamp,omitempty"`
	ThreadID     string          `json:"threadId,omitempty"`
	RunID        string          `json:"runId,omitempty"`
	MessageID    string          `json:"messageId,omitempty"`
	Role         string          `json:"role,omitempty"`
	Delta        string          `json:"delta,omitempty"`
	ToolCallID   string          `json:"toolCallId,omitempty"`
	ToolCallName string          `json:"toolCallName,omitempty"`
	StepName     string          `json:"stepName,omitempty"`
	Name         string          `json:"name,omitempty"`
	Value        json.RawMessage `json:"value,omitempty"`
	Result       json.RawMessage `json:"result,omitempty"`
	Content      string          `json:"content,omitempty"`
	Message      string          `json:"message,omitempty"`
	Code         string          `json:"code,omitempty"`
	Outcome      json.RawMessage `json:"outcome,omitempty"`
	Snapshot     json.RawMessage `json:"snapshot,omitempty"`
	Messages     json.RawMessage `json:"messages,omitempty"`
}

// RenderMaps converts one projection event into its AG-UI wire events. One
// stored event may expand into several wire events (start/content/end
// triples); the renderer never fabricates lifecycle boundaries that the
// projection did not carry.
func RenderMaps(e ProjectionEvent) ([]AGUIEvent, error) {
	if err := (&e).Validate(); err != nil {
		return nil, err
	}
	base := AGUIEvent{ThreadID: e.ThreadID, RunID: e.RunID}
	switch e.Kind {
	case KindRunStarted:
		base.Type = "RunStarted"
		return []AGUIEvent{base}, nil
	case KindRunFinished:
		base.Type = "RunFinished"
		if len(e.Payload) > 0 && string(e.Payload) != "{}" {
			base.Result = json.RawMessage(e.Payload)
		}
		return []AGUIEvent{base}, nil
	case KindRunError:
		base.Type = "RunError"
		base.Message = payloadString(e.Payload, "message", "execution failed")
		base.Code = payloadString(e.Payload, "code", "")
		return []AGUIEvent{base}, nil
	case KindRunInterrupted:
		// AG-UI models a paused run as RunFinished with an interrupt outcome.
		base.Type = "RunFinished"
		outcome := map[string]any{
			"type":       "interrupt",
			"interrupts": []any{json.RawMessage(nonEmpty(e.Payload))},
		}
		raw, err := json.Marshal(outcome)
		if err != nil {
			return nil, fmt.Errorf("agentui: interrupt outcome: %w", err)
		}
		base.Outcome = raw
		return []AGUIEvent{base}, nil
	case KindStepStarted:
		base.Type = "StepStarted"
		base.StepName = payloadString(e.Payload, "step", "unnamed-step")
		return []AGUIEvent{base}, nil
	case KindStepFinished:
		base.Type = "StepFinished"
		base.StepName = payloadString(e.Payload, "step", "unnamed-step")
		return []AGUIEvent{base}, nil
	case KindTextMessage:
		return streamTriple(base, "TextMessage", payloadString(e.Payload, "message_id", e.RunID+":text"), "assistant", payloadString(e.Payload, "text", "")), nil
	case KindToolCall:
		id := payloadString(e.Payload, "tool_call_id", e.RunID+":tool")
		name := payloadString(e.Payload, "tool_name", "unknown_tool")
		events := []AGUIEvent{
			{Type: "ToolCallStart", ThreadID: base.ThreadID, RunID: base.RunID, ToolCallID: id, ToolCallName: name},
			{Type: "ToolCallArgs", ThreadID: base.ThreadID, RunID: base.RunID, ToolCallID: id, Delta: payloadString(e.Payload, "args", "{}")},
			{Type: "ToolCallEnd", ThreadID: base.ThreadID, RunID: base.RunID, ToolCallID: id},
			{Type: "ToolCallResult", ThreadID: base.ThreadID, RunID: base.RunID, MessageID: id + ":result", ToolCallID: id, Role: "tool", Content: payloadString(e.Payload, "result", "")},
		}
		return events, nil
	default:
		// App-specific kinds render as documented custom events.
		base.Type = "Custom"
		base.Name = customPrefix + string(e.Kind)
		base.Value = json.RawMessage(nonEmpty(e.Payload))
		return []AGUIEvent{base}, nil
	}
}

func streamTriple(base AGUIEvent, prefix, messageID, role, text string) []AGUIEvent {
	start := base
	start.Type = prefix + "Start"
	start.MessageID = messageID
	start.Role = role
	content := base
	content.Type = prefix + "Content"
	content.MessageID = messageID
	content.Delta = text
	end := base
	end.Type = prefix + "End"
	end.MessageID = messageID
	return []AGUIEvent{start, content, end}
}

// payloadString extracts a bounded string field from a JSON payload.
func payloadString(p Payload, key, fallback string) string {
	var m map[string]any
	if err := json.Unmarshal(p, &m); err != nil {
		return fallback
	}
	if s, ok := m[key].(string); ok && s != "" {
		return s
	}
	return fallback
}

func emptyAsObject(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || string(raw) == "{}" {
		return json.RawMessage("{}")
	}
	return raw
}

func nonEmpty(p Payload) string {
	if len(p) == 0 || string(p) == "{}" {
		return "{}"
	}
	return string(p)
}

// RefreshSnapshot is the refresh-reconciliation payload emitted on connect:
// a state snapshot derived from execution records plus the ordered message
// transcription the client should hold before replaying the tail.
type RefreshSnapshot struct {
	State    json.RawMessage `json:"state"`
	Messages json.RawMessage `json:"messages"`
}

// RenderStateSnapshot renders an AG-UI StateSnapshot wire event.
func RenderStateSnapshot(threadID, runID string, state json.RawMessage) AGUIEvent {
	return AGUIEvent{Type: "StateSnapshot", ThreadID: threadID, RunID: runID, Snapshot: emptyAsObject(state)}
}

// RenderMessagesSnapshot renders an AG-UI MessagesSnapshot wire event.
func RenderMessagesSnapshot(threadID, runID string, messages json.RawMessage) AGUIEvent {
	return AGUIEvent{Type: "MessagesSnapshot", ThreadID: threadID, RunID: runID, Messages: emptyAsObject(messages)}
}

// CustomNames returns the documented custom event names the renderer emits,
// for client contract tests.
func CustomNames() []string {
	kinds := []EventKind{KindPlannerDecision, KindCheckpoint, KindChildObservation, KindTeamObservation,
		KindWorkflowObservation, KindMailboxObservation, KindCitation, KindObservation, KindCancellationStatus, KindFailure}
	names := make([]string, 0, len(kinds))
	for _, k := range kinds {
		names = append(names, customPrefix+string(k))
	}
	sort.Strings(names)
	return names
}

// SanitizeSurfaceValue guards a surface-bound value: it must be JSON data and
// is bounded like projection payloads. Untrusted content stays data.
func SanitizeSurfaceValue(raw []byte) (json.RawMessage, error) {
	if len(raw) == 0 {
		return json.RawMessage("{}"), nil
	}
	p, err := NewPayload(raw)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(p), nil
}

// isReservedKey reports whether a map key collides with the wire envelope so
// untrusted data cannot spoof protocol fields.
func isReservedKey(key string) bool {
	switch strings.ToLower(key) {
	case "type", "name", "id", "component", "action", "function":
		return true
	}
	return false
}
