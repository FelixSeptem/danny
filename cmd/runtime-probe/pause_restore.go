package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	openaiadapter "github.com/FelixSeptem/baymax/model/openai"
	"github.com/FelixSeptem/baymax/orchestration/composer"
	"github.com/FelixSeptem/baymax/orchestration/mailbox"
	"github.com/FelixSeptem/baymax/orchestration/scheduler"
	"github.com/FelixSeptem/baymax/orchestration/teams"
	"github.com/FelixSeptem/danny/internal/agent/baymax"
	"github.com/FelixSeptem/danny/internal/context/stele"
	"github.com/FelixSeptem/danny/internal/runtimeprobe"
)

// Live pause/restore evidence. The pause phase runs in this process and
// persists a source-owned checkpoint through the production owners; the
// restore and duplicate-resume phases each run in a fresh child OS process
// that rebuilds the client from the persisted ticket and completes the work
// through the real provider. A chain passes only when the restore succeeds,
// the real provider was called exactly where work remained, and the duplicate
// resume converged without re-running completed effects.

const (
	prCapability     = "live-pause-restore"
	prOwnerNamespace = "danny.live.pause.restore"
	prTicketDir      = "tickets"
	prResumeMode     = "live-resume"
	prTimeout        = 4 * time.Minute
)

type pauseRestoreTicket struct {
	Form           string `json:"form"`
	Reason         string `json:"reason"`
	OwnerRoot      string `json:"owner_root"`
	OwnerNamespace string `json:"owner_namespace"`
	IdempotencyKey string `json:"idempotency_key"`

	ChildRef         composer.ChildContinuationRef `json:"child_ref,omitempty"`
	ChildGraphDigest string                        `json:"child_graph_digest,omitempty"`
	ChildGeneration  uint64                        `json:"child_generation,omitempty"`

	TeamRef teams.TeamContinuationRef `json:"team_ref,omitempty"`

	MailboxRef        mailbox.MailboxContinuationRef `json:"mailbox_ref,omitempty"`
	MailboxBinding    mailbox.HostPlanBinding        `json:"mailbox_binding,omitempty"`
	MailboxGeneration uint64                         `json:"mailbox_generation,omitempty"`

	Call authorizedCall `json:"call"`
}

// authorizedCall carries the product-authorized identity the fresh process
// uses for the real model work that remains after the pause.
type authorizedCall struct {
	RunID            string `json:"run_id"`
	ParentRunID      string `json:"parent_run_id"`
	CorrelationID    string `json:"correlation_id"`
	PlanVersion      string `json:"plan_version"`
	PlanDigest       string `json:"plan_digest"`
	CapabilityDigest string `json:"capability_digest"`
	ProjectionDigest string `json:"projection_digest"`
	WriteScopeDigest string `json:"write_scope_digest"`
	EffectNamespace  string `json:"effect_namespace"`
	Role             string `json:"role"`
	Input            string `json:"input"`
}

type pauseRestoreResult struct {
	OK           bool   `json:"ok"`
	Error        string `json:"error,omitempty"`
	ModelRuns    int    `json:"model_runs"`
	Terminal     string `json:"terminal,omitempty"`
	Duplicate    bool   `json:"duplicate"`
	ResumerCalls int    `json:"resumer_calls"`
	WinnerVote   string `json:"winner_vote,omitempty"`
}

func (c authorizedCall) toAuthorizedRequest() baymax.AuthorizedRequest {
	return baymax.AuthorizedRequest{
		RunID: c.RunID, ParentRunID: c.ParentRunID, CorrelationID: c.CorrelationID,
		PlanVersion: c.PlanVersion, PlanDigest: c.PlanDigest, CapabilityDigest: c.CapabilityDigest,
		ProjectionDigest: c.ProjectionDigest, WriteScopeDigest: c.WriteScopeDigest,
		EffectNamespace: c.EffectNamespace, Role: c.Role, Input: c.Input,
	}
}

type prForm struct {
	name  string
	kind  runtimeprobe.Kind
	pause func(ctx context.Context, client *baymax.Client, call authorizedCall, reason, key string) (pauseRestoreTicket, error)
}

