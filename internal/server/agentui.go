package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/FelixSeptem/danny/internal/agentui"
	"github.com/FelixSeptem/danny/internal/storage"
)

// RunSource supplies projection events and refresh snapshots for one run.
// The SSE transport stays protocol-neutral behind this interface; a future
// WebSocket transport can share it.
type RunSource interface {
	// EventsAfter returns the ordered page after the cursor and whether the
	// log is closed by a terminal.
	EventsAfter(ctx context.Context, executionID string, afterSeq int64, limit int) ([]storage.AgentUIEvent, bool, error)
	// Snapshot derives the refresh-reconciliation snapshot from execution
	// records for a client that connects with a stale cursor.
	Snapshot(ctx context.Context, executionID string) (agentui.RefreshSnapshot, error)
	// Render converts a stored event into AG-UI wire events.
	Render(event storage.AgentUIEvent) ([]agentui.AGUIEvent, error)
}

// RunController projects cancellation status and performs cancellation
// through Danny's existing control boundary.
type RunController interface {
	Cancel(ctx context.Context, executionID, nativeRunID, idempotencyKey string) error
}

// SurfaceBoundary validates and executes surface submissions at Danny's
// authorization and idempotency boundary.
type SurfaceBoundary interface {
	Submit(ctx context.Context, submission agentui.SurfaceSubmission) (status int, body []byte, err error)
}

// AgentUIService wires the browser-facing endpoints. Zero-value fields fail
// closed: a missing dependency disables its endpoint.
type AgentUIService struct {
	Source    RunSource
	Control   RunController
	Surfaces  SurfaceBoundary
	Heartbeat time.Duration
	Poll      time.Duration
}

// Handler returns the /agent/ subtree. Routing parses path segments
// manually: the boundary must behave identically regardless of the serving
// mux's pattern-matching semantics.
func (s *AgentUIService) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		segments := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(segments) < 2 || segments[0] != "agent" {
			http.NotFound(w, r)
			return
		}
		switch {
		case segments[1] == "threads" && len(segments) == 6 && segments[3] == "runs":
			runID := segments[4]
			switch {
			case r.Method == http.MethodGet && segments[5] == "events":
				s.handleEvents(w, r, runID)
			case r.Method == http.MethodPost && segments[5] == "cancel":
				s.handleCancel(w, r, runID)
			default:
				http.NotFound(w, r)
			}
		case segments[1] == "surfaces" && len(segments) == 4 && r.Method == http.MethodPost && segments[3] == "actions":
			s.handleSubmit(w, r, segments[2])
		default:
			http.NotFound(w, r)
		}
	})
}

// handleEvents streams the AG-UI projection for one run: refresh
// reconciliation snapshot, bounded replay after the Last-Event-ID cursor,
// then live tailing with heartbeats until a terminal event.
func (s *AgentUIService) handleEvents(w http.ResponseWriter, r *http.Request, runID string) {
	if s.Source == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "unavailable", "event source is not configured")
		return
	}
	if runID == "" {
		writeJSONError(w, http.StatusBadRequest, "malformed", "run id is required")
		return
	}
	cursor := int64(0)
	if last := r.Header.Get("Last-Event-ID"); last != "" {
		value, err := strconv.ParseInt(last, 10, 64)
		if err != nil || value < 0 {
			writeJSONError(w, http.StatusBadRequest, "malformed", "Last-Event-ID must be a non-negative sequence")
			return
		}
		cursor = value
	} else if raw := r.URL.Query().Get("cursor"); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value < 0 {
			writeJSONError(w, http.StatusBadRequest, "malformed", "cursor must be a non-negative sequence")
			return
		}
		cursor = value
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSONError(w, http.StatusInternalServerError, "malformed", "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, "retry: 3000\n\n")
	flusher.Flush()

	heartbeat := s.heartbeat()
	poll := s.pollInterval()
	ctx := r.Context()
	if err := s.stream(ctx, w, flusher, runID, cursor, heartbeat, poll); err != nil && ctx.Err() == nil {
		// Transport failure is distinct from the source terminal outcome: the
		// error goes to the stream as a typed SSE comment, never as a fake
		// RunError for the run itself.
		_, _ = fmt.Fprintf(w, ": stream error %s\n\n", sanitizedLine(err.Error()))
		flusher.Flush()
	}
}

