package stele

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/FelixSeptem/danny/internal/storage"
)

func TestProjectionWorkerReplaysReferenceOnlyOutboxPayload(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"event_id":"event-1"}`))
	}))
	defer server.Close()
	client, err := NewClient(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(CompilationProjection{RunID: "run", GenerationID: 4, SourceIDs: []string{"source"}, DecisionDigest: "digest", ArtifactSummaries: []string{"summary"}})
	if err := client.ProjectOutbox(context.Background(), storage.OutboxMessage{Kind: storage.OutboxKindStele, IdempotencyKey: "compile:4:stele", Payload: payload}); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("calls=%d", calls)
	}
}
