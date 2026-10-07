package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/FelixSeptem/danny/internal/runtimeprobe"
)

func TestLiveSmokeUsesConfiguredOpenAICompatibleEndpointAndRedactsCredential(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/responses" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"resp-probe","output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}]}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"context_id":"ctx-probe"}`))
	}))
	defer server.Close()

	t.Setenv("OPENAI_BASE_URL", server.URL)
	t.Setenv("OPENAI_API_KEY", "sentinel-openai-key")
	t.Setenv("OPENAI_MODEL", "probe-model")
	t.Setenv("STELE_URL", server.URL)

	bundle, err := liveSmoke(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := bundle.Validate(); err != nil {
		t.Fatalf("live bundle invalid: %v", err)
	}
	redacted, err := runtimeprobe.Redact(bundle, []string{"sentinel-openai-key"}, 256)
	if err != nil {
		t.Fatal(err)
	}
	serialized := string(mustJSON(t, redacted))
	if strings.Contains(serialized, "sentinel-openai-key") {
		t.Fatalf("live evidence leaked credential: %s", serialized)
	}
	if !strings.Contains(serialized, `"schema_version":"runtime-probe.v1"`) {
		t.Fatalf("live evidence did not use runtime probe schema: %s", serialized)
	}
}

func TestLiveSmokeSkipsWithoutExplicitConfiguration(t *testing.T) {
	_ = os.Unsetenv("OPENAI_BASE_URL")
	_ = os.Unsetenv("OPENAI_MODEL")
	bundle, err := liveSmoke(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := bundle.Events[0].Status; got != runtimeprobe.StatusSkippedUnconfigured {
		t.Fatalf("status = %q, want skipped-unconfigured", got)
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
