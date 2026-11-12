package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	bmtypes "github.com/FelixSeptem/baymax/core/types"
	openaiadapter "github.com/FelixSeptem/baymax/model/openai"
	"github.com/FelixSeptem/baymax/orchestration/mailbox"
	"github.com/FelixSeptem/baymax/orchestration/teams"
	"github.com/FelixSeptem/danny/internal/agent/baymax"
	"github.com/FelixSeptem/danny/internal/context/stele"
	"github.com/FelixSeptem/danny/internal/runtimeprobe"
)

// The live gate needs real provider evidence per execution form, not a single
// direct call. Each form runs through Danny's authorized adapter so the
// evidence records what the product path actually did.
const (
	liveFormTimeout = 3 * time.Minute
	liveFormPrompt  = "Reply with one short sentence."
	// liveVotePrompt must stay answerable by a single approve/reject token:
	// vote aggregation rejects prose, and a model that ignores the instruction
	// is a real contract mismatch worth recording.
	liveVotePrompt = "Reply with exactly one word and no punctuation: approve"
)

const (
	liveOwnerRootEnv = "DANNY_LIVE_OWNER_ROOT"
	liveFormsEnv     = "DANNY_LIVE_FORMS"
	liveDefaultRoot  = ".tmp/live-probe"
)

type liveForm struct {
	name string
	run  func(context.Context, *baymax.Client, string) ([]runtimeprobe.Event, error)
}

func allLiveForms() []liveForm {
	return []liveForm{
		{name: "direct", run: runLiveDirect},
		{name: "child", run: runLiveChild},
		{name: "team_parallel", run: runLiveTeamParallel},
		{name: "team_vote", run: runLiveTeamVote},
		{name: "workflow", run: runLiveWorkflow},
		{name: "mailbox", run: runLiveMailbox},
	}
}

// selectedLiveForms honours DANNY_LIVE_FORMS so an operator can re-run one
// failing form without paying for the rest. Unknown names are rejected instead
// of silently skipped, because a silently skipped form would look like a pass.
func selectedLiveForms() ([]liveForm, error) {
	spec := strings.TrimSpace(os.Getenv(liveFormsEnv))
	all := allLiveForms()
	if spec == "" {
		return all, nil
	}
	wanted := make(map[string]bool, len(all))
	for _, name := range strings.Split(spec, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		wanted[name] = true
	}
	selected := make([]liveForm, 0, len(all))
	known := make(map[string]bool, len(all))
	for _, form := range all {
		known[form.name] = true
		if wanted[form.name] {
			selected = append(selected, form)
		}
	}
	for name := range wanted {
		if !known[name] {
			return nil, fmt.Errorf("%s contains unknown form %q", liveFormsEnv, name)
		}
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("%s selected no form", liveFormsEnv)
	}
	return selected, nil
}

type liveChain struct {
	correlation string
	capability  string
	events      []runtimeprobe.Event
}

func newLiveChain(correlation, capability string) *liveChain {
	return &liveChain{correlation: correlation, capability: capability}
}

func (c *liveChain) add(id string, kind runtimeprobe.Kind, parentID string, status runtimeprobe.Status, payload map[string]string) {
	c.events = append(c.events, runtimeprobe.Event{
		Sequence:      int64(len(c.events) + 1),
		ID:            id,
		Kind:          kind,
		ParentID:      parentID,
		CorrelationID: c.correlation,
		Capability:    c.capability,
		Status:        status,
		At:            time.Unix(0, 0).UTC().Add(time.Duration(len(c.events)) * time.Nanosecond),
		Payload:       payload,
	})
}

// closeChain always terminates the correlation so a failed form still produces
// valid evidence instead of dropping the whole bundle.
func (c *liveChain) closeChain(parentID string, status runtimeprobe.Status, payload map[string]string) {
	c.add(c.correlation+":terminal", runtimeprobe.KindTerminal, parentID, status, payload)
}

