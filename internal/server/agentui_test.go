package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/FelixSeptem/danny/internal/agentui"
	"github.com/FelixSeptem/danny/internal/storage"
)

func newServerStore(t *testing.T) *storage.Store {
	t.Helper()
	store, err := storage.Open(context.Background(), "sqlite", "file:server-agentui-"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("Open(): %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate(): %v", err)
	}
	return store
}

func seedExecution(t *testing.T, store *storage.Store, executionID string) {
	t.Helper()
	ctx := context.Background()
	task, err := store.Tasks().Create(ctx, storage.Task{ID: "task-" + executionID, Title: "ui contract"})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if _, err := store.Executions().Admit(ctx, storage.ExecutionAdmission{
		Execution: storage.TaskExecution{ID: executionID, TaskID: task.ID, TaskRevision: task.Revision, CorrelationID: "corr-" + executionID, IdempotencyKey: "idem-" + executionID, Mode: "direct", PlanVersion: "planner.v1", PlanDigest: "plan", CapabilityDigest: "cap", ProjectionDigest: "projection"},
		Plan:      storage.ExecutionPlanRecord{Version: "planner.v1", Mode: "direct", PlanDigest: "plan", CapabilityDigest: "cap", ProjectionDigest: "projection", Rationale: "simple"},
	}); err != nil {
		t.Fatalf("admit: %v", err)
	}
	repo := store.AgentUI()
	appendEvent := func(kind string, terminal bool, payload string) {
		if _, err := repo.AppendEvent(ctx, storage.AgentUIEvent{ExecutionID: executionID, Kind: kind, Terminal: terminal, Contract: agentui.ProjectionContractV1, Payload: []byte(payload)}); err != nil {
			t.Fatalf("append %s: %v", kind, err)
		}
	}
	appendEvent("run_started", false, `{"mode":"direct"}`)
	appendEvent("planner_decision", false, `{"plan_version":"planner.v1","rationale":"simple"}`)
	appendEvent("text_message", false, `{"message_id":"m1","text":"hello"}`)
}

// TestSSEReconnectConvergesOnTerminalState covers the browser exit gate: a
// client connecting late with a stale cursor gets the snapshot plus bounded
// replay and ends on exactly one terminal event.
func TestSSEReconnectConvergesOnTerminalState(t *testing.T) {
	store := newServerStore(t)
	seedExecution(t, store, "exec-sse")
	ctx := context.Background()
	current, err := store.Executions().UpdateExecution(ctx, "exec-sse", 1, storage.ExecutionDispatching, nil, nil)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	current, err = store.Executions().UpdateExecution(ctx, "exec-sse", current.Revision, storage.ExecutionRunning, nil, nil)
	if err != nil {
		t.Fatalf("running: %v", err)
	}
	if _, err := store.Executions().UpdateExecution(ctx, "exec-sse", current.Revision, storage.ExecutionSucceeded, []byte(`"done"`), nil); err != nil {
		t.Fatalf("complete execution: %v", err)
	}
	if _, err := store.AgentUI().AppendEvent(ctx, storage.AgentUIEvent{ExecutionID: "exec-sse", Kind: "run_finished", Terminal: true, Contract: agentui.ProjectionContractV1, Payload: []byte(`{"result":"done"}`)}); err != nil {
		t.Fatalf("append terminal: %v", err)
	}

	source := &StorageRunSource{Store: store}
	service := &AgentUIService{Source: source}
	server := httptest.NewServer(service.Handler())
	defer server.Close()

	readAll := func(cursor string) []string {
		req, _ := http.NewRequest(http.MethodGet, server.URL+"/agent/threads/corr/runs/exec-sse/events", nil)
		req.Header.Set("Accept", "text/event-stream")
		if cursor != "" {
			req.Header.Set("Last-Event-ID", cursor)
		}
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatalf("stream: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
			t.Fatalf("stream headers: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
		}
		raw, _ := io.ReadAll(resp.Body)
		return strings.Split(string(raw), "\n\n")
	}

	frames := readAll("0")
	joined := strings.Join(frames, "\n")
	if !strings.Contains(joined, "event: snapshot") || !strings.Contains(joined, `"execution_id":"exec-sse"`) {
		t.Fatalf("snapshot frame missing: %s", joined)
	}
	if !strings.Contains(joined, "id: 1") || !strings.Contains(joined, "RunStarted") {
		t.Fatalf("replayed start missing: %s", joined)
	}
	if !strings.Contains(joined, "TextMessageStart") || !strings.Contains(joined, "hello") {
		t.Fatalf("text triple missing: %s", joined)
	}
	if !strings.Contains(joined, `"type":"RunFinished"`) {
		t.Fatalf("terminal missing: %s", joined)
	}
	if strings.Count(joined, `"type":"RunFinished"`) != 1 {
		t.Fatalf("terminal must appear exactly once: %s", joined)
	}

	// Reconnect after the last event: only the snapshot, no replayed events.
	tail := readAll("4")
	tailJoined := strings.Join(tail, "\n")
	if strings.Contains(tailJoined, "RunStarted") {
		t.Fatalf("stale-cursor reconnect must not replay closed history: %s", tailJoined)
	}
}

func TestSSERejectsMalformedCursor(t *testing.T) {
	store := newServerStore(t)
	seedExecution(t, store, "exec-cur")
	service := &AgentUIService{Source: &StorageRunSource{Store: store}}
	server := httptest.NewServer(service.Handler())
	defer server.Close()
	req, _ := http.NewRequest(http.MethodGet, server.URL+"/agent/threads/corr/runs/exec-cur/events", nil)
	req.Header.Set("Last-Event-ID", "not-a-number")
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("malformed cursor must be 400, got %d", resp.StatusCode)
	}
}