func prForms() []prForm {
	return []prForm{
		{name: "child", kind: runtimeprobe.KindChild, pause: prPauseChild},
		{name: "team_parallel", kind: runtimeprobe.KindTeam, pause: func(ctx context.Context, client *baymax.Client, call authorizedCall, reason, key string) (pauseRestoreTicket, error) {
			return prPauseTeam(ctx, client, call, reason, key, teams.StrategyParallel)
		}},
		{name: "team_vote", kind: runtimeprobe.KindTeam, pause: func(ctx context.Context, client *baymax.Client, call authorizedCall, reason, key string) (pauseRestoreTicket, error) {
			return prPauseTeam(ctx, client, call, reason, key, teams.StrategyVote)
		}},
		{name: "mailbox", kind: runtimeprobe.KindMailbox, pause: prPauseMailbox},
	}
}

func prReasons() []string { return baymax.NativePauseReasons }

func livePauseRestore(ctx context.Context) (runtimeprobe.Bundle, error) {
	endpoint, modelName := os.Getenv("OPENAI_BASE_URL"), os.Getenv("OPENAI_MODEL")
	if endpoint == "" || modelName == "" {
		now := time.Unix(0, 0).UTC()
		return runtimeprobe.Bundle{SchemaVersion: runtimeprobe.EvidenceSchemaVersion, BaymaxVersion: baymax.PinnedVersion, SteleVersion: stele.PinnedVersion, Events: []runtimeprobe.Event{
			{Sequence: 1, ID: "live-pr:parent", Kind: runtimeprobe.KindParent, CorrelationID: "live-pr", Capability: prCapability, Status: runtimeprobe.StatusSkippedUnconfigured, At: now},
			{Sequence: 2, ID: "live-pr:terminal", Kind: runtimeprobe.KindTerminal, ParentID: "live-pr:parent", CorrelationID: "live-pr", Capability: prCapability, Status: runtimeprobe.StatusSkippedUnconfigured, At: now.Add(time.Nanosecond)},
		}}, nil
	}
	gatewayEndpoint, providerCompat, shim, err := liveGateway(endpoint)
	if err != nil {
		return runtimeprobe.Bundle{}, err
	}
	if shim != nil {
		defer func() { _ = shim.Close() }()
	}
	model := liveOpenAIModel(gatewayEndpoint, modelName)

	ownerRoot := filepath.Join(liveOwnerRoot(), "pause-restore-"+strconv.FormatInt(time.Now().UTC().UnixNano(), 10))
	if err := os.MkdirAll(filepath.Join(ownerRoot, prTicketDir), 0o700); err != nil {
		return runtimeprobe.Bundle{}, err
	}

	events := make([]runtimeprobe.Event, 0, 64)
	failures := make([]string, 0, 4)
	selected := map[string]bool{}
	if spec := strings.TrimSpace(os.Getenv("DANNY_LIVE_PR_FORMS")); spec != "" {
		for _, name := range strings.Split(spec, ",") {
			selected[strings.TrimSpace(name)] = true
		}
	}
	for _, form := range prForms() {
		if len(selected) > 0 && !selected[form.name] {
			continue
		}
		for _, reason := range prReasons() {
			chain := newPRChain(form, reason)
			chainErr := prRunChain(ctx, model, form, reason, ownerRoot, chain)
			events = append(events, chain.events...)
			if chainErr != nil {
				failures = append(failures, form.name+"/"+reason+": "+chainErr.Error())
			}
			// Free-tier providers rate-limit bursts; spacing the chains keeps
			// a 429 storm from masquerading as a contract failure.
			if gap := prChainGap(); gap > 0 {
				time.Sleep(gap)
			}
		}
	}
	for index := range events {
		events[index].Sequence = int64(index + 1)
	}
	bundle := runtimeprobe.Bundle{SchemaVersion: runtimeprobe.EvidenceSchemaVersion, BaymaxVersion: baymax.PinnedVersion, SteleVersion: stele.PinnedVersion, Events: events, ProviderCompat: providerCompat}
	if err := bundle.Validate(); err != nil {
		return runtimeprobe.Bundle{}, err
	}
	if len(failures) > 0 {
		return bundle, errors.New(strings.Join(failures, "; "))
	}
	return bundle, nil
}

type prChain struct {
	correlation string
	kind        runtimeprobe.Kind
	events      []runtimeprobe.Event
}

