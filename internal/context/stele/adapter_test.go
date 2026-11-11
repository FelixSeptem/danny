package stele

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FelixSeptem/danny/internal/runtimeboundary"
	"github.com/FelixSeptem/danny/internal/runtimeprobe"
	"github.com/FelixSeptem/danny/internal/storage"
)

func TestClientUsesPublicProviderContextEndpoint(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if r.Header.Get("Idempotency-Key") != "corr-1" {
			t.Errorf("missing idempotency key")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"metadata":{"request_id":"corr-1"},"result":{}}`))
	}))
	defer server.Close()
	client, err := NewClient(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.BindingID, client.SessionID = "binding", "session"
	out, err := client.Context(context.Background(), runtimeprobe.ProbeRequest{CorrelationID: "corr-1", ParentID: "parent", Input: "query", Capability: "context"})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/v1/provider/context" {
		t.Fatalf("path = %q", gotPath)
	}
	bundle := runtimeprobe.Bundle{SchemaVersion: runtimeprobe.EvidenceSchemaVersion, BaymaxVersion: "baymax", SteleVersion: client.Version(), Events: out.Events}
	if err := bundle.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestOpenAPIInventoryIsPinnedPublicContract(t *testing.T) {
	spec := OpenAPIInventory()
	for _, endpoint := range []string{"/v1/provider/runtime", "/v1/provider/context", "/v1/provider/intents", "/v1/provider/status"} {
		if !strings.Contains(spec, endpoint) {
			t.Fatalf("OpenAPI inventory missing %s", endpoint)
		}
	}
	var ignored any
	if err := json.Unmarshal([]byte(`{"ok":true}`), &ignored); err != nil {
		t.Fatal(err)
	}
}

func TestPinnedContextFixtureMatchesOpenAPIAndProjectionBoundary(t *testing.T) {
	fixturePath := filepath.Join("..", "..", "..", "testdata", "runtimeprobe", "stele-context-reference.json")
	raw, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read pinned context fixture: %v", err)
	}
	var response map[string]any
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatalf("decode pinned context fixture: %v", err)
	}
	metadata, _ := response["metadata"].(map[string]any)
	if metadata["schema_version"] != "provider-v1" || metadata["operation_id"] != "providerAssembleContext" {
		t.Fatalf("fixture metadata=%#v", metadata)
	}

	var gotInput map[string]any
	var gotHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		gotInput, _ = body["input"].(map[string]any)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
	}))
	defer server.Close()

	client, err := NewClient(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.Tenant, client.Project, client.Namespace = "tenant", "project", "namespace"
	client.BindingID, client.SessionID = "binding-1", "session-1"
	projection, err := client.BoundedProjection(context.Background(), ProjectionRequest{
		RunID: "fixture-run-1", Query: "current task", Budget: 2, PathPrefix: "tasks/demo",
		TurnID: "turn-fixture-1", SessionID: "session-1", Scope: "tenant/project/namespace",
		ProjectionIntent: "task_context", PolicyDigest: "policy-fixture", MaxBytes: 4096, MaxReferences: 2,
	})
	if err != nil {
		t.Fatalf("fixture projection: %v", err)
	}
	if len(projection.References) != 1 || projection.References[0].MemoryPath != "tasks/demo/item" {
		t.Fatalf("references=%#v", projection.References)
	}
	if len(projection.References[0].Citation) != 1 || projection.References[0].Citation[0].SourceKind != "canonical_memory" {
		t.Fatalf("citations=%#v", projection.References[0].Citation)
	}
	if projection.Continuity.ReadSemantics != "fresh" || projection.Continuity.Outcome != "not_requested" {
		t.Fatalf("continuity=%#v", projection.Continuity)
	}
	if projection.Budget.Usage["response_bytes"] == nil || projection.Budget.Usage["citations"] == nil {
		t.Fatalf("budget usage=%#v", projection.Budget.Usage)
	}
	if gotInput["path_prefix"] != "tasks/demo" {
		t.Fatalf("path_prefix=%#v", gotInput["path_prefix"])
	}
	if gotInput["budget"] != float64(2) || gotInput["turn_id"] != "turn-fixture-1" || gotInput["response_mode"] != "reference_only" {
		t.Fatalf("bounded input=%#v", gotInput)
	}
	limits, _ := gotInput["budget_limits"].(map[string]any)
	if limits["max_response_bytes"] != float64(4096) || limits["max_citations"] != float64(2) {
		t.Fatalf("budget limits=%#v", limits)
	}
	if gotHeaders.Get("X-Stele-Tenant") != "tenant" || gotHeaders.Get("X-Stele-Project") != "project" || gotHeaders.Get("X-Stele-Namespace") != "namespace" || gotHeaders.Get("X-Stele-Runtime-Binding") != "binding-1" || gotHeaders.Get("X-Stele-Runtime-Session") != "session-1" {
		t.Fatalf("scope/runtime headers=%v", gotHeaders)
	}
	for _, unsupported := range []string{"role", "references"} {
		if _, ok := gotInput[unsupported]; ok {
			t.Fatalf("undocumented request field %q=%#v", unsupported, gotInput[unsupported])
		}
	}

	spec := OpenAPIInventory()
	inputStart := strings.Index(spec, "ProviderContextInput:")
	if inputStart < 0 {
		t.Fatal("pinned OpenAPI does not expose ProviderContextInput")
	}
	inputEnd := strings.Index(spec[inputStart:], "    ProviderContextDigest:")
	if inputEnd < 0 {
		t.Fatal("pinned OpenAPI does not delimit ProviderContextInput")
	}
	inputSchema := spec[inputStart : inputStart+inputEnd]
	for _, field := range []string{"path:", "path_prefix:", "budget_limits:", "turn_id:", "projection_intent:", "response_mode:"} {
		if !strings.Contains(inputSchema, field) {
			t.Fatalf("ProviderContextInput missing %s", field)
		}
	}
	for _, unsupported := range []string{"role:", "references:"} {
		if strings.Contains(inputSchema, unsupported) {
			t.Fatalf("ProviderContextInput unexpectedly supports %s", unsupported)
		}
	}
}

func TestClientExposesScopedTaskAndRunReferences(t *testing.T) {
	client, err := NewClient("http://127.0.0.1:1", nil)
	if err != nil {
		t.Fatal(err)
	}
	client.Tenant, client.Project, client.Namespace = "tenant", "project", "namespace"
	if _, err := client.References(context.Background(), runtimeprobe.ProbeRequest{CorrelationID: "corr", ParentID: "parent", Input: "reference", Capability: "references"}); err == nil {
		t.Fatal("References unexpectedly reached an unavailable endpoint")
	}
}

func TestClientReferencesUsesSafeProviderOperationTokens(t *testing.T) {
	var retrieveMetadata map[string]any
	var retrievePaths []string
	var eventTypes []string
	var eventSourceMetadata []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if r.URL.Path == "/v1/provider/events" {
			eventType, _ := body["event_type"].(string)
			eventTypes = append(eventTypes, eventType)
			sourceMetadata, _ := body["source_metadata"].(map[string]any)
			eventSourceMetadata = append(eventSourceMetadata, sourceMetadata)
			_, _ = w.Write([]byte(`{"event_id":"event-1"}`))
			return
		}
		if r.URL.Path != "/v1/provider/retrieve" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		retrieveMetadata, _ = body["metadata"].(map[string]any)
		input, _ := body["input"].(map[string]any)
		path, _ := input["path"].(string)
		if path != "" {
			retrievePaths = append(retrievePaths, path)
		}
		response := map[string]any{"result": map[string]any{"disclosure_profile": "memory-reference-v1", "response_mode": "reference_only", "matches": []any{map[string]any{"memory_id": "memory-1", "memory_path": path, "class": "episodic", "state": "active", "version": 1, "source_metadata": map[string]string{"version": "source-metadata-v1", "trust": "untrusted", "classification": "low"}}}}}
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Fatal(err)
		}
	}))
	defer server.Close()
	client, err := NewClient(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.References(context.Background(), runtimeprobe.ProbeRequest{CorrelationID: "corr", ParentID: "parent", Input: "reference", Capability: "references"})
	if err != nil {
		t.Fatal(err)
	}
	requestID, _ := retrieveMetadata["request_id"].(string)
	idempotencyKey, _ := retrieveMetadata["idempotency_key"].(string)
	for name, value := range map[string]string{"request_id": requestID, "idempotency_key": idempotencyKey} {
		if value == "" || strings.Contains(value, "/") {
			t.Fatalf("%s=%q contains an invalid path separator", name, value)
		}
	}
	if len(retrievePaths) != 2 || retrievePaths[0] != "runtimeprobe/tasks/corr:task" || retrievePaths[1] != "runtimeprobe/runs/corr:run" {
		t.Fatalf("retrieve paths=%v, want original task and run memory paths", retrievePaths)
	}
	if len(eventTypes) != 2 || eventTypes[0] != "conversation.message" || eventTypes[1] != "conversation.message" {
		t.Fatalf("event types=%v, want conversation.message for both canonical references", eventTypes)
	}
	for index, sourceMetadata := range eventSourceMetadata {
		if sourceMetadata["version"] != "source-metadata-v1" || sourceMetadata["trust"] != "untrusted" || sourceMetadata["classification"] != "medium" {
			t.Fatalf("event %d source_metadata=%#v, want explicit untrusted/medium disclosure", index, sourceMetadata)
		}
	}
}

func TestClientReferencesRetriesUntilGovernedMemoryIsVisible(t *testing.T) {
	var retrieveAttempts = map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if r.URL.Path == "/v1/provider/events" {
			_, _ = w.Write([]byte(`{"event_id":"event-1"}`))
			return
		}
		if r.URL.Path != "/v1/provider/retrieve" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		input, _ := body["input"].(map[string]any)
		memoryPath, _ := input["path"].(string)
		retrieveAttempts[memoryPath]++
		if retrieveAttempts[memoryPath] == 1 {
			_, _ = w.Write([]byte(`{"result":{"matches":[]}}`))
			return
		}
		response := map[string]any{"result": map[string]any{"matches": []any{map[string]any{"memory_id": "memory-1", "memory_path": memoryPath}}}}
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Fatal(err)
		}
	}))
	defer server.Close()
	client, err := NewClient(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.References(context.Background(), runtimeprobe.ProbeRequest{CorrelationID: "corr", ParentID: "parent", Input: "reference", Capability: "references"})
	if err != nil {
		t.Fatalf("References() error = %v", err)
	}
	if retrieveAttempts["runtimeprobe/tasks/corr:task"] < 2 || retrieveAttempts["runtimeprobe/runs/corr:run"] < 2 {
		t.Fatalf("retrieve attempts = %#v, want at least two attempts per path", retrieveAttempts)
	}
}

func TestCreateTurnUsesBindingOwnedSessionNotCallerBody(t *testing.T) {
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/provider/turns" {
			t.Fatalf("path=%q", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"metadata":{"request_id":"turn-1","operation_id":"context_turn","schema_version":"provider-v1"},"result":{"turn_id":"turn-1"}}`))
	}))
	defer server.Close()
	client, err := NewClient(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.SessionID = "session-bound"
	turn, err := client.CreateTurn(context.Background(), "turn-1", "query", 4)
	if err != nil {
		t.Fatal(err)
	}
	if turn.ID != "turn-1" {
		t.Fatalf("turn=%+v", turn)
	}
	input := got["input"].(map[string]any)
	if _, exists := input["session_id"]; exists {
		t.Fatalf("turn input contains undocumented session_id: %#v", input)
	}
	if input["query"] != "query" || input["context_budget"] != float64(4) {
		t.Fatalf("turn input=%#v", input)
	}
}