func runLiveDirect(ctx context.Context, client *baymax.Client, _ string) ([]runtimeprobe.Event, error) {
	out, err := client.Run(ctx, runtimeprobe.ProbeRequest{CorrelationID: "live:direct", Input: liveFormPrompt, Capability: "direct"})
	return out.Events, err
}

func runLiveChild(ctx context.Context, client *baymax.Client, _ string) ([]runtimeprobe.Event, error) {
	const correlation = "live:child"
	chain := newLiveChain(correlation, "child")
	parentID := correlation + ":parent"
	result, err := client.RunChildAuthorized(ctx, baymax.AuthorizedRequest{
		RunID: "live-child-run", ParentRunID: "live-run", CorrelationID: correlation,
		PlanVersion: "live.plan.v1", PlanDigest: "sha256:live-plan",
		CapabilityDigest: "sha256:live-capability", ProjectionDigest: "sha256:live-projection",
		WriteScopeDigest: "sha256:live-scope", EffectNamespace: "live:child",
		Role: "subagent", PerChildBudget: 120000,
		Input: liveFormPrompt,
	})
	status := runtimeprobe.StatusPassed
	payload := map[string]string{
		"run_id":                 "live-child-run",
		"terminal_status":        result.Terminal.Status,
		"native_history_durable": fmt.Sprint(result.NativeHistoryDurable),
	}
	if err != nil || result.Terminal.Status != "succeeded" {
		status = runtimeprobe.StatusFailedContract
		if err != nil {
			payload["error"] = err.Error()
		}
	}
	chain.add(parentID, runtimeprobe.KindParent, "", status, nil)
	chain.add(correlation+":child", runtimeprobe.KindChild, parentID, status, payload)
	chain.closeChain(correlation+":child", status, nil)
	return chain.events, err
}

func runLiveTeamParallel(ctx context.Context, client *baymax.Client, _ string) ([]runtimeprobe.Event, error) {
	return runLiveTeam(ctx, client, teams.StrategyParallel, "live:team:parallel", "team_parallel", liveFormPrompt)
}

func runLiveTeamVote(ctx context.Context, client *baymax.Client, _ string) ([]runtimeprobe.Event, error) {
	return runLiveTeam(ctx, client, teams.StrategyVote, "live:team:vote", "team_vote", liveVotePrompt)
}

func runLiveTeam(ctx context.Context, client *baymax.Client, strategy teams.Strategy, correlation, capability, memberInput string) ([]runtimeprobe.Event, error) {
	chain := newLiveChain(correlation, capability)
	parentID := correlation + ":parent"
	teamID := correlation + ":team"
	result, err := client.RunTeamAuthorized(ctx, baymax.TeamRequest{
		RunID: correlation + ":run", TeamID: teamID, CorrelationID: correlation,
		PlanVersion: "live.plan.v1", PlanDigest: "sha256:live-plan",
		CapabilityDigest: "sha256:live-capability", ProjectionDigest: "sha256:live-projection",
		WriteScopeDigest: "sha256:live-scope", EffectNamespace: correlation,
		Strategy: strategy, VoteTieBreak: teams.VoteTieBreakFirstTaskID,
		TaskTimeout: 90 * time.Second, ParallelMaxWorkers: 2,
		Members: []baymax.TeamMember{
			{ID: "member-a", Role: "worker", Input: memberInput},
			{ID: "member-b", Role: "worker", Input: memberInput},
		},
	})
	status := runtimeprobe.StatusPassed
	if err != nil || result.Status != "succeeded" {
		status = runtimeprobe.StatusFailedContract
	}
	teamPayload := map[string]string{
		"team_id":                teamID,
		"strategy":               string(strategy),
		"member_count":           "2",
		"native_history_durable": fmt.Sprint(result.NativeHistoryDurable),
	}
	if result.WinnerVote != "" {
		teamPayload["winner_vote"] = result.WinnerVote
	}
	if err != nil {
		teamPayload["error"] = err.Error()
	}
	chain.add(parentID, runtimeprobe.KindParent, "", status, nil)
	chain.add(teamID, runtimeprobe.KindTeam, parentID, status, teamPayload)
	for _, member := range result.Members {
		memberStatus := runtimeprobe.StatusPassed
		if member.Status != "succeeded" {
			memberStatus = runtimeprobe.StatusFailedContract
		}
		memberPayload := map[string]string{"member_id": member.MemberID, "terminal_status": member.Status}
		if member.FailureClass != "" {
			memberPayload["failure_class"] = member.FailureClass
		}
		chain.add(correlation+":member:"+member.MemberID, runtimeprobe.KindChild, teamID, memberStatus, memberPayload)
	}
	chain.closeChain(teamID, status, nil)
	return chain.events, err
}

