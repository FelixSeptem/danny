package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/FelixSeptem/danny/internal/context/stele"
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
	t.Setenv("DANNY_LIVE_FORMS", "direct")
	t.Setenv("DANNY_LIVE_OWNER_ROOT", t.TempDir())

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

func TestLiveSmokeCoversEverySelectedFormWithPassedProviderEvidence(t *testing.T) {
	server := newLiveProviderFake(t, "approve")
	defer server.Close()
	t.Setenv("OPENAI_BASE_URL", server.URL)
	t.Setenv("OPENAI_API_KEY", "sentinel-live-key")
	t.Setenv("OPENAI_MODEL", "probe-model")
	t.Setenv("STELE_URL", "")
	t.Setenv("DANNY_LIVE_FORMS", "")
	t.Setenv("DANNY_LIVE_OWNER_ROOT", t.TempDir())

	bundle, err := liveSmoke(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := bundle.Validate(); err != nil {
		t.Fatalf("live bundle invalid: %v", err)
	}

	wantKinds := map[string]runtimeprobe.Kind{
		"live:direct":        runtimeprobe.KindParent,
		"live:child":         runtimeprobe.KindChild,
		"live:team:parallel": runtimeprobe.KindTeam,
		"live:team:vote":     runtimeprobe.KindTeam,
		"live:workflow":      runtimeprobe.KindChild,
		"live:mailbox":       runtimeprobe.KindMailbox,
	}
	for correlation, wantKind := range wantKinds {
		var sawKind, sawTerminal bool
		for _, event := range bundle.Events {
			if event.CorrelationID != correlation {
				continue
			}
			if event.Kind == wantKind {
				sawKind = true
			}
			if event.Kind == runtimeprobe.KindTerminal {
				sawTerminal = true
				if event.Status != runtimeprobe.StatusPassed {
					t.Fatalf("%s terminal status = %q payload=%#v", correlation, event.Status, event.Payload)
				}
			}
		}
		if !sawKind || !sawTerminal {
			t.Fatalf("%s kind=%v terminal=%v", correlation, sawKind, sawTerminal)
		}
	}
}

func TestLiveSmokeKeepsPartialEvidenceWhenAFormFailsContract(t *testing.T) {
	// Vote aggregation only accepts a single approve/reject token, so "ok" is a
	// real contract failure for that form.
	server := newLiveProviderFake(t, "ok")
	defer server.Close()
	t.Setenv("OPENAI_BASE_URL", server.URL)
	t.Setenv("OPENAI_API_KEY", "sentinel-live-key")
	t.Setenv("OPENAI_MODEL", "probe-model")
	t.Setenv("STELE_URL", "")
	t.Setenv("DANNY_LIVE_FORMS", "direct,team_vote")
	t.Setenv("DANNY_LIVE_OWNER_ROOT", t.TempDir())

	bundle, err := liveSmoke(context.Background())
	if err == nil {
		t.Fatal("incomplete live run returned no error")
	}
	if len(bundle.Events) == 0 {
		t.Fatal("incomplete live run discarded collected evidence")
	}
	if validateErr := bundle.Validate(); validateErr != nil {
		t.Fatalf("partial bundle invalid: %v", validateErr)
	}
	statuses := map[string]runtimeprobe.Status{}
	for _, event := range bundle.Events {
		if event.Kind == runtimeprobe.KindTerminal {
			statuses[event.CorrelationID] = event.Status
		}
	}
	if statuses["live:direct"] != runtimeprobe.StatusPassed {
		t.Fatalf("direct terminal = %q, want passed", statuses["live:direct"])
	}
	if statuses["live:team:vote"] != runtimeprobe.StatusFailedContract {
		t.Fatalf("vote terminal = %q, want failed-contract", statuses["live:team:vote"])
	}
}

func TestLiveSmokeRejectsUnknownFormFilter(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "http://127.0.0.1:1")
	t.Setenv("OPENAI_MODEL", "probe-model")
	t.Setenv("DANNY_LIVE_FORMS", "direct,not-a-form")
	t.Setenv("DANNY_LIVE_OWNER_ROOT", t.TempDir())
	if _, err := liveSmoke(context.Background()); err == nil || !strings.Contains(err.Error(), "not-a-form") {
		t.Fatalf("error = %v, want unknown form rejection", err)
	}
}

func newLiveProviderFake(t *testing.T, answer string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/responses" {
			_, _ = w.Write([]byte(`{"id":"resp-live","output":[{"type":"message","content":[{"type":"output_text","text":"` + answer + `"}]}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"context_id":"ctx-live"}`))
	}))
}