func TestClientBuildsBoundedReferenceFirstProjection(t *testing.T) {
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		writeContextResponse(t, w, []byte(`{"result":{"turn_id":"turn-1","response_mode":"reference_only","context_digest":{"contract_version":"context-turn-digest-v1","algorithm":"sha-256","canonicalization_version":"context-turn-canonical-v1","selection_policy_version":"context-selection-v1","selection_policy_fingerprint":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","value":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},"continuity":{"contract_version":"context-continuity-v1","read_semantics":"fresh","outcome":"not_requested"},"disclosure":{"profile":"memory-reference-v1","policy_version":"context-disclosure-v1","projection_intent":"task_context","source_metadata_version":"source-metadata-v1"},"budget_accounting":{"contract_version":"context-budget-v1","packing_policy_version":"context-packing-v1","requested":{"max_items":1,"max_content_bytes":null,"max_response_bytes":null,"max_citations":null},"effective":{"max_items":1,"max_content_bytes":1024,"max_response_bytes":4096,"max_citations":4},"usage":{"items":1,"content_bytes":0,"response_bytes":100,"citations":1},"selection":{"outcome":"selected","omission_reasons":[],"unsupported_sections":[]}},"profile":[],"recent_session":[],"recent_episodes":[{"reference":{"kind":"memory","id":"source-1","version":"1","memory_path":"tasks/one","class":"episodic","availability":"available","source_metadata":{"version":"source-metadata-v1","trust":"untrusted","classification":"low"}},"citations":[{"source_kind":"canonical_memory","reference":"source-1","version":"1","availability":"available"}]}],"relevant_summaries":[],"related_entities":[],"citations":[{"source_kind":"canonical_memory","reference":"source-1","version":"1","availability":"available"}]},"citations":[{"source_kind":"canonical_memory","reference":"source-1","version":"1","availability":"available"}]}`))
	}))
	defer server.Close()
	client, err := NewClient(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.Tenant, client.Project, client.Namespace = "tenant", "project", "namespace"
	client.BindingID, client.SessionID = "binding-1", "session-1"
	projection, err := client.BoundedProjection(context.Background(), ProjectionRequest{RunID: "run-1", Query: "task", Budget: 1, Role: "section_analyst", TurnID: "turn-1", SessionID: "session-1", Scope: "tenant/project/namespace", PolicyDigest: "policy", MaxBytes: 4096, MaxReferences: 4})
	if err != nil {
		t.Fatal(err)
	}
	if projection.Digest == "" || len(projection.References) != 1 || projection.References[0].ID != "source-1" || projection.SourceDigest != "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" {
		t.Fatalf("projection=%#v", projection)
	}
	input := got["input"].(map[string]any)
	if input["budget"].(float64) != 1 || input["turn_id"].(string) != "turn-1" || input["projection_intent"].(string) != "task_context" {
		t.Fatalf("input=%#v", input)
	}
	if _, ok := input["role"]; ok {
		t.Fatalf("undocumented role field leaked into request: %#v", input)
	}
	if projection.Content != "" {
		t.Fatal("bounded projection copied full store content")
	}
}

