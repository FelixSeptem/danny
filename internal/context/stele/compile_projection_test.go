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

func TestClientProjectsCompileReferenceWithoutArtifactBody(t *testing.T) {
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"event_id":"event-1"}`))
	}))
	defer server.Close()
	client, err := NewClient(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	err = client.ProjectCompilation(context.Background(), CompilationProjection{RunID: "run-1", GenerationID: 3, SourceIDs: []string{"source-1"}, DecisionDigest: "digest", ArtifactSummaries: []string{"summary reference"}})
	if err != nil {
		t.Fatal(err)
	}
	if got["event_type"] != "conversation.message" {
		t.Fatalf("event=%#v", got)
	}
	if got["memory_path"] != "knowledge/generations/3" {
		t.Fatalf("memory_path=%#v", got["memory_path"])
	}
	content, ok := got["content"].(string)
	if !ok || !strings.Contains(content, `"generation_id":3`) {
		t.Fatalf("content=%#v, want bounded serialized projection", got["content"])
	}
	metadataMap, ok := got["metadata_map"].(map[string]any)
	if !ok || metadataMap["generation_id"] != float64(3) {
		t.Fatalf("metadata_map=%#v", got["metadata_map"])
	}
	if _, ok := got["artifact_bodies"]; ok {
		t.Fatalf("artifact body leaked: %#v", got)
	}
}

func TestProjectCompilationResultPreservesSteleReplayIdentity(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		if calls == 1 {
			_, _ = w.Write([]byte(`{"event_id":"event-1","replayed":false}`))
			return
		}
		_, _ = w.Write([]byte(`{"event_id":"event-1","replayed":true}`))
	}))
	defer server.Close()
	client, err := NewClient(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	projection := CompilationProjection{RunID: "run-1", GenerationID: 3, SourceIDs: []string{"source-1"}, DecisionDigest: "digest"}
	first, err := client.ProjectCompilationResult(context.Background(), projection)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := client.ProjectCompilationResult(context.Background(), projection)
	if err != nil {
		t.Fatal(err)
	}
	if first.EventID != "event-1" || first.Replayed || replay.EventID != first.EventID || !replay.Replayed {
		t.Fatalf("first=%#v replay=%#v", first, replay)
	}
}

func TestCompilationProbeCorrelatesReplayAndExactPathReference(t *testing.T) {
	var projection CompilationProjection
	var eventCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		switch r.URL.Path {
		case "/v1/provider/events":
			eventCalls++
			content, _ := body["content"].(string)
			if err := json.Unmarshal([]byte(content), &projection); err != nil {
				t.Fatal(err)
			}
			if eventCalls == 1 {
				_, _ = w.Write([]byte(`{"event_id":"event-compile","replayed":false}`))
				return
			}
			_, _ = w.Write([]byte(`{"event_id":"event-compile","replayed":true}`))
		case "/v1/provider/retrieve":
			content, _ := json.Marshal(projection)
			response := map[string]any{"result": map[string]any{"hits": []any{map[string]any{"memory": map[string]string{"memory_path": "knowledge/generations/987654321", "content": string(content)}}}}}
			_ = json.NewEncoder(w).Encode(response)
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	client, err := NewClient(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	out, err := client.Compilation(context.Background(), runtimeprobe.ProbeRequest{CorrelationID: "compile-corr", ParentID: "compile-parent", Capability: "compilation"})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Events) != 3 || out.Events[1].Payload["replayed"] != "true" || out.Events[1].Payload["reference_found"] != "true" {
		t.Fatalf("events=%#v", out.Events)
	}
}