func TestSteleContextProjectionUsesTurnBoundContract(t *testing.T) {
	var gotInputs []map[string]any
	contextCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/provider/context" {
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			gotInput, _ := body["input"].(map[string]any)
			gotInputs = append(gotInputs, gotInput)
			contextCalls++
			continuity := `{"contract_version":"context-continuity-v1","read_semantics":"fresh","outcome":"issued","reference":"ctxc_0123456789abcdef0123456789abcdef","expires_at":"2026-10-22T00:00:00Z"}`
			if contextCalls == 2 {
				continuity = `{"contract_version":"context-continuity-v1","read_semantics":"fresh_compare","outcome":"unchanged","reference":"ctxc_0123456789abcdef0123456789abcdef","expires_at":"2026-10-22T00:00:00Z"}`
			}
			requestID := "stele:context:projection"
			if contextCalls == 2 {
				requestID += ":compare"
			}
			writeProviderContextFixture(t, w, []byte(`{"result":{"turn_id":"turn-server-1","response_mode":"reference_only","context_digest":{"contract_version":"context-turn-digest-v1","algorithm":"sha-256","canonicalization_version":"context-turn-canonical-v1","selection_policy_version":"context-selection-v1","selection_policy_fingerprint":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","value":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},"continuity":`+continuity+`,"disclosure":{"profile":"memory-reference-v1","policy_version":"context-disclosure-v1","projection_intent":"task_context","source_metadata_version":"source-metadata-v1"},"budget_accounting":{"contract_version":"context-budget-v1","packing_policy_version":"context-packing-v1","requested":{"max_items":4},"effective":{"max_items":4},"usage":{"items":0},"selection":{"outcome":"empty"}},"profile":[],"recent_session":[],"recent_episodes":[],"relevant_summaries":[],"related_entities":[],"citations":[]}}`), requestID)
			return
		}
		if r.URL.Path == "/v1/provider/turns" {
			_, _ = w.Write([]byte(`{"result":{"turn_id":"turn-server-1"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"binding_id":"binding-1","session_id":"session-1"}`))
	}))
	defer server.Close()
	client, err := stele.NewClient(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.Tenant, client.Project, client.Namespace = "tenant", "project", "namespace"
	client.BindingID, client.SessionID = "binding-1", "session-1"
	out := steleContextProjection(context.Background(), client, runtimeprobe.ProbeRequest{CorrelationID: "stele:context", ParentID: "stele:context:parent", Input: "probe", Capability: "context"})
	if err := (runtimeprobe.Bundle{SchemaVersion: runtimeprobe.EvidenceSchemaVersion, BaymaxVersion: "baymax", SteleVersion: stele.PinnedVersion, Events: out.Events}).Validate(); err != nil {
		t.Fatal(err)
	}
	if out.Events[1].Status != runtimeprobe.StatusPassed || out.Events[1].Payload["continuity"] != "issued" || out.Events[1].Payload["continuity_compare"] != "unchanged" || out.Events[1].Payload["turn_id"] != "turn-server-1" {
		t.Fatalf("events=%#v", out.Events)
	}
	if len(gotInputs) != 2 || gotInputs[0]["turn_id"] != "turn-server-1" || gotInputs[0]["response_mode"] != "reference_only" {
		t.Fatalf("inputs=%#v", gotInputs)
	}
	if gotInputs[0]["continuity"].(map[string]any)["mode"] != "issue" || gotInputs[0]["budget_limits"].(map[string]any)["max_citations"].(float64) != 8 {
		t.Fatalf("inputs=%#v", gotInputs)
	}
	if gotInputs[1]["continuity"].(map[string]any)["mode"] != "compare" {
		t.Fatalf("inputs=%#v", gotInputs)
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

func writeProviderContextFixture(t *testing.T, w http.ResponseWriter, raw []byte, requestID string) {
	t.Helper()
	var envelope map[string]any
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatal(err)
	}
	result, ok := envelope["result"].(map[string]any)
	if !ok {
		t.Fatal("provider fixture result is not an object")
	}
	envelope["metadata"] = map[string]any{"request_id": requestID, "operation_id": "providerAssembleContext", "schema_version": "provider-v1"}
	envelope["citations"] = result["citations"]
	accounting, ok := result["budget_accounting"].(map[string]any)
	if !ok {
		t.Fatal("provider fixture budget accounting is not an object")
	}
	accounting["requested"] = map[string]any{"max_items": 4, "max_content_bytes": nil, "max_response_bytes": 16384, "max_citations": 8}
	accounting["effective"] = map[string]any{"max_items": 4, "max_content_bytes": 16384, "max_response_bytes": 16384, "max_citations": 8}
	accounting["usage"] = map[string]any{"items": 0, "content_bytes": 0, "response_bytes": 0, "citations": 0}
	for i := 0; i < 4; i++ {
		encoded, err := json.Marshal(envelope)
		if err != nil {
			t.Fatal(err)
		}
		accounting["usage"].(map[string]any)["response_bytes"] = len(encoded)
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write(encoded)
}
