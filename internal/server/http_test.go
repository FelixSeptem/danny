package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FelixSeptem/danny/internal/agentui"
	"github.com/FelixSeptem/danny/internal/config"
	"github.com/FelixSeptem/danny/internal/security"
	"github.com/FelixSeptem/danny/internal/storage"
)

func newWorkbenchServer(t *testing.T) (*Server, *storage.Store) {
	t.Helper()
	store := newServerStore(t)
	return &Server{
		AgentUI:     &AgentUIService{Source: &StorageRunSource{Store: store}},
		Projections: store.Projections(),
		Tasks:       store.Tasks(),
	}, store
}

func startWorkbench(t *testing.T, svc *Server) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(svc.Handler())
	t.Cleanup(srv.Close)
	return srv
}

func doRequest(t *testing.T, srv *httptest.Server, method, path, body string, mutate func(*http.Request)) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if mutate != nil {
		mutate(req)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}

func sameOrigin(req *http.Request) { req.Header.Set("Origin", "http://"+req.Host) }

func errorCode(t *testing.T, body string) string {
	t.Helper()
	var decoded struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatalf("decode error body %q: %v", body, err)
	}
	return decoded.Error
}

func TestSameOriginGuardRejectsEveryStateChangingMethod(t *testing.T) {
	svc, store := newWorkbenchServer(t)
	ctx := context.Background()
	if _, err := store.Tasks().Create(ctx, storage.Task{ID: "task-1", Title: "original"}); err != nil {
		t.Fatalf("create task: %v", err)
	}
	srv := startWorkbench(t, svc)
	methods := []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete}
	for _, method := range methods {
		status, body := doRequest(t, srv, method, "/api/tasks/task-1", `{"expected_revision":1,"title":"changed"}`, nil)
		if status != http.StatusForbidden || errorCode(t, body) != "same_origin_required" {
			t.Fatalf("%s without origin: status=%d body=%s", method, status, body)
		}
	}
	// No handler ran: the task is untouched.
	task, _ := store.Tasks().Get(ctx, "task-1")
	if task.Title != "original" {
		t.Fatalf("guarded request mutated state: %#v", task)
	}
	// A mismatched Origin is rejected too.
	status, body := doRequest(t, srv, http.MethodPatch, "/api/tasks/task-1", `{"expected_revision":1,"title":"changed"}`, func(r *http.Request) {
		r.Header.Set("Origin", "http://evil.example")
	})
	if status != http.StatusForbidden || errorCode(t, body) != "same_origin_required" {
		t.Fatalf("mismatched origin: status=%d body=%s", status, body)
	}
	// Sec-Fetch-Site is an accepted alternative signal.
	status, _ = doRequest(t, srv, http.MethodPatch, "/api/tasks/task-1", `{"expected_revision":1,"title":"allowed"}`, func(r *http.Request) {
		r.Header.Set("Sec-Fetch-Site", "same-origin")
	})
	if status != http.StatusOK {
		t.Fatalf("same-site signal must pass, got %d", status)
	}
}

func TestSameOriginGuardCoversAgentControlEndpoints(t *testing.T) {
	store := newServerStore(t)
	seedExecution(t, store, "exec-guard")
	controller := &fakeController{}
	svc := &Server{AgentUI: &AgentUIService{Source: &StorageRunSource{Store: store}, Control: controller}}
	srv := startWorkbench(t, svc)
	status, body := doRequest(t, srv, http.MethodPost, "/agent/threads/corr/runs/exec-guard/cancel", `{"idempotency_key":"c-1"}`, nil)
	if status != http.StatusForbidden || errorCode(t, body) != "same_origin_required" {
		t.Fatalf("cross-site cancel: status=%d body=%s", status, body)
	}
	if controller.calls != 0 {
		t.Fatalf("cancel handler ran through the guard: calls=%d", controller.calls)
	}
	status, _ = doRequest(t, srv, http.MethodPost, "/agent/threads/corr/runs/exec-guard/cancel", `{"idempotency_key":"c-1"}`, sameOrigin)
	if status != http.StatusOK || controller.calls != 1 {
		t.Fatalf("same-origin cancel: status=%d calls=%d", status, controller.calls)
	}
}