type fakeController struct {
	calls int
	err   error
}

func (f *fakeController) Cancel(ctx context.Context, executionID, nativeRunID, idempotencyKey string) error {
	f.calls++
	return f.err
}

func TestCancelEndpointReusesControlBoundary(t *testing.T) {
	store := newServerStore(t)
	seedExecution(t, store, "exec-cancel")
	controller := &fakeController{}
	service := &AgentUIService{Source: &StorageRunSource{Store: store}, Control: controller}
	server := httptest.NewServer(service.Handler())
	defer server.Close()

	resp, err := server.Client().Post(server.URL+"/agent/threads/corr/runs/exec-cancel/cancel", "application/json", strings.NewReader(`{"idempotency_key":"c-1"}`))
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || controller.calls != 1 {
		t.Fatalf("cancel accepted: status=%d calls=%d", resp.StatusCode, controller.calls)
	}
	resp, err = server.Client().Post(server.URL+"/agent/threads/corr/runs/exec-cancel/cancel", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("cancel empty: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing idempotency key must be 400, got %d", resp.StatusCode)
	}
}

func TestSnapshotDerivesFromExecutionRecords(t *testing.T) {
	store := newServerStore(t)
	seedExecution(t, store, "exec-snap")
	source := &StorageRunSource{Store: store}
	snapshot, err := source.Snapshot(context.Background(), "exec-snap")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	var state map[string]any
	if err := json.Unmarshal(snapshot.State, &state); err != nil {
		t.Fatalf("state json: %v", err)
	}
	if state["status"] != "admitted" || state["mode"] != "direct" {
		t.Fatalf("state derivation: %s", snapshot.State)
	}
	var messages []map[string]any
	if err := json.Unmarshal(snapshot.Messages, &messages); err != nil || len(messages) != 1 || messages[0]["content"] != "hello" {
		t.Fatalf("message derivation: %s err=%v", snapshot.Messages, err)
	}
	if _, err := source.Snapshot(context.Background(), "exec-missing"); err == nil {
		t.Fatal("foreign execution snapshot must fail typed")
	}
}

