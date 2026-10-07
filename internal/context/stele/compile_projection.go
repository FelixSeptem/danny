package stele

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/FelixSeptem/danny/internal/runtimeprobe"
	"github.com/FelixSeptem/danny/internal/storage"
)

type CompilationProjection struct {
	RunID             string   `json:"run_id"`
	GenerationID      int64    `json:"generation_id"`
	SourceIDs         []string `json:"source_ids"`
	DecisionDigest    string   `json:"decision_digest"`
	ArtifactSummaries []string `json:"artifact_summaries,omitempty"`
}

type CompilationProjectionResult struct {
	EventID  string
	Replayed bool
}

func (c *Client) ProjectCompilation(ctx context.Context, projection CompilationProjection) error {
	_, err := c.ProjectCompilationResult(ctx, projection)
	return err
}

func (c *Client) ProjectCompilationResult(ctx context.Context, projection CompilationProjection) (CompilationProjectionResult, error) {
	if projection.RunID == "" || projection.GenerationID <= 0 {
		return CompilationProjectionResult{}, fmt.Errorf("stele compilation projection requires run and generation")
	}
	content, err := json.Marshal(projection)
	if err != nil {
		return CompilationProjectionResult{}, err
	}
	idempotencyKey := fmt.Sprintf("knowledge:generation:%d", projection.GenerationID)
	payload := map[string]any{
		"metadata": map[string]any{"request_id": projection.RunID, "operation_id": "knowledge.compilation", "schema_version": "provider-v1", "idempotency_key": idempotencyKey},
		// Stele's pinned governance extractor promotes conversation.message
		// events. The operation metadata retains the Danny-specific projection
		// kind while the event follows the public memory lifecycle contract.
		"event_type":   "conversation.message",
		"memory_path":  fmt.Sprintf("knowledge/generations/%d", projection.GenerationID),
		"content":      string(content),
		"metadata_map": map[string]any{"generation_id": projection.GenerationID, "source_ids": projection.SourceIDs, "decision_digest": projection.DecisionDigest, "artifact_summaries": projection.ArtifactSummaries},
	}
	var response struct {
		EventID  string `json:"event_id"`
		Replayed bool   `json:"replayed"`
	}
	if err := c.request(ctx, http.MethodPost, "/v1/provider/events", payload, &response); err != nil {
		return CompilationProjectionResult{}, err
	}
	if response.EventID == "" {
		return CompilationProjectionResult{}, fmt.Errorf("stele compilation projection returned no event_id")
	}
	return CompilationProjectionResult{EventID: response.EventID, Replayed: response.Replayed}, nil
}

func (c *Client) ProjectOutbox(ctx context.Context, message storage.OutboxMessage) error {
	if message.Kind != storage.OutboxKindStele {
		return fmt.Errorf("stele: unsupported outbox kind %q", message.Kind)
	}
	var projection CompilationProjection
	if err := json.Unmarshal(message.Payload, &projection); err != nil {
		return err
	}
	return c.ProjectCompilation(ctx, projection)
}

