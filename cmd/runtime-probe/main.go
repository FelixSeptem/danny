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
	"github.com/FelixSeptem/danny/internal/agent/baymax"
	"github.com/FelixSeptem/danny/internal/context/stele"
	"github.com/FelixSeptem/danny/internal/runtimeprobe"
)

func main() {
	mode := "offline"
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}
	if mode == prResumeMode {
		ticketPath := ""
		for index, arg := range os.Args {
			if arg == "--ticket" && index+1 < len(os.Args) {
				ticketPath = os.Args[index+1]
			}
		}
		if ticketPath == "" {
			fmt.Fprintln(os.Stderr, "live-resume requires --ticket <path>")
			os.Exit(2)
		}
		if err := runLiveResume(ticketPath); err != nil {
			// The result file carries the structured outcome; the exit status
			// must stay non-zero so a failed restore cannot read as a pass.
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
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
	case "live-pause-restore":
		bundle, err = livePauseRestore(context.Background())
	case "stele-smoke":
		bundle, err = steleSmoke(context.Background())
	default:
		fmt.Fprintln(os.Stderr, "usage: runtime-probe [offline|live-smoke|live-pause-restore|stele-smoke]")
		os.Exit(2)
	}
	if len(bundle.Events) == 0 {
		if err == nil {
			err = fmt.Errorf("runtime probe produced no evidence")
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	secrets := []string{os.Getenv("OPENAI_API_KEY"), os.Getenv("STELE_API_KEY")}
	if path := os.Getenv("STELE_CREDENTIAL_FILE"); path != "" {
		if secret, readErr := os.ReadFile(path); readErr == nil {
			secrets = append(secrets, strings.TrimSpace(string(secret)))
		}
	}
	redacted, redactErr := runtimeprobe.Redact(bundle, secrets, 256)
	if redactErr != nil {
		fmt.Fprintln(os.Stderr, redactErr)
		os.Exit(1)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(redacted)
	// A partial live run still emits the evidence it collected, but the exit
	// status must stay non-zero so an incomplete gate cannot be mistaken for a
	// pass.
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
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
		{"compilation", client.Compilation},
		{"restart", client.RestartProjection},
		{"idempotent", client.IdempotentMemory},
	}
	for _, probe := range probes {
		corr := "stele:" + probe.name
		request := runtimeprobe.ProbeRequest{CorrelationID: corr, ParentID: corr + ":parent", Input: "runtime probe memory continuity", Capability: probe.name}
		var out runtimeprobe.ProbeOutcome
		if probe.name == "context" {
			out = steleContextProjection(ctx, client, request)
		} else {
			out, _ = probe.call(ctx, request)
		}
		events = append(events, out.Events...)
	}
	for i := range events {
		events[i].Sequence = int64(i + 1)
	}
	bundle := runtimeprobe.Bundle{SchemaVersion: runtimeprobe.EvidenceSchemaVersion, BaymaxVersion: baymax.PinnedVersion, SteleVersion: client.Version(), Events: events}
	return bundle, bundle.Validate()
}

// steleContextProjection exercises the current turn-bound provider/context
// contract. It records only bounded contract evidence; references and provider
// payloads never enter the runtime-probe artifact.
func steleContextProjection(ctx context.Context, client *stele.Client, req runtimeprobe.ProbeRequest) runtimeprobe.ProbeOutcome {
	now := time.Unix(0, 0).UTC()
	parent := runtimeprobe.Event{Sequence: 1, ID: req.ParentID, Kind: runtimeprobe.KindParent, CorrelationID: req.CorrelationID, Capability: req.Capability, Status: runtimeprobe.StatusPassed, At: now}
	turn, err := client.CreateTurn(ctx, req.CorrelationID+":turn", req.Input, 4)
	if err != nil {
		parent.Status = runtimeprobe.StatusFailedContract
		return runtimeprobe.ProbeOutcome{Events: []runtimeprobe.Event{
			parent,
			{Sequence: 2, ID: req.ParentID + ":context", Kind: runtimeprobe.KindContext, ParentID: req.ParentID, CorrelationID: req.CorrelationID, Capability: req.Capability, Status: runtimeprobe.StatusFailedContract, At: now.Add(time.Nanosecond), Payload: map[string]string{"path": "/v1/provider/turns", "error": err.Error()}},
			{Sequence: 3, ID: req.ParentID + ":context:terminal", Kind: runtimeprobe.KindTerminal, ParentID: req.ParentID + ":context", CorrelationID: req.CorrelationID, Capability: req.Capability, Status: runtimeprobe.StatusFailedContract, At: now.Add(2 * time.Nanosecond)},
		}}
	}
	projection, err := client.BoundedProjection(ctx, stele.ProjectionRequest{
		RunID: req.CorrelationID + ":projection", Query: req.Input, Budget: 4, MaxBytes: 16384, MaxReferences: 8,
		Scope: strings.Join([]string{client.Tenant, client.Project, client.Namespace}, "/"), SessionID: client.SessionID,
		TurnID: turn.ID, PolicyDigest: "runtimeprobe-policy-v1", ProjectionIntent: "task_context", ContinuityMode: "issue",
	})
	status := runtimeprobe.StatusPassed
	payload := map[string]string{"path": "/v1/provider/context", "turn_id": turn.ID}
	if err != nil {
		status = runtimeprobe.StatusFailedContract
		payload["error"] = err.Error()
	} else {
		payload["source_digest"] = projection.SourceDigest
		payload["projection_digest"] = projection.Digest
		payload["continuity"] = projection.Continuity.Outcome
		payload["references"] = fmt.Sprint(len(projection.References))
		comparison, compareErr := client.BoundedProjection(ctx, stele.ProjectionRequest{
			RunID: req.CorrelationID + ":projection:compare", Query: req.Input, Budget: 4, MaxBytes: 16384, MaxReferences: 8,
			Scope: strings.Join([]string{client.Tenant, client.Project, client.Namespace}, "/"), SessionID: client.SessionID,
			TurnID: turn.ID, PolicyDigest: "runtimeprobe-policy-v1", ProjectionIntent: "task_context", ContinuityMode: "compare", ContinuityReference: projection.Continuity.Reference,
		})
		if compareErr != nil {
			status = runtimeprobe.StatusFailedContract
			payload["compare_error"] = compareErr.Error()
		} else {
			payload["continuity_compare"] = comparison.Continuity.Outcome
			payload["compare_projection_digest"] = comparison.Digest
		}
	}
	parent.Status = status
	return runtimeprobe.ProbeOutcome{Events: []runtimeprobe.Event{
		parent,
		{Sequence: 2, ID: req.ParentID + ":context", Kind: runtimeprobe.KindContext, ParentID: req.ParentID, CorrelationID: req.CorrelationID, Capability: req.Capability, Status: status, At: now.Add(time.Nanosecond), Payload: payload},
		{Sequence: 3, ID: req.ParentID + ":context:terminal", Kind: runtimeprobe.KindTerminal, ParentID: req.ParentID + ":context", CorrelationID: req.CorrelationID, Capability: req.Capability, Status: status, At: now.Add(2 * time.Nanosecond)},
	}}
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