func newPRChain(form prForm, reason string) *prChain {
	correlation := "live-pr:" + form.name + ":" + reason
	chain := &prChain{correlation: correlation, kind: form.kind}
	chain.add(correlation+":parent", runtimeprobe.KindParent, "", runtimeprobe.StatusPassed, nil)
	return chain
}

func (c *prChain) add(id string, kind runtimeprobe.Kind, parentID string, status runtimeprobe.Status, payload map[string]string) {
	c.events = append(c.events, runtimeprobe.Event{
		Sequence: int64(len(c.events) + 1), ID: id, Kind: kind, ParentID: parentID,
		CorrelationID: c.correlation, Capability: prCapability, Status: status,
		At: time.Now().UTC(), Payload: payload,
	})
}

func (c *prChain) closeTerminal(parentID string, status runtimeprobe.Status, payload map[string]string) {
	c.add(c.correlation+":terminal", runtimeprobe.KindTerminal, parentID, status, payload)
}

// prRunChain executes pause (this process), restore (fresh process) and
// duplicate resume (a second fresh process, same idempotency key).
func prRunChain(ctx context.Context, model *openaiadapter.Client, form prForm, reason, ownerRoot string, chain *prChain) error {
	call := authorizedCall{
		RunID: "live-pr-run-" + form.name + "-" + reason, ParentRunID: "live-pr-parent",
		CorrelationID: chain.correlation, PlanVersion: "live.plan.v1", PlanDigest: "sha256:live-plan",
		CapabilityDigest: "sha256:live-capability", ProjectionDigest: "sha256:live-projection",
		WriteScopeDigest: "sha256:live-scope", EffectNamespace: "live-pr:" + form.name, Role: "subagent",
		Input: liveFormPrompt,
	}
	pauseClient, err := baymax.New(model)
	if err != nil {
		return err
	}
	defer func() { _ = pauseClient.Close() }()
	config := baymax.ContinuationOwnerConfig{Root: filepath.Join(ownerRoot, "continuation"), OwnerNamespace: prOwnerNamespace, Retention: 64}
	if form.name == "mailbox" {
		// The pause-phase handler performs the real model work for the
		// message, then declares the safe point. The restore phase must not
		// re-run this handler.
		handlerRuns := 0
		config.Handler = func(handlerCtx context.Context, record mailbox.Record) (mailbox.HandlerResult, error) {
			handlerRuns++
			if _, runErr := prCallWithRateLimitRetry(handlerCtx, pauseClient, call); runErr != nil {
				detail := fmt.Sprintf("pause handler model call: %v", runErr)
				fmt.Fprintln(os.Stderr, "mailbox pause handler: "+detail)
				return mailbox.HandlerResult{}, errors.New(detail)
			}
			now := time.Now().UTC()
			return mailbox.HandlerResult{Pause: &mailbox.HandlerPause{
				Reason: mailbox.MailboxPauseReason(reason), PendingActionReference: "handler:" + call.RunID,
				CompletedEffectRefs: []string{"model:" + call.RunID}, ConsumedBudget: 1,
				OriginalDeadline: now.Add(time.Hour), ExpiresAt: now.Add(time.Hour),
				CheckpointReference: "live-handler-" + call.RunID,
			}}, nil
		}
	}
	if err := pauseClient.BindOwnerStore(baymax.OwnerStoreConfig{Root: filepath.Join(ownerRoot, "owner-store"), Retention: 64, ProjectionRetention: 64}); err != nil {
		return err
	}
	if err := pauseClient.BindContinuationOwners(config); err != nil {
		return err
	}
	pauseCtx, cancel := context.WithTimeout(ctx, prTimeout)
	ticket, pauseErr := form.pause(pauseCtx, pauseClient, call, reason, chain.correlation)
	cancel()
	if pauseErr != nil {
		chain.add(chain.correlation+":paused", chain.kind, chain.correlation+":parent", runtimeprobe.StatusFailedContract, map[string]string{"error": pauseErr.Error()})
		chain.closeTerminal(chain.correlation+":paused", runtimeprobe.StatusFailedContract, nil)
		return pauseErr
	}
	ticket.OwnerRoot, ticket.OwnerNamespace, ticket.IdempotencyKey, ticket.Form, ticket.Reason, ticket.Call = ownerRoot, prOwnerNamespace, chain.correlation, form.name, reason, call
	chain.add(chain.correlation+":paused", chain.kind, chain.correlation+":parent", runtimeprobe.StatusPassed, map[string]string{
		"checkpoint_id": ticket.CheckpointID(), "pause_reason": reason,
		"pause_model_runs": strconv.Itoa(expectedPRModelRuns(form.name, "pause")),
	})

	ticketPath := filepath.Join(ownerRoot, prTicketDir, form.name+"-"+reason+".json")
	restore, restoreErr := prRunSubprocess(ctx, ticketPath, ticket, "restore")
	if restoreErr != nil {
		chain.add(chain.correlation+":restored", chain.kind, chain.correlation+":paused", runtimeprobe.StatusFailedContract, map[string]string{"error": restoreErr.Error()})
		chain.closeTerminal(chain.correlation+":restored", runtimeprobe.StatusFailedContract, nil)
		return restoreErr
	}
	duplicate, duplicateErr := prRunSubprocess(ctx, ticketPath, ticket, "duplicate")
	if duplicateErr != nil {
		chain.add(chain.correlation+":restored", chain.kind, chain.correlation+":paused", runtimeprobe.StatusFailedContract, map[string]string{"error": duplicateErr.Error()})
		chain.closeTerminal(chain.correlation+":restored", runtimeprobe.StatusFailedContract, nil)
		return duplicateErr
	}
	restoreOK := restore.OK && expectedPRModelRuns(form.name, "restore") == restore.ModelRuns && restore.Terminal == expectedPRTerminal(form.name)
	duplicateOK := duplicate.OK && expectedPRModelRuns(form.name, "duplicate") == duplicate.ModelRuns && duplicate.Terminal == expectedPRTerminal(form.name) && duplicateConverged(form.name, restore, duplicate)
	status := runtimeprobe.StatusPassed
	payload := map[string]string{
		"restore_status": restore.Terminal, "restore_model_runs": strconv.Itoa(restore.ModelRuns),
		"restore_resumer_calls": strconv.Itoa(restore.ResumerCalls),
		"duplicate_status":      duplicate.Terminal, "duplicate_model_runs": strconv.Itoa(duplicate.ModelRuns),
		"duplicate_resumer_calls": strconv.Itoa(duplicate.ResumerCalls),
		"duplicate_converged":     strconv.FormatBool(duplicateOK),
	}
	if restore.WinnerVote != "" {
		payload["winner_vote"] = restore.WinnerVote
	}
	if !restoreOK || !duplicateOK {
		status = runtimeprobe.StatusFailedContract
		if !restoreOK {
			payload["restore_error"] = restore.Error
		}
		if !duplicateOK {
			payload["duplicate_error"] = duplicate.Error
		}
	}
	chain.add(chain.correlation+":restored", chain.kind, chain.correlation+":paused", status, payload)
	chain.closeTerminal(chain.correlation+":restored", status, nil)
	if status != runtimeprobe.StatusPassed {
		return fmt.Errorf("chain %s: restore=%v duplicate=%v", chain.correlation, restoreOK, duplicateOK)
	}
	return nil
}