func TestReadsDoNotRequireOrigin(t *testing.T) {
	svc, _ := newWorkbenchServer(t)
	srv := startWorkbench(t, svc)
	for _, path := range []string{"/api/health", "/api/tasks", "/api/schedules", "/api/deliveries", "/api/notifications", "/api/knowledge/generations", "/api/knowledge/sources", "/api/knowledge/status", "/api/knowledge/artifacts?generation_id=1", "/api/runs"} {
		status, _ := doRequest(t, srv, http.MethodGet, path, "", nil)
		if status != http.StatusOK {
			t.Fatalf("GET %s without origin: status=%d", path, status)
		}
	}
}

func TestTaskPatchIsRevisionAware(t *testing.T) {
	svc, store := newWorkbenchServer(t)
	ctx := context.Background()
	if _, err := store.Tasks().Create(ctx, storage.Task{ID: "task-1", Title: "original"}); err != nil {
		t.Fatalf("create task: %v", err)
	}
	srv := startWorkbench(t, svc)

	status, body := doRequest(t, srv, http.MethodPatch, "/api/tasks/task-1", `{"expected_revision":1,"title":"updated"}`, sameOrigin)
	if status != http.StatusOK || !strings.Contains(body, `"title":"updated"`) || !strings.Contains(body, `"revision":2`) {
		t.Fatalf("current revision update: status=%d body=%s", status, body)
	}
	status, body = doRequest(t, srv, http.MethodPatch, "/api/tasks/task-1", `{"expected_revision":1,"title":"stale"}`, sameOrigin)
	if status != http.StatusConflict || errorCode(t, body) != "task_revision_conflict" {
		t.Fatalf("stale revision: status=%d body=%s", status, body)
	}
	status, body = doRequest(t, srv, http.MethodPatch, "/api/tasks/task-1", `{"title":"no-revision"}`, sameOrigin)
	if status != http.StatusBadRequest || errorCode(t, body) != "malformed" {
		t.Fatalf("missing revision: status=%d body=%s", status, body)
	}
	task, _ := store.Tasks().Get(ctx, "task-1")
	if task.Title != "updated" || task.Revision != 2 {
		t.Fatalf("stale or malformed update mutated state: %#v", task)
	}
}

func TestUnknownResourcesAndWritePathsAreTyped(t *testing.T) {
	svc, store := newWorkbenchServer(t)
	ctx := context.Background()
	if _, err := store.Tasks().Create(ctx, storage.Task{ID: "task-1", Title: "original"}); err != nil {
		t.Fatalf("create task: %v", err)
	}
	srv := startWorkbench(t, svc)
	for _, path := range []string{"/api/tasks/missing", "/api/schedules/missing", "/api/deliveries/missing", "/api/runs/missing"} {
		status, body := doRequest(t, srv, http.MethodGet, path, "", nil)
		if status != http.StatusNotFound || errorCode(t, body) != "not_found" {
			t.Fatalf("GET %s: status=%d body=%s", path, status, body)
		}
	}
	// No browser write path beyond the task patch and the M9 control endpoints.
	for _, path := range []string{"/api/tasks", "/api/schedules", "/api/knowledge/status", "/api/runs"} {
		status, body := doRequest(t, srv, http.MethodPost, path, `{}`, sameOrigin)
		if status != http.StatusMethodNotAllowed && status != http.StatusNotFound {
			t.Fatalf("POST %s must not mutate: status=%d body=%s", path, status, body)
		}
	}
	status, _ := doRequest(t, srv, http.MethodGet, "/api/knowledge/artifacts", "", nil)
	if status != http.StatusBadRequest {
		t.Fatalf("artifacts without generation_id: status=%d", status)
	}
}

