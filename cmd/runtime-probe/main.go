package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/FelixSeptem/baymax/core/types"
	"github.com/FelixSeptem/baymax/integration/fakes"
	openaiadapter "github.com/FelixSeptem/baymax/model/openai"
	"github.com/FelixSeptem/danny/internal/agent/baymax"
	"github.com/FelixSeptem/danny/internal/context/stele"
	"github.com/FelixSeptem/danny/internal/runtimeprobe"
)

func main() {
	mode := "offline"
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}
	var bundle runtimeprobe.Bundle
	var err error
	switch mode {
	case "offline":
		model := fakes.NewModel([]fakes.ModelStep{{Response: types.ModelResponse{FinalAnswer: "offline"}}, {Response: types.ModelResponse{FinalAnswer: "offline"}}})
		adapter, e := baymax.New(model)
		if e != nil {
			err = e
			break
		}
		bundle, err = (runtimeprobe.OfflineRunner{Baymax: adapter}).Run(context.Background(), "runtime probe")
	case "live-smoke":
		bundle, err = liveSmoke(context.Background())
	case "stele-smoke":
		bundle, err = steleSmoke(context.Background())
	default:
		fmt.Fprintln(os.Stderr, "usage: runtime-probe [offline|live-smoke|stele-smoke]")
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	secrets := []string{os.Getenv("OPENAI_API_KEY"), os.Getenv("STELE_API_KEY")}
	if path := os.Getenv("STELE_CREDENTIAL_FILE"); path != "" {
		if secret, readErr := os.ReadFile(path); readErr == nil {
			secrets = append(secrets, strings.TrimSpace(string(secret)))
		}
	}
	redacted, err := runtimeprobe.Redact(bundle, secrets, 256)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(redacted)
}

func liveSmoke(ctx context.Context) (runtimeprobe.Bundle, error) {
	endpoint, modelName := os.Getenv("OPENAI_BASE_URL"), os.Getenv("OPENAI_MODEL")
	if endpoint == "" || modelName == "" {
		now := time.Unix(0, 0).UTC()
		return runtimeprobe.Bundle{SchemaVersion: runtimeprobe.EvidenceSchemaVersion, BaymaxVersion: baymax.PinnedVersion, SteleVersion: stele.PinnedVersion, Events: []runtimeprobe.Event{
			{Sequence: 1, ID: "live:parent", Kind: runtimeprobe.KindParent, CorrelationID: "live", Capability: "live-smoke", Status: runtimeprobe.StatusSkippedUnconfigured, At: now},
			{Sequence: 2, ID: "live:terminal", Kind: runtimeprobe.KindTerminal, ParentID: "live:parent", CorrelationID: "live", Capability: "live-smoke", Status: runtimeprobe.StatusSkippedUnconfigured, At: now.Add(time.Nanosecond)},
		}}, nil
	}
	profile := &openaiadapter.EndpointProfile{
		Version:  openaiadapter.EndpointProfileVersion,
		ID:       "danny-runtime-probe",
		Endpoint: endpoint,
		Model:    modelName,
		APIShape: openaiadapter.ResponsesAPIShapeVersion,
		Capabilities: map[openaiadapter.EndpointCapability]types.CapabilitySupport{
			openaiadapter.EndpointCapabilityStructuredInput:  types.CapabilitySupportSupported,
			openaiadapter.EndpointCapabilityNativeToolResult: types.CapabilitySupportSupported,
			openaiadapter.EndpointCapabilityStreaming:        types.CapabilitySupportSupported,
		},
	}
	model := openaiadapter.NewClient(openaiadapter.Config{APIKey: os.Getenv("OPENAI_API_KEY"), BaseURL: endpoint, Model: modelName, Profile: profile})
	adapter, err := baymax.New(model)
	if err != nil {
		return runtimeprobe.Bundle{}, err
	}
	out, err := adapter.Run(ctx, runtimeprobe.ProbeRequest{CorrelationID: "live:run", Input: "Reply with one short sentence.", Capability: "run"})
	if err != nil {
		return runtimeprobe.Bundle{}, err
	}
	steleClient, err := stele.NewClient(os.Getenv("STELE_URL"), http.DefaultClient)
	if err != nil {
		return runtimeprobe.Bundle{}, err
	}
	if steleClient != nil {
		st, e := steleClient.Context(ctx, runtimeprobe.ProbeRequest{CorrelationID: "live:stele", ParentID: "live:stele:parent", Input: "probe", Capability: "context"})
		out.Events = append(out.Events, st.Events...)
		if e != nil {
			return runtimeprobe.Bundle{}, e
		}
	} else {
		now := time.Unix(0, 0).UTC()
		base := int64(len(out.Events))
		out.Events = append(out.Events,
			runtimeprobe.Event{Sequence: base + 1, ID: "live:stele:parent", Kind: runtimeprobe.KindParent, CorrelationID: "live:stele", Capability: "context", Status: runtimeprobe.StatusSkippedUnconfigured, At: now},
			runtimeprobe.Event{Sequence: base + 2, ID: "live:stele:terminal", Kind: runtimeprobe.KindTerminal, ParentID: "live:stele:parent", CorrelationID: "live:stele", Capability: "context", Status: runtimeprobe.StatusSkippedUnconfigured, At: now.Add(time.Nanosecond)},
		)
	}
	for i := range out.Events {
		out.Events[i].Sequence = int64(i + 1)
	}
	return runtimeprobe.Bundle{SchemaVersion: runtimeprobe.EvidenceSchemaVersion, BaymaxVersion: adapter.Version(), SteleVersion: stele.PinnedVersion, Events: out.Events}, nil
}