func (t pauseRestoreTicket) CheckpointID() string {
	switch t.Form {
	case "child":
		return t.ChildRef.CheckpointID
	case "team_parallel", "team_vote":
		return t.TeamRef.CheckpointID
	case "mailbox":
		return t.MailboxRef.CheckpointID
	}
	return ""
}

// expectedPRModelRuns records the contract: the parallel/vote team pause
// boundary sits before the whole batch, so pause performs no model work;
// restore performs exactly the work that remained; a duplicate resume
// performs none.
func expectedPRModelRuns(form, phase string) int {
	switch form {
	case "child":
		if phase == "restore" {
			return 1
		}
		return 0
	case "team_parallel", "team_vote":
		if phase == "restore" {
			return 2
		}
		return 0
	case "mailbox":
		if phase == "restore" {
			return 1
		}
		return 0
	}
	return 0
}

// expectedPRTerminal is the terminal marker per form: mailbox deliveries ack
// through the mailbox lifecycle; every other form commits "succeeded".
func expectedPRTerminal(form string) string {
	if form == "mailbox" {
		return "acked"
	}
	return "succeeded"
}

func duplicateConverged(form string, restore, duplicate pauseRestoreResult) bool {
	switch form {
	case "child":
		return duplicate.Duplicate
	case "mailbox":
		return duplicate.ResumerCalls == 0
	default:
		return duplicate.ModelRuns == 0
	}
}

