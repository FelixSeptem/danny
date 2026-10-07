package stele

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/FelixSeptem/danny/internal/runtimeprobe"
	"github.com/FelixSeptem/stele/openapi"
)

const PinnedVersion = "v0.0.0-20261007033528-a556ac0ddacc"

// Adapter is the HTTP/OpenAPI boundary for Stele's public runtime surface.
// Stele's internal Go packages are intentionally not imported by this project.
type Adapter interface {
	runtimeprobe.SteleAdapter
}

type Client struct {
	BaseURL   string
	HTTP      *http.Client
	APIKey    string
	BindingID string
	SessionID string
	Tenant    string
	Project   string
	Namespace string
}

// ProjectionRequest is the narrow product request for bounded context
// disclosure. It intentionally carries a query and budget rather than a
// request to export the Stele store.
type ProjectionRequest struct {
	RunID  string
	Query  string
	Budget int
	Role   string
}

type ProjectionReference struct {
	ID      string   `json:"id,omitempty"`
	Kind    string   `json:"kind,omitempty"`
	Locator string   `json:"locator,omitempty"`
	Summary string   `json:"summary,omitempty"`
	Tags    []string `json:"tags,omitempty"`
}

type BoundedProjection struct {
	References []ProjectionReference `json:"references"`
	Digest     string                `json:"digest"`
	Content    string                `json:"-"`
}

func (c *Client) BoundedProjection(ctx context.Context, req ProjectionRequest) (BoundedProjection, error) {
	if c == nil || strings.TrimSpace(c.BaseURL) == "" || strings.TrimSpace(req.RunID) == "" {
		return BoundedProjection{}, fmt.Errorf("stele projection request is not configured")
	}
	if req.Budget <= 0 {
		req.Budget = 2048
	}
	body := map[string]any{
		"metadata": map[string]any{"request_id": req.RunID, "operation_id": "context_projection", "schema_version": "provider-v1", "idempotency_key": req.RunID + ":projection"},
		"input":    map[string]any{"query": req.Query, "budget": req.Budget, "role": req.Role},
	}
	var response struct {
		Result struct {
			References []ProjectionReference `json:"references"`
			Content    string                `json:"content"`
		} `json:"result"`
	}
	if err := c.request(ctx, http.MethodPost, "/v1/provider/context", body, &response); err != nil {
		return BoundedProjection{}, err
	}
	// Only references enter the child projection. Content remains available
	// through an explicit bounded read operation and is never copied here.
	refs := append([]ProjectionReference(nil), response.Result.References...)
	canonical, _ := json.Marshal(refs)
	digest := sha256.Sum256(canonical)
	return BoundedProjection{References: refs, Digest: hex.EncodeToString(digest[:])}, nil
}

func NewClient(baseURL string, httpClient *http.Client) (*Client, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, nil
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{BaseURL: baseURL, HTTP: httpClient}, nil
}

func (c *Client) Version() string { return PinnedVersion }

// OpenAPIInventory returns the exact public service contract exposed by the
// pinned Stele module. The adapter never imports Stele internal packages.
func OpenAPIInventory() string { return openapi.SpecYAML() }

type RuntimeInitialization struct {
	Scope          map[string]string `json:"scope"`
	AgentID        string            `json:"agent_id"`
	SessionID      string            `json:"session_id"`
	ConversationID string            `json:"conversation_id,omitempty"`
}

type RuntimeBinding struct {
	BindingID string `json:"binding_id"`
	SessionID string `json:"session_id"`
}

func (c *Client) InitializeRuntime(ctx context.Context, in RuntimeInitialization) (RuntimeBinding, error) {
	var out RuntimeBinding
	if err := c.request(ctx, http.MethodPost, "/v1/provider/runtime", in, &out); err != nil {
		return RuntimeBinding{}, err
	}
	c.BindingID, c.SessionID = out.BindingID, out.SessionID
	c.Tenant, c.Project, c.Namespace = in.Scope["tenant"], in.Scope["project"], in.Scope["namespace"]
	if c.SessionID == "" {
		c.SessionID = in.SessionID
	}
	return out, nil
}

func (c *Client) Context(ctx context.Context, req runtimeprobe.ProbeRequest) (runtimeprobe.ProbeOutcome, error) {
	return c.operation(ctx, req, runtimeprobe.KindContext, "/v1/provider/context", map[string]any{"input": map[string]any{"query": req.Input, "budget": 2048}})
}

