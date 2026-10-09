package parser

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPClientUsesVersionedJSONContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/parser/jobs" || r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("unexpected request: %s %s %s", r.Method, r.URL.Path, r.Header.Get("Content-Type"))
		}
		var request Request
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.StagingObjectID != "object-1" {
			t.Fatalf("staging object = %q", request.StagingObjectID)
		}
		_ = json.NewEncoder(w).Encode(Job{Request: request, Status: StatusAccepted})
	}))
	defer server.Close()
	client := HTTPClient{BaseURL: server.URL}
	job, err := client.Submit(context.Background(), Request{ContractVersion: ContractVersion, JobID: "job-1", IdempotencyKey: "key-1", StagingObjectID: "object-1", SourceFingerprint: "source-1", ContentHash: "hash-1", Filename: "note.md"})
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != StatusAccepted {
		t.Fatalf("job status = %s", job.Status)
	}
}