func steleSmoke(ctx context.Context) (runtimeprobe.Bundle, error) {
	endpoint := os.Getenv("STELE_URL")
	credentialFile := os.Getenv("STELE_CREDENTIAL_FILE")
	if endpoint == "" || credentialFile == "" {
		now := time.Unix(0, 0).UTC()
		return runtimeprobe.Bundle{SchemaVersion: runtimeprobe.EvidenceSchemaVersion, BaymaxVersion: baymax.PinnedVersion, SteleVersion: stele.PinnedVersion, Events: []runtimeprobe.Event{{Sequence: 1, ID: "stele:parent", Kind: runtimeprobe.KindParent, CorrelationID: "stele", Capability: "stele-smoke", Status: runtimeprobe.StatusSkippedUnconfigured, At: now}, {Sequence: 2, ID: "stele:terminal", Kind: runtimeprobe.KindTerminal, ParentID: "stele:parent", CorrelationID: "stele", Capability: "stele-smoke", Status: runtimeprobe.StatusSkippedUnconfigured, At: now.Add(time.Nanosecond)}}}, nil
	}
	secret, err := os.ReadFile(credentialFile)
	if err != nil {
		return runtimeprobe.Bundle{}, err
	}
	client, err := stele.NewClient(endpoint, http.DefaultClient)
	if err != nil {
		return runtimeprobe.Bundle{}, err
	}
	client.APIKey = strings.TrimSpace(string(secret))
	client.Tenant, client.Project, client.Namespace = envOr("STELE_TENANT", "tenant-a"), envOr("STELE_PROJECT", "project-a"), envOr("STELE_NAMESPACE", "namespace-a")
	if _, err := client.InitializeRuntime(ctx, stele.RuntimeInitialization{Scope: map[string]string{"tenant": client.Tenant, "project": client.Project, "namespace": client.Namespace}, AgentID: envOr("STELE_AGENT_ID", "danny-runtime-probe"), SessionID: envOr("STELE_SESSION_ID", "danny-runtime-probe-session")}); err != nil {
		return runtimeprobe.Bundle{}, err
	}
	events := make([]runtimeprobe.Event, 0, 16)
	probes := []struct {
		name string
		call func(context.Context, runtimeprobe.ProbeRequest) (runtimeprobe.ProbeOutcome, error)
	}{
		{"context", client.Context},
		{"memory", client.Memory},
		{"references", client.References},
		{"restart", client.RestartProjection},
		{"idempotent", client.IdempotentMemory},
	}
	for _, probe := range probes {
		corr := "stele:" + probe.name
		out, _ := probe.call(ctx, runtimeprobe.ProbeRequest{CorrelationID: corr, ParentID: corr + ":parent", Input: "runtime probe memory continuity", Capability: probe.name})
		events = append(events, out.Events...)
	}
	for i := range events {
		events[i].Sequence = int64(i + 1)
	}
	bundle := runtimeprobe.Bundle{SchemaVersion: runtimeprobe.EvidenceSchemaVersion, BaymaxVersion: baymax.PinnedVersion, SteleVersion: client.Version(), Events: events}
	return bundle, bundle.Validate()
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
