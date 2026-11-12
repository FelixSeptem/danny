package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/FelixSeptem/danny/internal/runtimeprobe"
)

func TestProviderShimInjectsExplicitStreamDefault(t *testing.T) {
	var mu sync.Mutex
	var bodies []map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		var doc map[string]any
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Errorf("upstream got non-JSON body %q", raw)
			return
		}
		mu.Lock()
		bodies = append(bodies, doc)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp-shim","output":[]}`))
	}))
	defer upstream.Close()

	shim, err := startProviderShim(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = shim.Close() }()

	post := func(body string) {
		t.Helper()
		resp, err := http.Post(shim.baseURL+"/responses", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("shim status = %d", resp.StatusCode)
		}
	}
	post(`{"model":"m","input":[]}`)
	post(`{"model":"m","input":[],"stream":true}`)

	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 2 {
		t.Fatalf("upstream saw %d requests", len(bodies))
	}
	if got, ok := bodies[0]["stream"].(bool); !ok || got {
		t.Fatalf("omitted stream was not filled with false: %#v", bodies[0])
	}
	if got, ok := bodies[1]["stream"].(bool); !ok || !got {
		t.Fatalf("explicit stream flag was overwritten: %#v", bodies[1])
	}
	if got := shim.PatchedRequests(); got != 1 {
		t.Fatalf("patched = %d, want 1", got)
	}
}

func TestLiveGatewayRejectsUnknownCompatMode(t *testing.T) {
	t.Setenv(liveProviderShimEnv, "rewrite-everything")
	if _, _, _, err := liveGateway("https://provider.example/v1"); err == nil || !strings.Contains(err.Error(), "not a supported compat mode") {
		t.Fatalf("error = %v, want unsupported compat mode", err)
	}
}

func TestLiveGatewayIsPassthroughWithoutCompatMode(t *testing.T) {
	t.Setenv(liveProviderShimEnv, "")
	endpoint, compat, shim, err := liveGateway("https://provider.example/v1")
	if err != nil {
		t.Fatal(err)
	}
	if shim != nil || compat != "" || endpoint != "https://provider.example/v1" {
		t.Fatalf("endpoint=%q compat=%q shim=%v", endpoint, compat, shim)
	}
}

// TestLiveSmokeRecordsProviderCompatSoAPassingBundleIsNotMisleading guards the
// honesty requirement: if the harness translated a provider deviation, the
// evidence says so instead of looking like a clean conformance run.
func TestLiveSmokeRecordsProviderCompatSoAPassingBundleIsNotMisleading(t *testing.T) {
	server := newLiveProviderFake(t, "ok")
	defer server.Close()
	t.Setenv("OPENAI_BASE_URL", server.URL)
	t.Setenv("OPENAI_API_KEY", "sentinel-live-key")
	t.Setenv("OPENAI_MODEL", "probe-model")
	t.Setenv("STELE_URL", "")
	t.Setenv("DANNY_LIVE_FORMS", "direct")
	t.Setenv("DANNY_LIVE_OWNER_ROOT", t.TempDir())
	t.Setenv(liveProviderShimEnv, shimStreamDefault)

	bundle, err := liveSmoke(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if bundle.ProviderCompat != shimCompatRecord {
		t.Fatalf("provider_compat = %q, want %q", bundle.ProviderCompat, shimCompatRecord)
	}
	if err := bundle.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, event := range bundle.Events {
		if event.Kind == runtimeprobe.KindTerminal && event.CorrelationID == "live:direct" && event.Status != runtimeprobe.StatusPassed {
			t.Fatalf("direct terminal = %q payload=%#v", event.Status, event.Payload)
		}
	}
	// The compat record must survive redaction, otherwise the persisted evidence
	// loses the only hint that the run was not a clean conformance pass.
	redacted, err := runtimeprobe.Redact(bundle, []string{"sentinel-live-key"}, 256)
	if err != nil {
		t.Fatal(err)
	}
	if redacted.ProviderCompat != shimCompatRecord {
		t.Fatalf("redacted provider_compat = %q", redacted.ProviderCompat)
	}
}

func TestLiveSmokeOmitsProviderCompatForADirectRun(t *testing.T) {
	server := newLiveProviderFake(t, "ok")
	defer server.Close()
	t.Setenv("OPENAI_BASE_URL", server.URL)
	t.Setenv("OPENAI_API_KEY", "sentinel-live-key")
	t.Setenv("OPENAI_MODEL", "probe-model")
	t.Setenv("STELE_URL", "")
	t.Setenv("DANNY_LIVE_FORMS", "direct")
	t.Setenv("DANNY_LIVE_OWNER_ROOT", t.TempDir())
	t.Setenv(liveProviderShimEnv, "")

	bundle, err := liveSmoke(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if bundle.ProviderCompat != "" {
		t.Fatalf("provider_compat = %q, want empty", bundle.ProviderCompat)
	}
	raw, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "provider_compat") {
		t.Fatalf("clean run serialized provider_compat: %s", raw)
	}
}
