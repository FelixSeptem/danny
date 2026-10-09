package parser

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServiceStagesSourceAndUsesHTTPJobLifecycle(t *testing.T) {
	var stagedObject string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/parser/jobs":
			var request Request
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatal(err)
			}
			stagedObject = request.StagingObjectID
			_ = json.NewEncoder(w).Encode(Job{Request: request, Status: StatusSucceeded, Result: &Result{JobID: request.JobID, ContentHash: request.ContentHash, CanonicalMarkdown: "# Parsed\n", UntrustedSource: true}})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	service := Service{Client: HTTPClient{BaseURL: server.URL}, Staging: StagingStore{Root: t.TempDir()}}
	result, err := service.Parse(context.Background(), "note.md", io.Reader(strings.NewReader("# source")))
	if err != nil {
		t.Fatal(err)
	}
	if result.CanonicalMarkdown != "# Parsed\n" || stagedObject == "" {
		t.Fatalf("result=%#v staged=%q", result, stagedObject)
	}
}
