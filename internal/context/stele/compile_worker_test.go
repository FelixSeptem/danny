package stele

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/FelixSeptem/danny/internal/storage"
)

func TestProjectionWorkerReconcilesAcceptedWriteAfterResponseLoss(t *testing.T) {
	var mu sync.Mutex
	seen := make(map[string]string)
	payloads := make(map[string]string)
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/provider/events" {
			http.NotFound(w, r)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
			return
		}
		key := r.Header.Get("Idempotency-Key")
		mu.Lock()
		defer mu.Unlock()
		calls++
		if key != "knowledge:generation:11" {
			t.Errorf("idempotency key=%q", key)
		}
		if prior, ok := payloads[key]; ok && prior != string(body) {
			t.Errorf("replayed payload changed: %q != %q", string(body), prior)
		}
		payloads[key] = string(body)
		eventID := seen[key]
		if eventID == "" {
			eventID = "accepted-event-1"
			seen[key] = eventID
		} else if got := seen[key]; got != eventID {
			t.Errorf("replay event id=%q, original=%q", eventID, got)
		}
		if calls == 1 {
			// The service committed the event, but the response was lost.
			panic(http.ErrAbortHandler)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"event_id":"accepted-event-1","replayed":true}`))
	}))
	defer server.Close()
	client, err := NewClient(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(CompilationProjection{RunID: "run-1", GenerationID: 11, DecisionDigest: "digest"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	store, err := storage.OpenSQLite(ctx, "file:stele-uncertain-response?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	commit, err := store.Compilations().Commit(ctx, storage.CompilationCommit{EffectKey: "compile:11", Manifest: []byte(`{"generation":11}`), StelePayload: payload})
	if err != nil {
		t.Fatal(err)
	}
	committed, err := store.Generations().Committed(ctx)
	if err != nil || committed.ID != commit.GenerationID {
		t.Fatalf("committed=%#v err=%v", committed, err)
	}
	message, err := store.Outbox().Claim(ctx, storage.OutboxKindStele, "stele-worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.ProjectOutbox(ctx, message); err == nil {
		t.Fatal("first delivery unexpectedly succeeded despite response loss")
	}
	if err := store.Outbox().Retry(ctx, message.ID, "stele-worker", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	retry, err := store.Outbox().ClaimByKey(ctx, storage.OutboxKindStele, message.IdempotencyKey, "stele-worker-2", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.ProjectOutbox(ctx, retry); err != nil {
		t.Fatal(err)
	}
	if err := store.Outbox().Ack(ctx, retry.ID, "stele-worker-2"); err != nil {
		t.Fatal(err)
	}
	result, err := client.ProjectCompilationResult(ctx, CompilationProjection{RunID: "run-1", GenerationID: 11, DecisionDigest: "digest"})
	if err != nil {
		t.Fatal(err)
	}
	committedAfterReplay, err := store.Generations().Committed(ctx)
	if err != nil || committedAfterReplay.ID != commit.GenerationID {
		t.Fatalf("committed after replay=%#v err=%v", committedAfterReplay, err)
	}
	if result.EventID != "accepted-event-1" || !result.Replayed || calls != 3 {
		t.Fatalf("result=%#v calls=%d", result, calls)
	}
}

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

func TestProjectionDeliveryFailureLeavesCommittedGenerationAndRetryIntent(t *testing.T) {
	ctx := context.Background()
	store, err := storage.OpenSQLite(ctx, "file:stele-delivery-failure?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	commit, err := store.Compilations().Commit(ctx, storage.CompilationCommit{EffectKey: "compile:failure", Manifest: []byte(`{"generation":1}`), StelePayload: []byte(`{"run_id":"run-1","generation_id":1,"decision_digest":"digest"}`)})
	if err != nil {
		t.Fatal(err)
	}
	message, err := store.Outbox().Claim(ctx, storage.OutboxKindStele, "stele-worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	client, err := NewClient(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if err := client.ProjectOutbox(ctx, message); err == nil {
		t.Fatal("delivery failure unexpectedly succeeded")
	}
	committed, err := store.Generations().Committed(ctx)
	if err != nil || committed.ID != commit.GenerationID {
		t.Fatalf("committed=%#v err=%v", committed, err)
	}
	if err := store.Outbox().Retry(ctx, message.ID, "stele-worker", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	retry, err := store.Outbox().ClaimByKey(ctx, storage.OutboxKindStele, message.IdempotencyKey, "stele-worker-2", time.Minute)
	if err != nil || retry.IdempotencyKey != message.IdempotencyKey {
		t.Fatalf("retry=%#v err=%v", retry, err)
	}
}