func TestAPIResponsesExposeOnlyAllowedFields(t *testing.T) {
	svc, store := newWorkbenchServer(t)
	ctx := context.Background()
	if _, err := store.Tasks().Create(ctx, storage.Task{ID: "task-1", Title: "t"}); err != nil {
		t.Fatal(err)
	}
	srv := startWorkbench(t, svc)
	status, body := doRequest(t, srv, http.MethodGet, "/api/tasks", "", nil)
	if status != http.StatusOK {
		t.Fatalf("GET tasks: %d", status)
	}
	var list []map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &list); err != nil || len(list) != 1 {
		t.Fatalf("decode tasks: %v %s", err, body)
	}
	allowed := map[string]bool{"id": true, "title": true, "description": true, "status": true, "revision": true, "created_at": true, "updated_at": true}
	for key := range list[0] {
		if !allowed[key] {
			t.Fatalf("task projection leaked field %q", key)
		}
	}
}

func TestAssembledHandlerPassesMilestone9SSEThrough(t *testing.T) {
	svc, store := newWorkbenchServer(t)
	seedExecution(t, store, "exec-m10")
	// Close the log with a terminal event so the SSE stream ends instead of
	// tailing until its deadline (the Milestone 9 terminal-framing contract).
	ctx := context.Background()
	if _, err := store.AgentUI().AppendEvent(ctx, storage.AgentUIEvent{
		ExecutionID: "exec-m10", Kind: "run_finished", Terminal: true,
		Contract: agentui.ProjectionContractV1, Payload: []byte(`{"result":"done"}`),
	}); err != nil {
		t.Fatalf("append terminal: %v", err)
	}
	srv := startWorkbench(t, svc)
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/agent/threads/corr/runs/exec-m10/events", nil)
	req.Header.Set("Accept", "text/event-stream")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("stream headers: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	raw, _ := io.ReadAll(resp.Body)
	joined := string(raw)
	if !strings.Contains(joined, "event: snapshot") || !strings.Contains(joined, "RunStarted") || !strings.Contains(joined, "hello") {
		t.Fatalf("M9 SSE framing changed through assembled handler: %s", joined)
	}
}

func TestMissingDependenciesFailClosed(t *testing.T) {
	srv := startWorkbench(t, &Server{})
	for _, path := range []string{"/api/tasks", "/agent/threads/corr/runs/x/events"} {
		status, body := doRequest(t, srv, http.MethodGet, path, "", nil)
		if status != http.StatusServiceUnavailable || errorCode(t, body) != "unavailable" {
			t.Fatalf("GET %s with missing dependency: status=%d body=%s", path, status, body)
		}
	}
	// Health is dependency-free.
	status, _ := doRequest(t, srv, http.MethodGet, "/api/health", "", nil)
	if status != http.StatusOK {
		t.Fatalf("health: status=%d", status)
	}
}

func TestStaticAssetsWithSPAFallback(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>workbench</html>"), 0o600); err != nil {
		t.Fatal(err)
	}
	svc, _ := newWorkbenchServer(t)
	svc.StaticDir = dir
	srv := startWorkbench(t, svc)
	status, body := doRequest(t, srv, http.MethodGet, "/some/client/route", "", nil)
	if status != http.StatusOK || !strings.Contains(body, "workbench") {
		t.Fatalf("SPA fallback: status=%d body=%s", status, body)
	}
	// Path traversal must not escape the asset root: either the standard
	// library rejects the unclean path or the SPA fallback serves the shell.
	status, body = doRequest(t, srv, http.MethodGet, "/../projections.go", "", nil)
	if strings.Contains(body, "package storage") || strings.Contains(body, "func (r *projectionRepository)") {
		t.Fatalf("traversal leaked a source file: status=%d body=%s", status, body)
	}
	if status != http.StatusOK && status != http.StatusBadRequest {
		t.Fatalf("traversal must be rejected or fall back, got %d", status)
	}
}