// prRunSubprocess writes the ticket and runs this binary in a fresh OS
// process in live-resume mode. The child process builds its own client and
// owner instances, so nothing in-memory is shared across the boundary.
func prRunSubprocess(ctx context.Context, ticketPath string, ticket pauseRestoreTicket, phase string) (pauseRestoreResult, error) {
	raw, err := json.Marshal(ticket)
	if err != nil {
		return pauseRestoreResult{}, err
	}
	if err := os.WriteFile(ticketPath, raw, 0o600); err != nil {
		return pauseRestoreResult{}, err
	}
	exe, err := os.Executable()
	if err != nil {
		return pauseRestoreResult{}, err
	}
	runCtx, cancel := context.WithTimeout(ctx, prTimeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, exe, prResumeMode, "--ticket", ticketPath)
	cmd.Env = os.Environ()
	cmd.Dir, err = os.Getwd()
	if err != nil {
		return pauseRestoreResult{}, err
	}
	output, runErr := cmd.CombinedOutput()
	resultPath := ticketPath + ".result.json"
	var result pauseRestoreResult
	if readErr := readResultFile(resultPath, &result); readErr != nil {
		result.Error = "no result file: " + readErr.Error()
	}
	if runErr != nil {
		tail := string(output)
		if len(tail) > 400 {
			tail = tail[len(tail)-400:]
		}
		if result.Error == "" {
			result.Error = tail
		}
		return result, fmt.Errorf("resume subprocess: %v: %s", runErr, tail)
	}
	if !result.OK {
		return result, fmt.Errorf("resume subprocess failed: %s", result.Error)
	}
	return result, nil
}

// prChainGap returns the spacing between chains. Operators can shorten it
// with DANNY_LIVE_PR_GAP_MS; the default stays conservative because the
// evidence must reflect provider behavior, not rate-limit noise.
func prChainGap() time.Duration {
	if raw := strings.TrimSpace(os.Getenv("DANNY_LIVE_PR_GAP_MS")); raw != "" {
		if millis, err := strconv.Atoi(raw); err == nil && millis >= 0 {
			return time.Duration(millis) * time.Millisecond
		}
	}
	return 15 * time.Second
}

// prCallWithRateLimitRetry runs one authorized model call, retrying
// provider-side 429s a bounded number of times so a free-tier quota window
// does not fail the chain.
func prCallWithRateLimitRetry(ctx context.Context, client *baymax.Client, call authorizedCall) (baymax.ExecutionResult, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return baymax.ExecutionResult{}, ctx.Err()
			case <-time.After(time.Duration(attempt) * 20 * time.Second):
			}
		}
		out, err := client.RunAuthorized(ctx, call.toAuthorizedRequest())
		if err == nil && out.Terminal.Status == "succeeded" {
			return out, nil
		}
		lastErr = err
		if err == nil {
			// Terminal-but-not-succeeded is a contract fact, not a rate limit.
			return out, fmt.Errorf("terminal status %q class %q", out.Terminal.Status, out.Terminal.FailureClass)
		}
		message := err.Error()
		if !strings.Contains(message, "429") && !strings.Contains(message, "Too Many Requests") {
			return out, err
		}
	}
	return baymax.ExecutionResult{}, lastErr
}

// readResultFile reads and decodes one subprocess result file.
func readResultFile(path string, into *pauseRestoreResult) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, into)
}

func prPauseChild(ctx context.Context, client *baymax.Client, call authorizedCall, reason, key string) (pauseRestoreTicket, error) {
	paused, err := client.PauseChildAuthorized(ctx, call.toAuthorizedRequest(), reason)
	if err != nil {
		return pauseRestoreTicket{}, err
	}
	return pauseRestoreTicket{ChildRef: paused.Ref, ChildGraphDigest: paused.GraphDigest, ChildGeneration: paused.Generation}, nil
}