func (c *Client) Memory(ctx context.Context, req runtimeprobe.ProbeRequest) (runtimeprobe.ProbeOutcome, error) {
	if c == nil || strings.TrimSpace(c.BaseURL) == "" {
		return c.operation(ctx, req, runtimeprobe.KindMemory, "/v1/provider/events", map[string]any{"event_type": "runtimeprobe.memory", "content": req.Input})
	}
	if req.CorrelationID == "" || req.ParentID == "" {
		return runtimeprobe.ProbeOutcome{}, fmt.Errorf("correlation and parent ids are required")
	}
	writeBody := map[string]any{"metadata": map[string]any{"request_id": req.CorrelationID, "operation_id": req.Capability, "schema_version": "provider-v1", "idempotency_key": req.CorrelationID}, "event_type": "runtimeprobe.memory", "content": req.Input}
	var writeResponse map[string]any
	writeErr := c.request(ctx, http.MethodPost, "/v1/provider/events", writeBody, &writeResponse)
	readBody := map[string]any{"metadata": map[string]any{"request_id": req.CorrelationID + ":retrieve", "operation_id": "retrieve", "schema_version": "provider-v1", "idempotency_key": req.CorrelationID + ":retrieve"}, "input": map[string]any{"query": req.Input, "top_k": 10}}
	var readResponse map[string]any
	readErr := c.request(ctx, http.MethodPost, "/v1/provider/retrieve", readBody, &readResponse)
	status := runtimeprobe.StatusPassed
	if writeErr != nil || readErr != nil {
		status = runtimeprobe.StatusFailedContract
	}
	now := time.Unix(0, 0).UTC()
	payload := map[string]string{"write_path": "/v1/provider/events", "retrieve_path": "/v1/provider/retrieve", "retrieve": "passed"}
	if id, ok := writeResponse["event_id"].(string); ok && id != "" {
		payload["event_id"] = id
	}
	if writeErr != nil {
		payload["write_error"] = writeErr.Error()
	}
	if readErr != nil {
		payload["retrieve"] = "failed"
		payload["retrieve_error"] = readErr.Error()
	}
	events := []runtimeprobe.Event{{Sequence: 1, ID: req.ParentID, Kind: runtimeprobe.KindParent, CorrelationID: req.CorrelationID, Capability: req.Capability, Status: status, At: now}, {Sequence: 2, ID: req.ParentID + ":memory", Kind: runtimeprobe.KindMemory, ParentID: req.ParentID, CorrelationID: req.CorrelationID, Capability: req.Capability, Status: status, At: now.Add(time.Nanosecond), Payload: payload}, {Sequence: 3, ID: req.ParentID + ":memory:terminal", Kind: runtimeprobe.KindTerminal, ParentID: req.ParentID + ":memory", CorrelationID: req.CorrelationID, Capability: req.Capability, Status: status, At: now.Add(2 * time.Nanosecond)}}
	var err error
	if writeErr != nil {
		err = writeErr
	} else if readErr != nil {
		err = readErr
	}
	return runtimeprobe.ProbeOutcome{Events: events}, err
}