func TestSurfaceSubmissionConvergence(t *testing.T) {
	store := newServerStore(t)
	seedExecution(t, store, "exec-surf")
	catalog := &agentui.Catalog{
		Version:    agentui.CatalogVersion,
		Components: []agentui.Component{{Name: "Card"}},
		Actions:    []agentui.Action{{ID: "confirm", InputSchema: json.RawMessage(`{"required":["choice"],"properties":{"choice":{"type":"string"}}}`), PolicyRef: "danny.policy.task.commit"}},
	}
	tree, err := agentui.NewTree([]byte(`[{"component":"Card"}]`))
	if err != nil {
		t.Fatalf("tree: %v", err)
	}
	surface := agentui.DynamicSurface{
		ContractVersion: agentui.SurfaceContractV1, ID: "surface-1", ExecutionID: "exec-surf",
		Revision: 1, CatalogVersion: agentui.CatalogVersion, Tree: tree, Actions: []string{"confirm"},
	}
	if _, err := store.AgentUI().SaveSurface(context.Background(), surface, 1); err != nil {
		t.Fatalf("save surface: %v", err)
	}
	executions := 0
	service := &AgentUIService{
		Source: &StorageRunSource{Store: store},
		Surfaces: &SurfaceService{Store: store, Catalog: catalog, Executor: func(ctx context.Context, s agentui.DynamicSurface, a agentui.SurfaceAction) ([]byte, error) {
			executions++
			return []byte(`{"ok":true}`), nil
		}},
	}
	server := httptest.NewServer(service.Handler())
	defer server.Close()

	submit := func(body string) (*http.Response, string) {
		resp, err := server.Client().Post(server.URL+"/agent/surfaces/surface-1/actions", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatalf("submit: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		buf := make([]byte, 1<<16)
		n, _ := resp.Body.Read(buf)
		return resp, string(buf[:n])
	}
	valid := `{"contract_version":"danny.surface.v1","execution_id":"exec-surf","surface_id":"surface-1","surface_revision":1,"task_revision":1,"idempotency_key":"idem-1","actions":[{"action_id":"confirm","user_activation":true,"payload":{"choice":"a"}}]}`
	resp, body := submit(valid)
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, `"ok":true`) || executions != 1 {
		t.Fatalf("first submission: status=%d body=%s executions=%d", resp.StatusCode, body, executions)
	}
	// Replayed idempotency key returns the recorded result without re-executing.
	resp, body = submit(valid)
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, `"ok":true`) || executions != 1 {
		t.Fatalf("replayed submission must converge: status=%d body=%s executions=%d", resp.StatusCode, body, executions)
	}
	stale := strings.Replace(valid, `"surface_revision":1`, `"surface_revision":0`, 1)
	if resp, _ = submit(stale); resp.StatusCode != http.StatusConflict {
		t.Fatalf("stale surface must be 409, got %d", resp.StatusCode)
	}
	noActivation := strings.Replace(valid, `"user_activation":true`, `"user_activation":false`, 1)
	noActivation = strings.Replace(noActivation, `"idempotency_key":"idem-1"`, `"idempotency_key":"idem-2"`, 1)
	if resp, _ = submit(noActivation); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing activation must be 400, got %d", resp.StatusCode)
	}
	unknown := strings.Replace(valid, `"idempotency_key":"idem-1"`, `"idempotency_key":"idem-3"`, 1)
	unknown = strings.Replace(unknown, `"action_id":"confirm"`, `"action_id":"mint_money"`, 1)
	if resp, _ = submit(unknown); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unregistered action must be 400, got %d", resp.StatusCode)
	}
}

func TestUnavailableEndpointsFailClosed(t *testing.T) {
	service := &AgentUIService{}
	server := httptest.NewServer(service.Handler())
	defer server.Close()
	resp, err := server.Client().Get(server.URL + "/agent/threads/corr/runs/exec/events")
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("missing source must be 503, got %d", resp.StatusCode)
	}
	controller := &fakeController{err: errors.New("not admitted")}
	svc := &AgentUIService{Control: controller}
	srv2 := httptest.NewServer(svc.Handler())
	defer srv2.Close()
	resp2, err := srv2.Client().Post(srv2.URL+"/agent/threads/corr/runs/exec/cancel", "application/json", strings.NewReader(`{"idempotency_key":"k"}`))
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	_ = resp2.Body.Close()
	if resp2.StatusCode != http.StatusInternalServerError {
		t.Fatalf("control failure must surface typed, got %d", resp2.StatusCode)
	}
	_ = time.Now
}