func TestBoundedProjectionRejectsTurnOrProvenanceDrift(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		writeContextResponse(t, w, []byte(`{"result":{"turn_id":"wrong-turn","response_mode":"reference_only","context_digest":{"contract_version":"context-turn-digest-v1","algorithm":"sha-256","canonicalization_version":"context-turn-canonical-v1","selection_policy_version":"context-selection-v1","selection_policy_fingerprint":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","value":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},"continuity":{"contract_version":"context-continuity-v1","read_semantics":"fresh","outcome":"not_requested"},"disclosure":{"profile":"memory-reference-v1","policy_version":"context-disclosure-v1","projection_intent":"task_context","source_metadata_version":"source-metadata-v1"},"budget_accounting":{"contract_version":"context-budget-v1","packing_policy_version":"context-packing-v1","requested":{"max_items":1,"max_content_bytes":null,"max_response_bytes":null,"max_citations":null},"effective":{"max_items":1,"max_content_bytes":1024,"max_response_bytes":4096,"max_citations":4},"usage":{"items":0,"content_bytes":0,"response_bytes":100,"citations":0},"selection":{"outcome":"empty","omission_reasons":[],"unsupported_sections":[]}},"profile":[],"recent_session":[],"recent_episodes":[],"relevant_summaries":[],"related_entities":[],"citations":[]},"citations":[]}`))
	}))
	defer server.Close()
	client, _ := NewClient(server.URL, server.Client())
	client.Tenant, client.Project, client.Namespace = "tenant", "project", "namespace"
	client.BindingID, client.SessionID = "binding-1", "session-1"
	_, err := client.BoundedProjection(context.Background(), ProjectionRequest{RunID: "run-1", Query: "task", Budget: 1, TurnID: "turn-1", SessionID: "session-1", Scope: "tenant/project/namespace", PolicyDigest: "policy", MaxBytes: 4096, MaxReferences: 4})
	if err == nil || !strings.Contains(err.Error(), "turn") {
		t.Fatalf("turn drift error = %v", err)
	}
}

func TestBoundedProjectionRejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		writeContextResponse(t, w, []byte(`{"result":`+strings.Repeat("x", 2048)+`}`))
	}))
	defer server.Close()
	client, _ := NewClient(server.URL, server.Client())
	client.Tenant, client.Project, client.Namespace = "tenant", "project", "namespace"
	client.BindingID, client.SessionID = "binding-1", "session-1"
	_, err := client.BoundedProjection(context.Background(), ProjectionRequest{RunID: "run-1", Query: "task", Budget: 1, TurnID: "turn-1", SessionID: "session-1", Scope: "tenant/project/namespace", PolicyDigest: "policy", MaxBytes: 128, MaxReferences: 1})
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized response error = %v", err)
	}
}

func TestBoundedProjectionMapsDocumentedPathSelectors(t *testing.T) {
	var gotInput map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		gotInput, _ = body["input"].(map[string]any)
		w.Header().Set("Content-Type", "application/json")
		writeContextResponse(t, w, []byte(`{"result":{"turn_id":"turn-1","response_mode":"reference_only","context_digest":{"contract_version":"context-turn-digest-v1","algorithm":"sha-256","canonicalization_version":"context-turn-canonical-v1","selection_policy_version":"context-selection-v1","selection_policy_fingerprint":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","value":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},"continuity":{"contract_version":"context-continuity-v1","read_semantics":"fresh","outcome":"not_requested"},"disclosure":{"profile":"memory-reference-v1","policy_version":"context-disclosure-v1","projection_intent":"task_context","source_metadata_version":"source-metadata-v1"},"budget_accounting":{"contract_version":"context-budget-v1","packing_policy_version":"context-packing-v1","requested":{"max_items":1,"max_content_bytes":null,"max_response_bytes":4096,"max_citations":4},"effective":{"max_items":1,"max_content_bytes":0,"max_response_bytes":4096,"max_citations":4},"usage":{"items":0,"content_bytes":0,"response_bytes":1369,"citations":0},"selection":{"outcome":"empty","omission_reasons":[],"unsupported_sections":[]}},"profile":[],"recent_session":[],"recent_episodes":[],"relevant_summaries":[],"related_entities":[],"citations":[]}}`))
	}))
	defer server.Close()
	client, err := NewClient(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.Tenant, client.Project, client.Namespace = "tenant", "project", "namespace"
	client.BindingID, client.SessionID = "binding-1", "session-1"
	_, err = client.BoundedProjection(context.Background(), ProjectionRequest{RunID: "run-1", Query: "task", Budget: 1, MemoryPath: "tasks/one", TurnID: "turn-1", SessionID: "session-1", Scope: "tenant/project/namespace", PolicyDigest: "policy", MaxBytes: 4096, MaxReferences: 4})
	if err != nil {
		t.Fatal(err)
	}
	if gotInput["path"] != "tasks/one" {
		t.Fatalf("path=%#v", gotInput["path"])
	}
	if _, ok := gotInput["path_prefix"]; ok {
		t.Fatalf("path_prefix unexpectedly present: %#v", gotInput)
	}
}