func TestStaticPlaceholderWithoutNodeToolchain(t *testing.T) {
	svc, _ := newWorkbenchServer(t)
	srv := startWorkbench(t, svc)
	status, body := doRequest(t, srv, http.MethodGet, "/", "", nil)
	if status != http.StatusOK || !strings.Contains(body, "Danny workbench") {
		t.Fatalf("placeholder: status=%d body=%s", status, body)
	}
}

func TestSurfaceEndpointRendersServerRegisteredWire(t *testing.T) {
	svc, store := newWorkbenchServer(t)
	catalog := agentui.DefaultCatalog()
	svc.Surfaces = &StorageSurfaceSource{Store: store, Catalog: catalog}
	ctx := context.Background()
	tree, err := agentui.NewTree([]byte(`[{"component":"Card","props":{"title":"Approve"},"children":[{"component":"Text","props":{"value":"review"}}]}]`))
	if err != nil {
		t.Fatalf("new tree: %v", err)
	}
	if _, err := store.AgentUI().SaveSurface(ctx, agentui.DynamicSurface{
		ContractVersion: agentui.SurfaceContractV1,
		ID:              "surface-1", ExecutionID: "exec-1", Revision: 1,
		CatalogVersion: agentui.CatalogVersion, Tree: tree, Actions: []string{"confirm"},
	}, 1); err != nil {
		t.Fatalf("save surface: %v", err)
	}
	srv := startWorkbench(t, svc)
	status, body := doRequest(t, srv, http.MethodGet, "/api/surfaces/surface-1", "", nil)
	if status != http.StatusOK {
		t.Fatalf("surface wire: status=%d body=%s", status, body)
	}
	var wire struct {
		A2UIVersion    string `json:"a2ui_version"`
		SurfaceID      string `json:"surface_id"`
		CatalogVersion string `json:"catalog_version"`
		Components     []struct {
			Type string `json:"type"`
		} `json:"components"`
	}
	if err := json.Unmarshal([]byte(body), &wire); err != nil {
		t.Fatalf("decode wire: %v", err)
	}
	if wire.A2UIVersion != agentui.A2UIWireVersion || wire.SurfaceID != "surface-1" || len(wire.Components) == 0 {
		t.Fatalf("unexpected wire: %#v", wire)
	}
	if wire.CatalogVersion != agentui.CatalogVersion {
		t.Fatalf("catalog version: %q", wire.CatalogVersion)
	}
	// Only server-registered components appear on the wire.
	for _, component := range wire.Components {
		if !catalog.RegisteredComponent(component.Type) {
			t.Fatalf("unregistered component %q reached the wire", component.Type)
		}
	}
	// Unknown surface is a typed 404.
	status, body = doRequest(t, srv, http.MethodGet, "/api/surfaces/missing", "", nil)
	if status != http.StatusNotFound || errorCode(t, body) != "not_found" {
		t.Fatalf("unknown surface: status=%d body=%s", status, body)
	}
	// No write path through the surface projection.
	status, body = doRequest(t, srv, http.MethodPost, "/api/surfaces/surface-1", `{}`, sameOrigin)
	if status != http.StatusMethodNotAllowed {
		t.Fatalf("POST surface: status=%d body=%s", status, body)
	}
}

func TestSurfaceEndpointFailsClosedWithoutSource(t *testing.T) {
	svc, _ := newWorkbenchServer(t)
	srv := startWorkbench(t, svc)
	status, body := doRequest(t, srv, http.MethodGet, "/api/surfaces/anything", "", nil)
	if status != http.StatusServiceUnavailable || errorCode(t, body) != "unavailable" {
		t.Fatalf("missing surface source: status=%d body=%s", status, body)
	}
}

func TestDefaultListenerIsLoopbackOnly(t *testing.T) {
	cfg, err := config.LoadFromEnv(func(string) (string, bool) { return "", false })
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if err := security.ValidateListener(cfg.ListenAddress, cfg.AuthenticatedMode); err != nil {
		t.Fatalf("default listener must be loopback: %v", err)
	}
	if err := security.ValidateListener("0.0.0.0:8080", false); err == nil {
		t.Fatal("non-loopback listener must be rejected in unauthenticated mode")
	}
}