func CompilationProjectionDigest(projection CompilationProjection) string {
	raw, _ := json.Marshal(projection)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// Compilation exercises the live Stele contract used by the knowledge
// compiler: reference-first projection, stable replay, and exact-path lookup.
// The provider's governance worker is asynchronous, so lookup is retried for a
// bounded interval while preserving the same request and idempotency keys.
func (c *Client) Compilation(ctx context.Context, req runtimeprobe.ProbeRequest) (runtimeprobe.ProbeOutcome, error) {
	if c == nil || strings.TrimSpace(c.BaseURL) == "" {
		return runtimeprobe.ProbeOutcome{}, fmt.Errorf("stele adapter is not configured")
	}
	if req.CorrelationID == "" || req.ParentID == "" {
		return runtimeprobe.ProbeOutcome{}, fmt.Errorf("correlation and parent ids are required")
	}
	projection := CompilationProjection{
		RunID:             req.CorrelationID,
		GenerationID:      987654321,
		SourceIDs:         []string{req.CorrelationID + ":source"},
		DecisionDigest:    "runtime-probe-compilation-digest",
		ArtifactSummaries: []string{"artifact:summary:source-section"},
	}
	first, firstErr := c.ProjectCompilationResult(ctx, projection)
	replay, replayErr := c.ProjectCompilationResult(ctx, projection)
	referencePath := fmt.Sprintf("knowledge/generations/%d", projection.GenerationID)
	expectedContent, _ := json.Marshal(projection)
	retrieved, lookupErr := c.retrieveCompilationReference(ctx, req, referencePath, string(expectedContent))
	status := runtimeprobe.StatusPassed
	if firstErr != nil || replayErr != nil || lookupErr != nil || first.EventID == "" || replay.EventID != first.EventID || !replay.Replayed || !retrieved {
		status = runtimeprobe.StatusFailedContract
	}
	now := time.Unix(0, 0).UTC()
	payload := map[string]string{
		"first_event_id":  first.EventID,
		"replay_event_id": replay.EventID,
		"first_replayed":  fmt.Sprint(first.Replayed),
		"replayed":        fmt.Sprint(replay.Replayed),
		"reference_path":  referencePath,
		"reference_found": fmt.Sprint(retrieved),
	}
	for key, err := range map[string]error{"first_error": firstErr, "replay_error": replayErr, "lookup_error": lookupErr} {
		if err != nil {
			payload[key] = err.Error()
		}
	}
	events := []runtimeprobe.Event{
		{Sequence: 1, ID: req.ParentID, Kind: runtimeprobe.KindParent, CorrelationID: req.CorrelationID, Capability: req.Capability, Status: status, At: now},
		{Sequence: 2, ID: req.ParentID + ":compilation", Kind: runtimeprobe.KindMemory, ParentID: req.ParentID, CorrelationID: req.CorrelationID, Capability: req.Capability, Status: status, At: now.Add(time.Nanosecond), Payload: payload},
		{Sequence: 3, ID: req.ParentID + ":compilation:terminal", Kind: runtimeprobe.KindTerminal, ParentID: req.ParentID + ":compilation", CorrelationID: req.CorrelationID, Capability: req.Capability, Status: status, At: now.Add(2 * time.Nanosecond)},
	}
	var err error
	for _, candidate := range []error{firstErr, replayErr, lookupErr} {
		if candidate != nil {
			err = candidate
			break
		}
	}
	if err == nil && status != runtimeprobe.StatusPassed {
		err = fmt.Errorf("stele compilation replay or reference lookup contract failed")
	}
	return runtimeprobe.ProbeOutcome{Events: events}, err
}

func (c *Client) retrieveCompilationReference(ctx context.Context, req runtimeprobe.ProbeRequest, memoryPath, expectedContent string) (bool, error) {
	body := map[string]any{
		"metadata": map[string]any{"request_id": req.CorrelationID + ":retrieve", "operation_id": "knowledge.compilation.retrieve", "schema_version": "provider-v1", "idempotency_key": req.CorrelationID + ":retrieve"},
		"input":    map[string]any{"query": expectedContent, "path": memoryPath, "top_k": 1},
	}
	var response struct {
		Result struct {
			Hits []struct {
				Memory struct {
					MemoryPath string `json:"memory_path"`
					Content    string `json:"content"`
				} `json:"memory"`
			} `json:"hits"`
		} `json:"result"`
	}
	var lastErr error
	for attempt := 0; attempt < 20; attempt++ {
		if err := c.request(ctx, http.MethodPost, "/v1/provider/retrieve", body, &response); err != nil {
			lastErr = err
		} else {
			for _, hit := range response.Result.Hits {
				if hit.Memory.MemoryPath == memoryPath && hit.Memory.Content == expectedContent {
					return true, nil
				}
			}
			lastErr = fmt.Errorf("stele compilation reference %q was not returned by exact-path retrieve", memoryPath)
		}
		if attempt < 19 {
			select {
			case <-ctx.Done():
				return false, ctx.Err()
			case <-time.After(500 * time.Millisecond):
			}
		}
	}
	return false, lastErr
}