func TestBoundedProjectionRejectsAmbiguousOrOversizedPathSelectors(t *testing.T) {
	client, err := NewClient("http://127.0.0.1:1", nil)
	if err != nil {
		t.Fatal(err)
	}
	base := ProjectionRequest{RunID: "run-1", Query: "task", Budget: 1, TurnID: "turn-1", SessionID: "session-1", Scope: "tenant/project/namespace", PolicyDigest: "policy", MaxBytes: 4096, MaxReferences: 4}
	base.MemoryPath, base.PathPrefix = "tasks/one", "tasks"
	if _, err := client.BoundedProjection(context.Background(), base); err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("ambiguous path error=%v", err)
	}
	base.MemoryPath, base.PathPrefix = strings.Repeat("x", 513), ""
	if _, err := client.BoundedProjection(context.Background(), base); err == nil || !strings.Contains(err.Error(), "path") {
		t.Fatalf("oversized path error=%v", err)
	}
	for _, unsafePath := range []string{"tasks/../secret", `tasks\\secret`, "tasks/*", "tasks/%2Fsecret", "tasks//secret"} {
		base.MemoryPath = unsafePath
		if _, err := client.BoundedProjection(context.Background(), base); err == nil || !strings.Contains(err.Error(), "path") {
			t.Fatalf("unsafe path %q error=%v", unsafePath, err)
		}
	}
}