// References exercises the public provider event memory_path and exact-path
// retrieval contract for task and run references. Stele does not expose a
// separate reference endpoint; memory_path is the public scoped reference
// boundary and the returned canonical memory IDs prove persistence.
func (c *Client) References(ctx context.Context, req runtimeprobe.ProbeRequest) (runtimeprobe.ProbeOutcome, error) {
	if c == nil || strings.TrimSpace(c.BaseURL) == "" {
		return c.operation(ctx, req, runtimeprobe.KindMemory, "/v1/provider/events", map[string]any{"event_type": "runtimeprobe.reference", "content": req.Input})
	}
	if req.CorrelationID == "" || req.ParentID == "" {
		return runtimeprobe.ProbeOutcome{}, fmt.Errorf("correlation and parent ids are required")
	}
	taskID := req.CorrelationID + ":task"
	runID := req.CorrelationID + ":run"
	taskValue := "task-reference:" + taskID
	runValue := "run-reference:" + runID
	taskPath := "runtimeprobe/tasks/" + taskID
	runPath := "runtimeprobe/runs/" + runID
	taskEventID, taskWriteErr := c.appendReference(ctx, req, "runtimeprobe.task_reference", taskPath, taskValue)
	runEventID, runWriteErr := c.appendReference(ctx, req, "runtimeprobe.run_reference", runPath, runValue)
	taskRetrieved, taskReadErr := c.retrieveReference(ctx, req, taskPath, taskValue)
	runRetrieved, runReadErr := c.retrieveReference(ctx, req, runPath, runValue)
	status := runtimeprobe.StatusPassed
	for _, err := range []error{taskWriteErr, runWriteErr, taskReadErr, runReadErr} {
		if err != nil {
			status = runtimeprobe.StatusFailedContract
			break
		}
	}
	now := time.Unix(0, 0).UTC()
	payload := map[string]string{
		"task_reference_id": taskEventID,
		"run_reference_id":  runEventID,
		"task_retrieved":    fmt.Sprint(taskRetrieved),
		"run_retrieved":     fmt.Sprint(runRetrieved),
		"task_path":         taskPath,
		"run_path":          runPath,
	}
	for key, err := range map[string]error{"task_write_error": taskWriteErr, "run_write_error": runWriteErr, "task_read_error": taskReadErr, "run_read_error": runReadErr} {
		if err != nil {
			payload[key] = err.Error()
		}
	}
	events := []runtimeprobe.Event{
		{Sequence: 1, ID: req.ParentID, Kind: runtimeprobe.KindParent, CorrelationID: req.CorrelationID, Capability: req.Capability, Status: status, At: now},
		{Sequence: 2, ID: req.ParentID + ":references", Kind: runtimeprobe.KindMemory, ParentID: req.ParentID, CorrelationID: req.CorrelationID, Capability: req.Capability, Status: status, At: now.Add(time.Nanosecond), Payload: payload},
		{Sequence: 3, ID: req.ParentID + ":references:terminal", Kind: runtimeprobe.KindTerminal, ParentID: req.ParentID + ":references", CorrelationID: req.CorrelationID, Capability: req.Capability, Status: status, At: now.Add(2 * time.Nanosecond)},
	}
	var err error
	for _, candidate := range []error{taskWriteErr, runWriteErr, taskReadErr, runReadErr} {
		if candidate != nil {
			err = candidate
			break
		}
	}
	return runtimeprobe.ProbeOutcome{Events: events}, err
}

func (c *Client) appendReference(ctx context.Context, req runtimeprobe.ProbeRequest, eventType, memoryPath, content string) (string, error) {
	body := map[string]any{
		"metadata": map[string]any{"request_id": req.CorrelationID + ":" + eventType, "operation_id": eventType, "schema_version": "provider-v1", "idempotency_key": req.CorrelationID + ":" + eventType},
		// Stele's default governed extractor promotes conversation.message
		// events to canonical memories. Keep the task/run distinction in the
		// operation metadata while using that documented event contract.
		"event_type":  "conversation.message",
		"memory_path": memoryPath,
		"content":     content,
	}
	var response map[string]any
	if err := c.request(ctx, http.MethodPost, "/v1/provider/events", body, &response); err != nil {
		return "", err
	}
	id, _ := response["event_id"].(string)
	if strings.TrimSpace(id) == "" {
		return "", fmt.Errorf("stele reference write returned no event_id")
	}
	return id, nil
}