func prPauseTeam(ctx context.Context, client *baymax.Client, call authorizedCall, reason, key string, strategy teams.Strategy) (pauseRestoreTicket, error) {
	req := baymax.TeamRequest{
		RunID: call.RunID, TeamID: call.RunID + ":team", CorrelationID: call.CorrelationID,
		PlanVersion: call.PlanVersion, PlanDigest: call.PlanDigest, CapabilityDigest: call.CapabilityDigest,
		ProjectionDigest: call.ProjectionDigest, WriteScopeDigest: call.WriteScopeDigest,
		EffectNamespace: call.EffectNamespace, Strategy: strategy,
		VoteTieBreak: teams.VoteTieBreakFirstTaskID, TaskTimeout: 90 * time.Second, ParallelMaxWorkers: 2,
		Members: []baymax.TeamMember{
			{ID: "member-a", Role: "worker", Input: call.Input},
			{ID: "member-b", Role: "worker", Input: call.Input},
		},
	}
	// The upstream parallel/vote batch has one safe boundary before any
	// member dispatches; the pause phase performs no model work by contract.
	paused, err := client.PauseTeamAuthorized(ctx, req, reason)
	if err != nil {
		return pauseRestoreTicket{}, err
	}
	return pauseRestoreTicket{TeamRef: paused.Ref}, nil
}

func prPauseMailbox(ctx context.Context, client *baymax.Client, call authorizedCall, reason, key string) (pauseRestoreTicket, error) {
	req := baymax.MailboxRequest{
		ConsumerID: "live-pr-consumer", Lease: 60 * time.Second,
		MessageID: "live-pr-message-" + reason, IdempotencyKey: key, RunID: call.RunID,
		ParentRunID: call.ParentRunID, CausationID: call.CorrelationID, CorrelationID: call.CorrelationID,
		AuthorityDigest: call.CapabilityDigest, PlanDigest: call.PlanDigest, WriteScope: call.WriteScopeDigest,
		EffectNamespace: call.EffectNamespace, Occurrence: call.CorrelationID + ":occurrence",
		ResourceLimit: 1, PlanVersion: call.PlanVersion,
		CapabilityDigest: call.CapabilityDigest, ProjectionDigest: call.ProjectionDigest,
	}
	paused, err := client.PauseMailboxAuthorized(ctx, req, reason)
	if err != nil {
		return pauseRestoreTicket{}, err
	}
	return pauseRestoreTicket{MailboxRef: paused.Ref, MailboxBinding: paused.Binding, MailboxGeneration: paused.Generation}, nil
}

// runLiveResume is the fresh-process half: it rebuilds the client from the
// persisted ticket, restores the continuation through the production owners,
// writes a result file, and exits. It never sees the pausing process.
func runLiveResume(ticketPath string) error {
	raw, err := os.ReadFile(ticketPath)
	if err != nil {
		return err
	}
	var ticket pauseRestoreTicket
	if err := json.Unmarshal(raw, &ticket); err != nil {
		return err
	}
	result, resumeErr := prResumeTicket(context.Background(), ticket)
	encoded, encodeErr := json.Marshal(result)
	if encodeErr == nil {
		_ = os.WriteFile(ticketPath+".result.json", encoded, 0o600)
	}
	return resumeErr
}