func TestBoundedProjectionIssuesAndComparesContinuityReceipt(t *testing.T) {
	var inputs []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		input, _ := body["input"].(map[string]any)
		inputs = append(inputs, input)
		continuity := `{"contract_version":"context-continuity-v1","read_semantics":"fresh","outcome":"issued","reference":"ctxc_0123456789abcdef0123456789abcdef","expires_at":"2026-10-22T00:00:00Z"}`
		if len(inputs) == 2 {
			continuity = `{"contract_version":"context-continuity-v1","read_semantics":"fresh_compare","outcome":"unchanged","reference":"ctxc_0123456789abcdef0123456789abcdef","expires_at":"2026-10-22T00:00:00Z"}`
		}
		w.Header().Set("Content-Type", "application/json")
		writeContextResponse(t, w, []byte(`{"result":{"turn_id":"turn-1","response_mode":"reference_only","context_digest":{"contract_version":"context-turn-digest-v1","algorithm":"sha-256","canonicalization_version":"context-turn-canonical-v1","selection_policy_version":"context-selection-v1","selection_policy_fingerprint":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","value":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},"continuity":`+continuity+`,"disclosure":{"profile":"memory-reference-v1","policy_version":"context-disclosure-v1","projection_intent":"task_context","source_metadata_version":"source-metadata-v1"},"budget_accounting":{"contract_version":"context-budget-v1","packing_policy_version":"context-packing-v1","requested":{"max_items":1,"max_content_bytes":null,"max_response_bytes":4096,"max_citations":4},"effective":{"max_items":1,"max_content_bytes":0,"max_response_bytes":4096,"max_citations":4},"usage":{"items":0,"content_bytes":0,"response_bytes":1369,"citations":0},"selection":{"outcome":"empty","omission_reasons":[],"unsupported_sections":[]}},"profile":[],"recent_session":[],"recent_episodes":[],"relevant_summaries":[],"related_entities":[],"citations":[]}}`))
	}))
	defer server.Close()
	client, err := NewClient(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.Tenant, client.Project, client.Namespace = "tenant", "project", "namespace"
	base := ProjectionRequest{RunID: "run-1", Query: "task", Budget: 1, TurnID: "turn-1", SessionID: "session-1", Scope: "tenant/project/namespace", PolicyDigest: "policy", MaxBytes: 4096, MaxReferences: 4, ContinuityMode: "issue"}
	first, err := client.BoundedProjection(context.Background(), base)
	if err != nil {
		t.Fatal(err)
	}
	if first.Continuity.Reference != "ctxc_0123456789abcdef0123456789abcdef" || first.Continuity.ReadSemantics != "fresh" {
		t.Fatalf("first continuity=%#v", first.Continuity)
	}
	base.ContinuityMode, base.ContinuityReference = "compare", first.Continuity.Reference
	second, err := client.BoundedProjection(context.Background(), base)
	if err != nil {
		t.Fatal(err)
	}
	if second.Continuity.ReadSemantics != "fresh_compare" || second.Continuity.Outcome != "unchanged" {
		t.Fatalf("second continuity=%#v", second.Continuity)
	}
	if inputs[0]["continuity"].(map[string]any)["mode"] != "issue" || inputs[1]["continuity"].(map[string]any)["reference"] != first.Continuity.Reference {
		t.Fatalf("inputs=%#v", inputs)
	}
}

func TestAssembleRejectsNonItemBudgetBeforeHTTP(t *testing.T) {
	client, _ := NewClient("http://127.0.0.1:1", nil)
	_, err := client.Assemble(context.Background(), runtimeboundary.ContextRequest{Version: runtimeboundary.ContextContractV1, RunID: "run", Scope: "tenant/project/ns", SessionID: "session", TurnID: "turn", Query: "q", Budget: runtimeboundary.ContextBudget{Unit: runtimeboundary.BudgetTokens, Limit: 1}, MaxBytes: 128, MaxReferences: 1, PolicyDigest: "policy"})
	if err == nil || !strings.Contains(err.Error(), "item budgets") {
		t.Fatalf("budget unit error = %v", err)
	}
}