func runLiveWorkflow(ctx context.Context, client *baymax.Client, root string) ([]runtimeprobe.Event, error) {
	const correlation = "live:workflow"
	chain := newLiveChain(correlation, "workflow")
	parentID := correlation + ":parent"
	result, err := client.RunWorkflowAuthorized(ctx, baymax.WorkflowRequest{
		Version: baymax.WorkflowContractVersion,
		RunID:   "live-workflow-run", WorkflowID: "live-workflow", SessionID: "live-session",
		ParentRunID: "live-run", CausationID: "live-cause", CorrelationID: correlation,
		PlanVersion: "live.plan.v1", PlanDigest: "sha256:live-plan",
		CapabilityDigest: "sha256:live-capability", ProjectionDigest: "sha256:live-projection",
		WriteScopeDigest: "sha256:live-scope", EffectNamespace: "live:workflow",
		OccurrenceID: "live-occurrence",
		HistoryPath:  filepath.Join(root, "workflow-history.json"),
		MaxSteps:     2,
		Assignments: []baymax.WorkflowAssignment{
			{ID: "step-a", Role: "worker", Objective: liveFormPrompt, Budget: 120000},
			{ID: "step-b", Role: "worker", Objective: liveFormPrompt, Budget: 120000, DependsOn: []string{"step-a"}},
		},
	})
	status := runtimeprobe.StatusPassed
	if err != nil || result.Status != "succeeded" {
		status = runtimeprobe.StatusFailedContract
	}
	workflowPayload := map[string]string{
		"workflow_id":            "live-workflow",
		"step_count":             fmt.Sprint(len(result.Steps)),
		"execution_order":        strings.Join(result.ExecutionOrder, ","),
		"native_history_durable": fmt.Sprint(result.NativeHistoryDurable),
	}
	if err != nil {
		workflowPayload["error"] = err.Error()
	}
	chain.add(parentID, runtimeprobe.KindParent, "", status, nil)
	lastParent := parentID
	for _, step := range result.Steps {
		stepStatus := runtimeprobe.StatusPassed
		if step.Status != "succeeded" {
			stepStatus = runtimeprobe.StatusFailedContract
		}
		stepID := correlation + ":step:" + step.ID
		stepPayload := map[string]string{"step_id": step.ID, "role": step.Role, "terminal_status": step.Status}
		if step.FailureClass != "" {
			stepPayload["failure_class"] = step.FailureClass
		}
		chain.add(stepID, runtimeprobe.KindChild, parentID, stepStatus, stepPayload)
		lastParent = stepID
	}
	chain.closeChain(lastParent, status, workflowPayload)
	return chain.events, err
}