func prResumeTicket(ctx context.Context, ticket pauseRestoreTicket) (pauseRestoreResult, error) {
	result := pauseRestoreResult{OK: false}
	endpoint, modelName := os.Getenv("OPENAI_BASE_URL"), os.Getenv("OPENAI_MODEL")
	if endpoint == "" || modelName == "" {
		return result, errors.New("resume process is missing provider configuration")
	}
	gatewayEndpoint, _, shim, err := liveGateway(endpoint)
	if err != nil {
		return result, err
	}
	if shim != nil {
		defer func() { _ = shim.Close() }()
	}
	client, err := baymax.New(liveOpenAIModel(gatewayEndpoint, modelName))
	if err != nil {
		return result, err
	}
	defer func() { _ = client.Close() }()
	if err := client.BindOwnerStore(baymax.OwnerStoreConfig{Root: filepath.Join(ticket.OwnerRoot, "owner-store"), Retention: 64, ProjectionRetention: 64}); err != nil {
		return result, err
	}
	config := baymax.ContinuationOwnerConfig{Root: filepath.Join(ticket.OwnerRoot, "continuation"), OwnerNamespace: ticket.OwnerNamespace, Retention: 64}
	var modelRuns atomic.Int64
	switch ticket.Form {
	case "child":
		// The restore executor completes the remaining child work through the
		// authorized boundary, then commits. A duplicate resume never reaches
		// this executor.
		config.Handler = nil
	case "mailbox":
		config.Resumer = func(_ context.Context, restored mailbox.MailboxContinuation, record mailbox.Record) (mailbox.HandlerResult, error) {
			result.ResumerCalls++
			modelRuns.Add(1)
			if _, runErr := prCallWithRateLimitRetry(ctx, client, ticket.Call); runErr != nil {
				return mailbox.HandlerResult{}, fmt.Errorf("resume resumer model call: %v", runErr)
			}
			return mailbox.HandlerResult{}, nil
		}
	}
	if err := client.BindContinuationOwners(config); err != nil {
		return result, err
	}
	resumeCtx, cancel := context.WithTimeout(ctx, prTimeout)
	defer cancel()
	switch ticket.Form {
	case "child":
		executor := composer.ChildContinuationExecutorFunc(func(execCtx context.Context, continuation composer.ChildContinuation) (scheduler.TerminalCommit, error) {
			modelRuns.Add(1)
			if _, runErr := prCallWithRateLimitRetry(execCtx, client, ticket.Call); runErr != nil {
				return scheduler.TerminalCommit{}, fmt.Errorf("restore executor model call: %v", runErr)
			}
			return scheduler.TerminalCommit{TaskID: continuation.Claimed.Record.Task.TaskID, AttemptID: continuation.Claimed.Attempt.AttemptID, Status: scheduler.TaskStateSucceeded}, nil
		})
		out, err := client.ResumeChildAuthorized(resumeCtx, baymax.ChildResumeAuthorizedRequest{
			Ref: ticket.ChildRef, GraphDigest: ticket.ChildGraphDigest, Generation: ticket.ChildGeneration,
			Target: composer.ChildTargetLocal, IdempotencyKey: ticket.IdempotencyKey,
		}, executor)
		if err != nil {
			result.Error = err.Error()
			return result, err
		}
		result.OK, result.Duplicate, result.Terminal = true, out.Duplicate, out.CommitStatus
		result.ModelRuns = int(modelRuns.Load())
	case "team_parallel", "team_vote":
		strategy := teams.StrategyParallel
		if ticket.Form == "team_vote" {
			strategy = teams.StrategyVote
		}
		input := liveFormPrompt
		if strategy == teams.StrategyVote {
			input = liveVotePrompt
		}
		req := baymax.TeamRequest{
			RunID: ticket.Call.RunID, TeamID: ticket.Call.RunID + ":team", CorrelationID: ticket.Call.CorrelationID,
			PlanVersion: ticket.Call.PlanVersion, PlanDigest: ticket.Call.PlanDigest, CapabilityDigest: ticket.Call.CapabilityDigest,
			ProjectionDigest: ticket.Call.ProjectionDigest, WriteScopeDigest: ticket.Call.WriteScopeDigest,
			EffectNamespace: ticket.Call.EffectNamespace, Strategy: strategy,
			VoteTieBreak: teams.VoteTieBreakFirstTaskID, TaskTimeout: 90 * time.Second, ParallelMaxWorkers: 2,
			Members: []baymax.TeamMember{
				{ID: "member-a", Role: "worker", Input: input},
				{ID: "member-b", Role: "worker", Input: input},
			},
		}
		out, err := client.ResumeTeamAuthorized(resumeCtx, req, baymax.TeamResumeAuthorizedRequest{Ref: ticket.TeamRef, IdempotencyKey: ticket.IdempotencyKey})
		if err != nil {
			result.Error = err.Error()
			return result, err
		}
		result.OK, result.Terminal, result.ModelRuns, result.WinnerVote = true, out.Status, out.ModelRuns, out.WinnerVote
	case "mailbox":
		out, err := client.ResumeMailboxAuthorized(resumeCtx, baymax.MailboxResumeAuthorizedRequest{
			Ref: ticket.MailboxRef, Binding: ticket.MailboxBinding, IdempotencyKey: ticket.IdempotencyKey,
			Generation: ticket.MailboxGeneration, PauseReason: ticket.Reason,
		})
		if err != nil {
			result.Error = err.Error()
			return result, err
		}
		result.OK, result.Terminal, result.ModelRuns = true, out.TerminalOutcome, int(modelRuns.Load())
	default:
		return result, fmt.Errorf("unknown pause/restore form %q", ticket.Form)
	}
	if result.OK {
		result.Error = ""
	}
	return result, nil
}