func TestPersistProjectionStoresDannyDigestAndSteleDigestSeparately(t *testing.T) {
	ctx := context.Background()
	store, err := storage.OpenSQLite(ctx, "file:stele-projection-persist?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	p := BoundedProjection{Digest: "app-digest", SourceDigest: "stele-digest", Scope: "tenant/project/ns", SessionID: "session", TurnID: "turn", BindingID: "binding", RunID: "run", Continuity: ContinuityEvidence{ContractVersion: "context-continuity-v1", Reference: "continuity-1", ExpiresAt: "2026-10-21T00:00:00Z"}, Envelope: runtimeboundary.ContextEnvelope{Version: runtimeboundary.ContextContractV1, RunID: "run", Scope: "tenant/project/ns", SessionID: "session", TurnID: "turn", SourceDigest: "stele-digest", ProjectionDigest: "app-digest", PolicyDigest: "policy", Budget: runtimeboundary.ContextBudget{Unit: runtimeboundary.BudgetItems, Limit: 1}, DiagnosticsState: runtimeboundary.DiagnosticsOmitted}}
	got, err := PersistProjection(ctx, store.ContextProjections(), p)
	if err != nil {
		t.Fatal(err)
	}
	if got.Digest != p.Digest || got.SourceDigest != p.SourceDigest || got.Scope != p.Scope {
		t.Fatalf("snapshot=%#v", got)
	}
	if got.ContinuityContractVersion != p.Continuity.ContractVersion || got.ContinuityReference != p.Continuity.Reference || got.ContinuityExpiresAt != p.Continuity.ExpiresAt {
		t.Fatalf("continuity=%#v", got)
	}
}

func TestProviderContextRejectsUnboundDigestDisclosureAndBudgetDrift(t *testing.T) {
	request := ProjectionRequest{Budget: 2, TurnID: "turn-1", ProjectionIntent: "task_context", MaxBytes: 4096, MaxReferences: 4}
	valid := providerContextResult{
		TurnID: "turn-1", ResponseMode: "reference_only",
		Continuity: ContinuityEvidence{ContractVersion: "context-continuity-v1", ReadSemantics: "fresh", Outcome: "not_requested"},
		Disclosure: DisclosureEvidence{Profile: "memory-reference-v1", PolicyVersion: "context-disclosure-v1", ProjectionIntent: "task_context", SourceMetadataVersion: "source-metadata-v1"},
		Budget:     BudgetEvidence{ContractVersion: "context-budget-v1", PackingPolicyVersion: "context-packing-v1", Requested: map[string]any{"max_items": 2, "max_content_bytes": nil, "max_response_bytes": 4096, "max_citations": 4}, Effective: map[string]any{"max_items": 2, "max_content_bytes": 0, "max_response_bytes": 4096, "max_citations": 4}, Usage: map[string]any{"items": 0, "content_bytes": 0, "response_bytes": 1024, "citations": 0}, Selection: SelectionEvidence{Outcome: "empty"}},
	}
	valid.ContextDigest.ContractVersion = "context-turn-digest-v1"
	valid.ContextDigest.Algorithm = "sha-256"
	valid.ContextDigest.CanonicalizationVersion = "context-turn-canonical-v1"
	valid.ContextDigest.SelectionPolicyVersion = "context-selection-v1"
	valid.ContextDigest.SelectionPolicyFingerprint = strings.Repeat("a", 64)
	valid.ContextDigest.Value = strings.Repeat("b", 64)
	if err := valid.validate(request); err != nil {
		t.Fatalf("valid response rejected: %v", err)
	}

	unbound := valid
	unbound.ContextDigest.ContractVersion = "context-digest-v1"
	if err := unbound.validate(request); err == nil || !strings.Contains(err.Error(), "digest") {
		t.Fatalf("unbound digest error=%v", err)
	}

	disclosureDrift := valid
	disclosureDrift.Disclosure.ProjectionIntent = "handoff"
	if err := disclosureDrift.validate(request); err == nil || !strings.Contains(err.Error(), "disclosure") {
		t.Fatalf("disclosure drift error=%v", err)
	}

	budgetDrift := valid
	budgetDrift.Budget.Usage = map[string]any{"items": 0, "response_bytes": 4097, "citations": 0}
	if err := budgetDrift.validate(request); err == nil || (!strings.Contains(err.Error(), "budget") && !strings.Contains(err.Error(), "response bytes")) {
		t.Fatalf("budget drift error=%v", err)
	}
}

func TestProviderContextRequiresCompleteBudgetAccounting(t *testing.T) {
	valid := validProviderContextResult()
	request := ProjectionRequest{Budget: 2, TurnID: "turn-1", ProjectionIntent: "task_context", MaxBytes: 4096, MaxReferences: 4}

	missingEffectiveBytes := valid
	missingEffectiveBytes.Budget.Effective = map[string]any{"max_items": 2, "max_citations": 4}
	if err := missingEffectiveBytes.validate(request); err == nil || !strings.Contains(err.Error(), "byte budget") {
		t.Fatalf("missing effective byte bound error=%v", err)
	}

	missingCitationUsage := valid
	missingCitationUsage.Budget.Usage = map[string]any{"items": 0, "content_bytes": 0, "response_bytes": 100}
	if err := missingCitationUsage.validate(request); err == nil || !strings.Contains(err.Error(), "citation") {
		t.Fatalf("missing citation usage error=%v", err)
	}
}

func TestProviderContextRejectsReferencesOutsideRequestedPath(t *testing.T) {
	request := ProjectionRequest{Budget: 2, TurnID: "turn-1", ProjectionIntent: "task_context", MaxBytes: 4096, MaxReferences: 4, MemoryPath: "tasks/allowed"}
	response := validProviderContextResult()
	response.Budget.Usage = map[string]any{"items": 1, "content_bytes": 0, "response_bytes": 1024, "citations": 1}
	response.RecentEpisodes = []providerReferenceItem{{
		Reference: ProjectionReference{ID: "ref-1", Kind: "memory", Version: "1", MemoryPath: "tasks/other", Class: "episodic", Availability: "available", Source: SourceMetadata{Version: "source-metadata-v1", Trust: "untrusted", Classification: "low"}},
		Citations: []ProjectionCitation{{SourceKind: "canonical_memory", Reference: "ref-1", Version: "1", Availability: "available"}},
	}}
	if err := response.validate(request); err == nil || !strings.Contains(err.Error(), "path") {
		t.Fatalf("foreign memory path error=%v", err)
	}
}

func TestClientRejectsProviderContextWithoutRequiredResponseMetadata(t *testing.T) {
	result := validProviderContextResult()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"result": result, "citations": []ProjectionCitation{}})
	}))
	defer server.Close()
	client, err := NewClient(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.Tenant, client.Project, client.Namespace = "tenant", "project", "namespace"
	_, err = client.BoundedProjection(context.Background(), ProjectionRequest{RunID: "run-1", Query: "task", Budget: 2, TurnID: "turn-1", SessionID: "session-1", Scope: "tenant/project/namespace", MaxBytes: 4096, MaxReferences: 4})
	if err == nil || !strings.Contains(err.Error(), "metadata") {
		t.Fatalf("missing provider response metadata error=%v", err)
	}
}