func runLiveMailbox(ctx context.Context, client *baymax.Client, root string) ([]runtimeprobe.Event, error) {
	const correlation = "live:mailbox"
	chain := newLiveChain(correlation, "mailbox")
	parentID := correlation + ":parent"
	store, err := mailbox.NewFileStore(filepath.Join(root, "mailbox.json"), mailbox.Policy{MaxAttempts: 2, DLQEnabled: true})
	if err != nil {
		chain.add(parentID, runtimeprobe.KindParent, "", runtimeprobe.StatusFailedContract, map[string]string{"error": err.Error()})
		chain.closeChain(parentID, runtimeprobe.StatusFailedContract, nil)
		return chain.events, err
	}
	box, err := mailbox.New(store)
	if err != nil {
		chain.add(parentID, runtimeprobe.KindParent, "", runtimeprobe.StatusFailedContract, map[string]string{"error": err.Error()})
		chain.closeChain(parentID, runtimeprobe.StatusFailedContract, nil)
		return chain.events, err
	}
	result, err := client.RunMailboxAuthorized(ctx, baymax.MailboxRequest{
		Mailbox: box, ConsumerID: "live-probe", Lease: 60 * time.Second,
		MessageID: "live-message", IdempotencyKey: "live:idempotency", RunID: "live-mailbox-run",
		ParentRunID: "live-run", CausationID: "live-cause", CorrelationID: correlation,
		AuthorityDigest: "sha256:live-capability", PlanDigest: "sha256:live-plan",
		WriteScope: "sha256:live-scope", EffectNamespace: "live:mailbox", Occurrence: "live-occurrence",
		ResourceLimit: 1, PlanVersion: "live.plan.v1",
		CapabilityDigest: "sha256:live-capability", ProjectionDigest: "sha256:live-projection",
		Input: liveFormPrompt,
	})
	status := runtimeprobe.StatusPassed
	if err != nil || result.Execution.Terminal.Status != "succeeded" {
		status = runtimeprobe.StatusFailedContract
	}
	payload := map[string]string{
		"message_id":        "live-message",
		"terminal_status":   result.Execution.Terminal.Status,
		"mailbox_lifecycle": string(result.Mailbox.LifecycleState),
	}
	if result.Mailbox.DeliveryAttempt > 0 {
		payload["delivery_attempt"] = fmt.Sprint(result.Mailbox.DeliveryAttempt)
	}
	if err != nil {
		payload["error"] = err.Error()
	}
	chain.add(parentID, runtimeprobe.KindParent, "", status, nil)
	chain.add(correlation+":mailbox", runtimeprobe.KindMailbox, parentID, status, payload)
	chain.closeChain(correlation+":mailbox", status, nil)
	return chain.events, err
}

func liveOwnerRoot() string {
	if root := strings.TrimSpace(os.Getenv(liveOwnerRootEnv)); root != "" {
		return root
	}
	return liveDefaultRoot
}