func (c *Client) retrieveReference(ctx context.Context, req runtimeprobe.ProbeRequest, memoryPath, expectedContent string) (bool, error) {
	pathToken := referencePathToken(memoryPath)
	body := map[string]any{
		"metadata": map[string]any{"request_id": req.CorrelationID + ":retrieve:" + pathToken, "operation_id": "runtimeprobe.reference.retrieve", "schema_version": "provider-v1", "idempotency_key": req.CorrelationID + ":retrieve:" + pathToken},
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
	if err := c.request(ctx, http.MethodPost, "/v1/provider/retrieve", body, &response); err != nil {
		return false, err
	}
	for _, hit := range response.Result.Hits {
		if hit.Memory.MemoryPath == memoryPath && hit.Memory.Content == expectedContent {
			return true, nil
		}
	}
	return false, fmt.Errorf("stele reference %q was not returned by exact-path retrieve", memoryPath)
}

// referencePathToken keeps provider operation metadata within Stele's token
// grammar while preserving the original memory path in the search selector.
// A digest also prevents path content from being copied into evidence or
// operation logs when this helper is reused for arbitrary references.
func referencePathToken(memoryPath string) string {
	digest := sha256.Sum256([]byte(memoryPath))
	return hex.EncodeToString(digest[:])
}

func (c *Client) RestartProjection(ctx context.Context, req runtimeprobe.ProbeRequest) (runtimeprobe.ProbeOutcome, error) {
	if c == nil {
		return runtimeprobe.ProbeOutcome{}, fmt.Errorf("stele adapter is nil")
	}
	fresh := *c
	fresh.BindingID, fresh.SessionID = "", ""
	if fresh.BindingID == "" && fresh.APIKey != "" {
		_, err := fresh.InitializeRuntime(ctx, RuntimeInitialization{Scope: map[string]string{"tenant": c.Tenant, "project": c.Project, "namespace": c.Namespace}, AgentID: "runtimeprobe-restarted", SessionID: req.CorrelationID + ":restarted"})
		if err != nil {
			return runtimeprobe.ProbeOutcome{}, err
		}
	}
	return fresh.operation(ctx, req, runtimeprobe.KindContext, "/v1/provider/context", map[string]any{"input": map[string]any{"query": req.Input, "budget": 2048}})
}

func (c *Client) IdempotentMemory(ctx context.Context, req runtimeprobe.ProbeRequest) (runtimeprobe.ProbeOutcome, error) {
	if c == nil || strings.TrimSpace(c.BaseURL) == "" {
		return c.operation(ctx, req, runtimeprobe.KindMemory, "/v1/provider/events", map[string]any{"event_type": "runtimeprobe.memory", "content": req.Input})
	}
	if req.CorrelationID == "" || req.ParentID == "" {
		return runtimeprobe.ProbeOutcome{}, fmt.Errorf("correlation and parent ids are required")
	}
	body := map[string]any{"metadata": map[string]any{"request_id": req.CorrelationID, "operation_id": req.Capability, "schema_version": "provider-v1", "idempotency_key": req.CorrelationID}, "event_type": "runtimeprobe.memory", "content": req.Input}
	var first, replay map[string]any
	errFirst := c.request(ctx, http.MethodPost, "/v1/provider/events", body, &first)
	errReplay := c.request(ctx, http.MethodPost, "/v1/provider/events", body, &replay)
	status := runtimeprobe.StatusPassed
	if errFirst != nil || errReplay != nil {
		status = runtimeprobe.StatusFailedContract
	}
	replayed, _ := replay["replayed"].(bool)
	if errFirst == nil && errReplay == nil && !replayed {
		status = runtimeprobe.StatusFailedContract
	}
	now := time.Unix(0, 0).UTC()
	payloadFirst := map[string]string{"path": "/v1/provider/events", "attempt": "first"}
	payloadReplay := map[string]string{"path": "/v1/provider/events", "attempt": "replay", "replayed": fmt.Sprint(replayed)}
	if id, ok := first["event_id"].(string); ok && id != "" {
		payloadFirst["event_id"] = id
	}
	if id, ok := replay["event_id"].(string); ok && id != "" {
		payloadReplay["event_id"] = id
	}
	if errFirst != nil {
		payloadFirst["error"] = errFirst.Error()
	}
	if errReplay != nil {
		payloadReplay["error"] = errReplay.Error()
	}
	events := []runtimeprobe.Event{{Sequence: 1, ID: req.ParentID, Kind: runtimeprobe.KindParent, CorrelationID: req.CorrelationID, Capability: req.Capability, Status: status, At: now}, {Sequence: 2, ID: req.ParentID + ":memory:first", Kind: runtimeprobe.KindMemory, ParentID: req.ParentID, CorrelationID: req.CorrelationID, Capability: req.Capability, Status: status, At: now.Add(time.Nanosecond), Payload: payloadFirst}, {Sequence: 3, ID: req.ParentID + ":memory:replay", Kind: runtimeprobe.KindMemory, ParentID: req.ParentID, CorrelationID: req.CorrelationID, Capability: req.Capability, Status: status, At: now.Add(2 * time.Nanosecond), Payload: payloadReplay}, {Sequence: 4, ID: req.ParentID + ":terminal", Kind: runtimeprobe.KindTerminal, ParentID: req.ParentID, CorrelationID: req.CorrelationID, Capability: req.Capability, Status: status, At: now.Add(3 * time.Nanosecond)}}
	var err error
	if errFirst != nil {
		err = errFirst
	} else if errReplay != nil {
		err = errReplay
	}
	return runtimeprobe.ProbeOutcome{Events: events}, err
}

func (c *Client) operation(ctx context.Context, req runtimeprobe.ProbeRequest, kind runtimeprobe.Kind, path string, input map[string]any) (runtimeprobe.ProbeOutcome, error) {
	if c == nil || strings.TrimSpace(c.BaseURL) == "" {
		return runtimeprobe.ProbeOutcome{Events: []runtimeprobe.Event{{Sequence: 1, ID: req.CorrelationID + ":terminal", Kind: runtimeprobe.KindTerminal, ParentID: req.ParentID, CorrelationID: req.CorrelationID, Capability: req.Capability, Status: runtimeprobe.StatusSkippedUnconfigured, At: time.Unix(0, 0).UTC()}}}, nil
	}
	if req.CorrelationID == "" || req.ParentID == "" {
		return runtimeprobe.ProbeOutcome{}, fmt.Errorf("correlation and parent ids are required")
	}
	body := map[string]any{"metadata": map[string]any{"request_id": req.CorrelationID, "operation_id": req.Capability, "schema_version": "provider-v1", "idempotency_key": req.CorrelationID}}
	for k, v := range input {
		body[k] = v
	}
	var response map[string]any
	err := c.request(ctx, http.MethodPost, path, body, &response)
	status := runtimeprobe.StatusPassed
	payload := map[string]string{"path": path}
	if eventID, ok := response["event_id"].(string); ok && eventID != "" {
		payload["event_id"] = eventID
	}
	if replayed, ok := response["replayed"].(bool); ok {
		payload["replayed"] = fmt.Sprint(replayed)
	}
	if err != nil {
		status = runtimeprobe.StatusFailedContract
		payload["error"] = err.Error()
	}
	now := time.Unix(0, 0).UTC()
	events := []runtimeprobe.Event{{Sequence: 1, ID: req.ParentID, Kind: runtimeprobe.KindParent, CorrelationID: req.CorrelationID, Capability: req.Capability, Status: status, At: now}, {Sequence: 2, ID: req.ParentID + ":" + string(kind), Kind: kind, ParentID: req.ParentID, CorrelationID: req.CorrelationID, Capability: req.Capability, Status: status, At: now.Add(time.Nanosecond), Payload: payload}, {Sequence: 3, ID: req.ParentID + ":" + string(kind) + ":terminal", Kind: runtimeprobe.KindTerminal, ParentID: req.ParentID + ":" + string(kind), CorrelationID: req.CorrelationID, Capability: req.Capability, Status: status, At: now.Add(2 * time.Nanosecond)}}
	return runtimeprobe.ProbeOutcome{Events: events}, err
}

func (c *Client) request(ctx context.Context, method, path string, body any, out any) error {
	if c == nil || strings.TrimSpace(c.BaseURL) == "" {
		return fmt.Errorf("stele endpoint is not configured")
	}
	base, err := url.Parse(c.BaseURL)
	if err != nil {
		return err
	}
	base.Path = strings.TrimRight(base.Path, "/") + path
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, base.String(), reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.APIKey != "" {
		req.Header.Set("X-API-Key", c.APIKey)
	}
	if c.Tenant != "" {
		req.Header.Set("X-Stele-Tenant", c.Tenant)
	}
	if c.Project != "" {
		req.Header.Set("X-Stele-Project", c.Project)
	}
	if c.Namespace != "" {
		req.Header.Set("X-Stele-Namespace", c.Namespace)
	}
	if c.BindingID != "" {
		req.Header.Set("X-Stele-Runtime-Binding", c.BindingID)
	}
	if c.SessionID != "" {
		req.Header.Set("X-Stele-Runtime-Session", c.SessionID)
	}
	if object, ok := body.(map[string]any); ok {
		if metadata, ok := object["metadata"].(map[string]any); ok {
			if key, ok := metadata["idempotency_key"].(string); ok && key != "" {
				req.Header.Set("Idempotency-Key", key)
			}
		}
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("stele %s returned %s", path, resp.Status)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

var _ Adapter = (*Client)(nil)