func validProviderContextResult() providerContextResult {
	result := providerContextResult{
		TurnID: "turn-1", ResponseMode: "reference_only",
		Continuity: ContinuityEvidence{ContractVersion: "context-continuity-v1", ReadSemantics: "fresh", Outcome: "not_requested"},
		Disclosure: DisclosureEvidence{Profile: "memory-reference-v1", PolicyVersion: "context-disclosure-v1", ProjectionIntent: "task_context", SourceMetadataVersion: "source-metadata-v1"},
		Budget:     BudgetEvidence{ContractVersion: "context-budget-v1", PackingPolicyVersion: "context-packing-v1", Requested: map[string]any{"max_items": 2, "max_content_bytes": nil, "max_response_bytes": 4096, "max_citations": 4}, Effective: map[string]any{"max_items": 2, "max_content_bytes": 0, "max_response_bytes": 4096, "max_citations": 4}, Usage: map[string]any{"items": 0, "content_bytes": 0, "response_bytes": 1024, "citations": 0}, Selection: SelectionEvidence{Outcome: "empty"}},
	}
	result.ContextDigest.ContractVersion = "context-turn-digest-v1"
	result.ContextDigest.Algorithm = "sha-256"
	result.ContextDigest.CanonicalizationVersion = "context-turn-canonical-v1"
	result.ContextDigest.SelectionPolicyVersion = "context-selection-v1"
	result.ContextDigest.SelectionPolicyFingerprint = strings.Repeat("a", 64)
	result.ContextDigest.Value = strings.Repeat("b", 64)
	return result
}

func writeContextResponse(t *testing.T, w http.ResponseWriter, raw []byte) {
	t.Helper()
	var envelope map[string]any
	if err := json.Unmarshal(raw, &envelope); err != nil {
		_, _ = w.Write(raw)
		return
	}
	result, _ := envelope["result"].(map[string]any)
	if result == nil {
		_, _ = w.Write(raw)
		return
	}
	envelope["metadata"] = map[string]any{"request_id": "run-1", "operation_id": "providerAssembleContext", "schema_version": "provider-v1"}
	if _, ok := envelope["citations"]; !ok {
		envelope["citations"] = result["citations"]
	}
	accounting, _ := result["budget_accounting"].(map[string]any)
	if accounting != nil {
		requested, _ := accounting["requested"].(map[string]any)
		if requested != nil {
			requested["max_content_bytes"] = nil
			requested["max_response_bytes"] = 4096
			requested["max_citations"] = 4
		}
		for i := 0; i < 4; i++ {
			encoded, err := json.Marshal(envelope)
			if err != nil {
				t.Fatal(err)
			}
			usage, _ := accounting["usage"].(map[string]any)
			if usage == nil {
				break
			}
			bytes := len(encoded)
			if usage["response_bytes"] == float64(bytes) {
				break
			}
			usage["response_bytes"] = bytes
		}
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write(encoded)
}