// liveSmoke runs every selected execution form against the configured
// OpenAI-compatible endpoint. A form that fails marks its own events
// failed-contract and is returned as an incomplete run; the evidence bundle is
// still valid so a partial gate can be diagnosed instead of discarded.
func liveSmoke(ctx context.Context) (runtimeprobe.Bundle, error) {
	endpoint, modelName := os.Getenv("OPENAI_BASE_URL"), os.Getenv("OPENAI_MODEL")
	if endpoint == "" || modelName == "" {
		now := time.Unix(0, 0).UTC()
		return runtimeprobe.Bundle{SchemaVersion: runtimeprobe.EvidenceSchemaVersion, BaymaxVersion: baymax.PinnedVersion, SteleVersion: stele.PinnedVersion, Events: []runtimeprobe.Event{
			{Sequence: 1, ID: "live:parent", Kind: runtimeprobe.KindParent, CorrelationID: "live", Capability: "live-smoke", Status: runtimeprobe.StatusSkippedUnconfigured, At: now},
			{Sequence: 2, ID: "live:terminal", Kind: runtimeprobe.KindTerminal, ParentID: "live:parent", CorrelationID: "live", Capability: "live-smoke", Status: runtimeprobe.StatusSkippedUnconfigured, At: now.Add(time.Nanosecond)},
		}}, nil
	}
	forms, err := selectedLiveForms()
	if err != nil {
		return runtimeprobe.Bundle{}, err
	}
	// gatewayEndpoint is what Danny dials; providerCompat says whether the
	// harness had to translate anything on the way to the real upstream.
	gatewayEndpoint, providerCompat, shim, err := liveGateway(endpoint)
	if err != nil {
		return runtimeprobe.Bundle{}, err
	}
	if shim != nil {
		defer func() { _ = shim.Close() }()
	}
	profile := &openaiadapter.EndpointProfile{
		Version:  openaiadapter.EndpointProfileVersion,
		ID:       "danny-runtime-probe",
		Endpoint: gatewayEndpoint,
		Model:    modelName,
		APIShape: openaiadapter.ResponsesAPIShapeVersion,
		Capabilities: map[openaiadapter.EndpointCapability]bmtypes.CapabilitySupport{
			openaiadapter.EndpointCapabilityStructuredInput:  bmtypes.CapabilitySupportSupported,
			openaiadapter.EndpointCapabilityNativeToolResult: bmtypes.CapabilitySupportSupported,
			openaiadapter.EndpointCapabilityStreaming:        bmtypes.CapabilitySupportSupported,
		},
	}
	model := openaiadapter.NewClient(openaiadapter.Config{APIKey: os.Getenv("OPENAI_API_KEY"), BaseURL: gatewayEndpoint, Model: modelName, Profile: profile})
	client, err := baymax.New(model)
	if err != nil {
		return runtimeprobe.Bundle{}, err
	}
	defer func() { _ = client.Close() }()

	// Every run gets its own owner root so a previous attempt's journal, ledger
	// or mailbox state can never masquerade as this run's evidence.
	ownerRoot := filepath.Join(liveOwnerRoot(), "live-probe-"+strconv.FormatInt(time.Now().UTC().UnixNano(), 10))
	if err := client.BindOwnerStore(baymax.OwnerStoreConfig{Root: filepath.Join(ownerRoot, "owner-store"), Retention: 64, ProjectionRetention: 64}); err != nil {
		return runtimeprobe.Bundle{}, err
	}
	if err := client.BindContinuationOwners(baymax.ContinuationOwnerConfig{Root: filepath.Join(ownerRoot, "continuation"), OwnerNamespace: "danny.live.probe", Retention: 64}); err != nil {
		return runtimeprobe.Bundle{}, err
	}

	events := make([]runtimeprobe.Event, 0, 32)
	failures := make([]string, 0, len(forms))
	for _, form := range forms {
		formCtx, cancel := context.WithTimeout(ctx, liveFormTimeout)
		formEvents, formErr := form.run(formCtx, client, ownerRoot)
		cancel()
		events = append(events, formEvents...)
		if formErr != nil {
			failures = append(failures, form.name+": "+formErr.Error())
		}
	}

	steleClient, err := stele.NewClient(os.Getenv("STELE_URL"), http.DefaultClient)
	if err != nil {
		return runtimeprobe.Bundle{}, err
	}
	if steleClient != nil {
		st, stErr := steleClient.Context(ctx, runtimeprobe.ProbeRequest{CorrelationID: "live:stele", ParentID: "live:stele:parent", Input: "probe", Capability: "context"})
		events = append(events, st.Events...)
		if stErr != nil {
			failures = append(failures, "stele context: "+stErr.Error())
		}
	} else {
		now := time.Unix(0, 0).UTC()
		base := len(events)
		events = append(events,
			runtimeprobe.Event{Sequence: int64(base + 1), ID: "live:stele:parent", Kind: runtimeprobe.KindParent, CorrelationID: "live:stele", Capability: "context", Status: runtimeprobe.StatusSkippedUnconfigured, At: now},
			runtimeprobe.Event{Sequence: int64(base + 2), ID: "live:stele:terminal", Kind: runtimeprobe.KindTerminal, ParentID: "live:stele:parent", CorrelationID: "live:stele", Capability: "context", Status: runtimeprobe.StatusSkippedUnconfigured, At: now.Add(time.Nanosecond)},
		)
	}
	for index := range events {
		events[index].Sequence = int64(index + 1)
	}
	bundle := runtimeprobe.Bundle{SchemaVersion: runtimeprobe.EvidenceSchemaVersion, BaymaxVersion: client.Version(), SteleVersion: stele.PinnedVersion, Events: events, ProviderCompat: providerCompat}
	if err := bundle.Validate(); err != nil {
		return runtimeprobe.Bundle{}, err
	}
	if len(failures) > 0 {
		return bundle, fmt.Errorf("live smoke incomplete: %s", strings.Join(failures, "; "))
	}
	return bundle, nil
}