func (s *AgentUIService) stream(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, runID string, cursor int64, heartbeat, poll time.Duration) error {
	// Refresh reconciliation: a client that was offline converges through a
	// snapshot derived from execution records before replaying the tail.
	if snapshot, err := s.Source.Snapshot(ctx, runID); err == nil {
		if err := writeSSE(w, flusher, "snapshot", strconv.FormatInt(cursor, 10), snapshot); err != nil {
			return err
		}
	}
	deadline := time.Now().Add(10 * time.Minute)
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		events, closed, err := s.Source.EventsAfter(ctx, runID, cursor, 200)
		if err != nil {
			return err
		}
		for _, event := range events {
			wire, err := s.Source.Render(event)
			if err != nil {
				return err
			}
			for i := range wire {
				if err := writeSSE(w, flusher, "event", strconv.FormatInt(event.Seq, 10), wire[i]); err != nil {
					return err
				}
			}
			cursor = event.Seq
		}
		if closed {
			return nil
		}
		if time.Now().After(deadline) {
			_, _ = io.WriteString(w, ": deadline exceeded, reconnect with the last event id\n\n")
			flusher.Flush()
			return nil
		}
		if err := heartbeatWait(ctx, w, flusher, heartbeat, poll); err != nil {
			return err
		}
	}
}

func heartbeatWait(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, heartbeat, poll time.Duration) error {
	timer := time.NewTimer(heartbeat)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		_, _ = io.WriteString(w, ": heartbeat\n\n")
		flusher.Flush()
		time.Sleep(poll)
		return nil
	}
}

func (s *AgentUIService) heartbeat() time.Duration {
	if s.Heartbeat > 0 {
		return s.Heartbeat
	}
	return 15 * time.Second
}

func (s *AgentUIService) pollInterval() time.Duration {
	if s.Poll > 0 {
		return s.Poll
	}
	return 250 * time.Millisecond
}

// handleCancel admits a cancellation through Danny's control boundary and
// projects its status as a custom event in the stream.
func (s *AgentUIService) handleCancel(w http.ResponseWriter, r *http.Request, runID string) {
	if s.Control == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "unavailable", "run control is not configured")
		return
	}
	var req struct {
		IdempotencyKey string `json:"idempotency_key"`
		NativeRunID    string `json:"native_run_id"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&req); err != nil || req.IdempotencyKey == "" {
		writeJSONError(w, http.StatusBadRequest, "malformed", "idempotency_key is required")
		return
	}
	if err := s.Control.Cancel(r.Context(), runID, req.NativeRunID, req.IdempotencyKey); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, storage.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeJSONError(w, status, "cancel_failed", sanitizedLine(err.Error()))
		return
	}
	writeJSONError(w, http.StatusOK, "accepted", "cancellation admitted; watch the event stream for the status projection")
}

// handleSubmit routes a surface submission through the validation boundary.
func (s *AgentUIService) handleSubmit(w http.ResponseWriter, r *http.Request, surfaceID string) {
	if s.Surfaces == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "unavailable", "surface boundary is not configured")
		return
	}
	_ = surfaceID // the submission body binds the surface id; the path is advisory
	var submission agentui.SurfaceSubmission
	if err := json.NewDecoder(io.LimitReader(r.Body, 256<<10)).Decode(&submission); err != nil {
		writeJSONError(w, http.StatusBadRequest, "malformed", "submission is not valid JSON")
		return
	}
	status, body, err := s.Surfaces.Submit(r.Context(), submission)
	if err != nil {
		writeJSONError(w, status, "submission_rejected", sanitizedLine(err.Error()))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

var sseMu sync.Mutex

func writeSSE(w io.Writer, flusher http.Flusher, event, id string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode SSE payload: %w", err)
	}
	sseMu.Lock()
	defer sseMu.Unlock()
	if _, err := fmt.Fprintf(w, "event: %s\nid: %s\ndata: %s\n\n", sanitizedLine(event), sanitizedLine(id), raw); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

// sanitizedLine keeps SSE framing intact: newlines never terminate a field.
func sanitizedLine(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r", " "), "\n", " ")
}

func writeJSONError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code, "message": message})
}
