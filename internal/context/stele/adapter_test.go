package stele

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/FelixSeptem/danny/internal/runtimeprobe"
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

func TestClientExposesScopedTaskAndRunReferences(t *testing.T) {
	client, err := NewClient("http://127.0.0.1:1", nil)
	if err != nil {
		t.Fatal(err)
	}
	var adapter runtimeprobe.SteleAdapter = client
	if adapter == nil {
		t.Fatal("stele adapter must implement the runtime probe boundary")
	}
	if _, err := client.References(context.Background(), runtimeprobe.ProbeRequest{CorrelationID: "corr", ParentID: "parent", Input: "reference", Capability: "references"}); err == nil {
		t.Fatal("References unexpectedly reached an unavailable endpoint")
	}
}

func TestClientReferencesUsesSafeProviderOperationTokens(t *testing.T) {
	var retrieveMetadata map[string]any
	var retrievePaths []string
	var eventTypes []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if r.URL.Path == "/v1/provider/events" {
			eventType, _ := body["event_type"].(string)
			eventTypes = append(eventTypes, eventType)
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
		content, _ := input["query"].(string)
		response := map[string]any{"result": map[string]any{"hits": []any{map[string]any{"memory": map[string]string{"memory_path": path, "content": content}}}}}
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
}
