package stele

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestScheduledMemoryReconcilesAcceptedWriteAfterResponseLoss(t *testing.T) {
	var mu sync.Mutex
	var keys []string
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/provider/retrieve" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result":{"matches":[]}}`))
			return
		}
		if r.URL.Path != "/v1/provider/events" {
			http.NotFound(w, r)
			return
		}
		_, _ = io.Copy(io.Discard, r.Body)
		mu.Lock()
		defer mu.Unlock()
		calls++
		keys = append(keys, r.Header.Get("Idempotency-Key"))
		if calls == 1 {
			// Simulate Stele committing the event while its response is lost.
			panic(http.ErrAbortHandler)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"event_id":"scheduled-event-1","replayed":true}`))
	}))
	defer server.Close()
	client, err := NewClient(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	request := ScheduledMemoryRequest{OccurrenceID: "occurrence-1", EffectKey: "schedule:occurrence-1:memory", ExecutionID: "execution-1", Content: "daily summary"}
	if _, err := client.ScheduledMemory(context.Background(), request); err == nil {
		t.Fatal("first write unexpectedly succeeded despite response loss")
	}
	out, err := client.ScheduledMemory(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(keys) != 2 || keys[0] != request.EffectKey || keys[1] != request.EffectKey {
		t.Fatalf("calls=%d keys=%#v", calls, keys)
	}
	if got := out.Events[1].Payload["event_id"]; got != "scheduled-event-1" {
		t.Fatalf("replayed event id=%v", got)
	}
}

func TestScheduledMemoryStopsOnProviderIdempotencyConflict(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/provider/events" {
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error":{"code":"idempotency_conflict"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"result":{"hits":[]}}`))
	}))
	defer server.Close()
	client, err := NewClient(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.ScheduledMemory(context.Background(), ScheduledMemoryRequest{OccurrenceID: "occurrence", EffectKey: "schedule:occurrence:memory", ExecutionID: "execution", Content: "changed"})
	if !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("error=%v, want ErrIdempotencyConflict", err)
	}
}

func TestScheduledMemoryUsesOccurrenceEffectKey(t *testing.T) {
	var keys []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		keys = append(keys, r.Header.Get("Idempotency-Key"))
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/provider/events" {
			_, _ = w.Write([]byte(`{"event_id":"event-1","replayed":false}`))
			return
		}
		_, _ = w.Write([]byte(`{"result":{"hits":[]}}`))
	}))
	defer server.Close()
	client, err := NewClient(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.ScheduledMemory(context.Background(), ScheduledMemoryRequest{OccurrenceID: "occurrence", EffectKey: "schedule:occurrence:memory", ExecutionID: "execution", Content: "content"})
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) == 0 || keys[0] != "schedule:occurrence:memory" {
		t.Fatalf("idempotency headers = %#v", keys)
	}
}
