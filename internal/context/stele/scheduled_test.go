package stele

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

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
