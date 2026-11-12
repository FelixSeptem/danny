package baymax

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	bmtypes "github.com/FelixSeptem/baymax/core/types"
	"github.com/FelixSeptem/baymax/orchestration/composer"
	"github.com/FelixSeptem/baymax/orchestration/mailbox"
	"github.com/FelixSeptem/baymax/orchestration/scheduler"
	"github.com/FelixSeptem/baymax/orchestration/teams"
)

// The live pause/restore gate (task 7.3, Milestone 8 exit) needs product
// entry points that pause a real authorized execution at a source-owned safe
// point and restore it in a separate process. These APIs wrap the same
// production owners that RunChildAuthorized/RunTeamAuthorized/RunMailboxAuthorized
// use; they never bypass authorization or fabricate terminal evidence.

// ChildPauseResult is the ticket a pausing process hands to a fresh process.
// Every field is source-owned continuation data; none of it is executable.
type ChildPauseResult struct {
	Ref          composer.ChildContinuationRef `json:"ref"`
	GraphDigest  string                        `json:"graph_digest"`
	Generation   uint64                        `json:"generation"`
	Target       composer.ChildTarget          `json:"target"`
	PauseReason  string                        `json:"pause_reason"`
	CheckpointID string                        `json:"checkpoint_id"`
}

// ChildResumeAuthorizedRequest carries the persisted continuation identity
// back into a fresh process. The executor decides whether restore commits
// directly (production default) or completes the child work first.
type ChildResumeAuthorizedRequest struct {
	Ref            composer.ChildContinuationRef `json:"ref"`
	GraphDigest    string                        `json:"graph_digest"`
	Generation     uint64                        `json:"generation"`
	Target         composer.ChildTarget          `json:"target"`
	IdempotencyKey string                        `json:"idempotency_key"`
}

// ChildResumeResult reports the terminal commit of a restore. Duplicate means
// a prior restore already committed this continuation and nothing re-ran.
type ChildResumeResult struct {
	CommitStatus string `json:"commit_status"`
	Duplicate    bool   `json:"duplicate"`
}

// PauseChildAuthorized claims the authorized child assignment and persists a
// source-owned pause checkpoint without executing any model work. The claim
// itself is the pending action; the checkpoint records the exact pause reason
// and the authority digests so a fresh process can restore it.
func (c *Client) PauseChildAuthorized(ctx context.Context, req AuthorizedRequest, reason string) (ChildPauseResult, error) {
	if c == nil || !validNativePauseReason(reason) || req.RunID == "" || req.ParentRunID == "" || req.CorrelationID == "" || req.PlanVersion == "" || req.CapabilityDigest == "" {
		return ChildPauseResult{}, ErrAuthorizedRequest
	}
	comp, err := c.childComposer(req.RunID)
	if err != nil {
		return ChildPauseResult{}, err
	}
	task := scheduler.Task{TaskID: req.RunID, RunID: req.RunID, ParentRunID: req.ParentRunID, MaxAttempts: 2, Payload: map[string]any{"correlation_id": req.CorrelationID, "plan_version": req.PlanVersion, "capability_digest": req.CapabilityDigest}}
	if _, err := comp.Scheduler().Enqueue(ctx, task); err != nil {
		return ChildPauseResult{}, err
	}
	claimed, ok, err := comp.Scheduler().Claim(ctx, "danny-authorized-child")
	if err != nil || !ok {
		return ChildPauseResult{}, fmt.Errorf("scheduler claim: ok=%v err=%v", ok, err)
	}
	now := time.Now().UTC()
	continuation, err := comp.PersistChildPause(ctx, composer.ChildContinuation{
		Ref:                    composer.ChildContinuationRef{OwnerNamespace: "child"},
		Target:                 composer.ChildTargetLocal,
		Claimed:                claimed,
		GraphID:                "danny-fanout",
		GraphVersion:           "v1",
		GraphDigest:            "sha256:live-child-graph",
		ParentRunID:            req.ParentRunID,
		RunID:                  req.RunID,
		SessionID:              req.CorrelationID,
		CausationID:            req.CorrelationID,
		AuthorityDigest:        req.CapabilityDigest,
		PlanDigest:             req.PlanDigest,
		WriteScopeDigest:       req.WriteScopeDigest,
		EffectNamespace:        req.EffectNamespace,
		OccurrenceID:           req.CorrelationID + ":occurrence",
		PendingActionReference: req.RunID,
		Generation:             1,
		CreatedAt:              now,
		ExpiresAt:              now.Add(time.Hour),
	}, composer.ChildPauseError{Reason: composer.ChildPauseReason(reason), PendingActionReference: req.RunID, OriginalDeadline: now.Add(time.Hour)})
	if err != nil {
		return ChildPauseResult{}, err
	}
	return ChildPauseResult{
		Ref: continuation.Ref, GraphDigest: continuation.GraphDigest, Generation: continuation.Generation,
		Target: continuation.Target, PauseReason: reason, CheckpointID: continuation.Ref.CheckpointID,
	}, nil
}

// ResumeChildAuthorized restores a paused child in the calling process. A nil
// executor keeps the production commit-only restore; the live harness passes
// an executor that completes the child work through the authorized boundary.
func (c *Client) ResumeChildAuthorized(ctx context.Context, req ChildResumeAuthorizedRequest, executor composer.ChildContinuationExecutor) (ChildResumeResult, error) {
	if c == nil || req.Ref.RunID == "" || req.IdempotencyKey == "" {
		return ChildResumeResult{}, ErrAuthorizedRequest
	}
	var extra []composer.Option
	if executor != nil {
		extra = append(extra, composer.WithChildContinuationExecutor(executor))
	}
	comp, err := c.childComposer(req.Ref.RunID, extra...)
	if err != nil {
		return ChildResumeResult{}, err
	}
	out, err := comp.ResumeChildContinuation(ctx, composer.ChildResumeRequest{
		Ref: req.Ref, GraphDigest: req.GraphDigest, Generation: req.Generation,
		Target: req.Target, IdempotencyKey: req.IdempotencyKey,
	})
	if err != nil {
		return ChildResumeResult{}, err
	}
	return ChildResumeResult{CommitStatus: string(out.Commit.Status), Duplicate: out.CommitMeta.Duplicate}, nil
}

// TeamPauseResult hands a paused team checkpoint to a fresh process.
type TeamPauseResult struct {
	Ref          teams.TeamContinuationRef `json:"ref"`
	PauseReason  string                    `json:"pause_reason"`
	CheckpointID string                    `json:"checkpoint_id"`
}

// TeamResumeAuthorizedRequest carries the persisted team checkpoint identity.
type TeamResumeAuthorizedRequest struct {
	Ref            teams.TeamContinuationRef `json:"ref"`
	IdempotencyKey string                    `json:"idempotency_key"`
}

// TeamResumeResult projects the restored aggregate. ModelRuns records how many
// member executions this restore actually performed, so evidence can prove
// that completed members were not re-run.
type TeamResumeResult struct {
	Status      string             `json:"status"`
	WinnerVote  string             `json:"winner_vote,omitempty"`
	Members     []TeamMemberResult `json:"members,omitempty"`
	ModelRuns   int                `json:"model_runs"`
	TeamSuccess int                `json:"team_success"`
	TeamFailed  int                `json:"team_failed"`
}

// PauseTeamAuthorized runs the authorized team until the pause boundary fires
// before any member dispatches. Upstream owns a single batch-level safe
// boundary for parallel/vote teams: pausing mid-batch would persist an
// earlier, never-executed member as in-flight and fabricate recovery
// material, so the engine validates every member before the first one runs.
// The checkpoint lands in the same file store the resume process will read.
func (c *Client) PauseTeamAuthorized(ctx context.Context, req TeamRequest, reason string) (TeamPauseResult, error) {
	if c == nil || !validNativePauseReason(reason) || req.RunID == "" || req.TeamID == "" || req.CorrelationID == "" || req.PlanVersion == "" || req.PlanDigest == "" || req.CapabilityDigest == "" || req.ProjectionDigest == "" || req.WriteScopeDigest == "" || req.EffectNamespace == "" || len(req.Members) == 0 || len(req.Members) > 8 {
		return TeamPauseResult{}, ErrAuthorizedRequest
	}
	strategy := req.Strategy
	if strategy == "" {
		strategy = teams.StrategyParallel
	}
	if strategy != teams.StrategyParallel && strategy != teams.StrategyVote {
		return TeamPauseResult{}, fmt.Errorf("unsupported authorized team strategy %q", strategy)
	}
	tasks, err := authorizedTeamTasks(c, req, strategy, nil)
	if err != nil {
		return TeamPauseResult{}, err
	}
	now := time.Now().UTC()
	controller := teams.TeamPauseControllerFunc(func(context.Context, teams.TeamPauseBoundary) error {
		return teams.TeamPauseError{Reason: teams.TeamPauseReason(reason), PendingActionReference: req.TeamID, OriginalDeadline: now.Add(time.Hour)}
	})
	source := bmtypes.SourceRef{Kind: bmtypes.HistorySourceTeam, Namespace: "danny.baymax.team.history.v1", ID: req.TeamID}
	engine, _, err := c.teamEngine(req.TeamID, source, teams.WithTeamPauseController(controller))
	if err != nil {
		return TeamPauseResult{}, err
	}
	// The plan must digest-identically match the resume plan (upstream
	// rejects a foreign graph otherwise), so the worker count stays the
	// requested one; the batch boundary pauses before any dispatch anyway.
	workers := req.ParallelMaxWorkers
	if workers <= 0 {
		workers = len(tasks)
	}
	aggregate, runErr := engine.Run(ctx, teamPlan(req, strategy, tasks, workers))
	if !errors.Is(runErr, teams.ErrTeamPaused) || aggregate.TeamCheckpoint == nil {
		return TeamPauseResult{}, fmt.Errorf("expected team pause, got err=%v checkpoint=%v", runErr, aggregate.TeamCheckpoint)
	}
	return TeamPauseResult{Ref: *aggregate.TeamCheckpoint, PauseReason: reason, CheckpointID: aggregate.TeamCheckpoint.CheckpointID}, nil
}

// ResumeTeamAuthorized restores a paused team in the calling process with real
// member runners. Pending members execute through the authorized boundary;
// members already settled at the pause point are never re-run.
func (c *Client) ResumeTeamAuthorized(ctx context.Context, req TeamRequest, resume TeamResumeAuthorizedRequest) (TeamResumeResult, error) {
	if c == nil || resume.Ref.TeamID == "" || resume.IdempotencyKey == "" {
		return TeamResumeResult{}, ErrAuthorizedRequest
	}
	store, err := c.teamContinuationStore(resume.Ref.TeamID)
	if err != nil {
		return TeamResumeResult{}, err
	}
	continuation, err := store.Load(ctx, resume.Ref)
	if err != nil {
		return TeamResumeResult{}, err
	}
	strategy := req.Strategy
	if strategy == "" {
		strategy = teams.StrategyParallel
	}
	if strategy != continuation.Strategy {
		return TeamResumeResult{}, fmt.Errorf("resume strategy %q does not match persisted %q", strategy, continuation.Strategy)
	}
	var modelRuns int64
	tasks, err := authorizedTeamTasks(c, req, strategy, &modelRuns)
	if err != nil {
		return TeamResumeResult{}, err
	}
	ownerNamespace := "danny.baymax.team.history.v1"
	if c.continuationOwners != nil {
		ownerNamespace = c.continuationOwners.config.OwnerNamespace
	}
	source := bmtypes.SourceRef{Kind: bmtypes.HistorySourceTeam, Namespace: "danny.baymax.team.history.v1", ID: resume.Ref.TeamID}
	engine, _, err := c.teamEngine(resume.Ref.TeamID, source)
	if err != nil {
		return TeamResumeResult{}, err
	}
	request := teams.TeamResumeRequest{
		Ref: continuation.Ref, GraphDigest: continuation.GraphDigest, Generation: continuation.Generation,
		Strategy: continuation.Strategy, IdempotencyKey: resume.IdempotencyKey,
		AuthorityDigest: continuation.AuthorityDigest, PlanDigest: continuation.PlanDigest,
		WriteScopeDigest: continuation.WriteScopeDigest, EffectNamespace: continuation.EffectNamespace,
		OccurrenceID: continuation.OccurrenceID, CausationID: continuation.CausationID,
		Deployment: bmtypes.DeploymentRequest{OwnerNamespace: ownerNamespace, WriterFence: "fence:" + ownerNamespace},
	}
	workers := req.ParallelMaxWorkers
	if workers <= 0 {
		workers = len(tasks)
	}
	final, err := engine.ResumeContinuation(ctx, teamPlan(req, strategy, tasks, workers), request, nil)
	if err != nil {
		return TeamResumeResult{}, err
	}
	out := TeamResumeResult{Status: string(resolveTeamStatus(final)), WinnerVote: final.WinnerVote, TeamSuccess: final.TeamTaskTotal - final.TeamTaskFailed, TeamFailed: final.TeamTaskFailed, ModelRuns: int(modelRuns)}
	for _, task := range final.Tasks {
		if member, ok := task.Output.(TeamMemberResult); ok {
			out.Members = append(out.Members, member)
		}
	}
	return out, nil
}

// MailboxPauseResult hands a paused mailbox delivery to a fresh process.
type MailboxPauseResult struct {
	Ref          mailbox.MailboxContinuationRef `json:"ref"`
	Binding      mailbox.HostPlanBinding        `json:"binding"`
	PauseReason  string                         `json:"pause_reason"`
	Generation   uint64                         `json:"generation"`
	CheckpointID string                         `json:"checkpoint_id"`
}

// MailboxResumeAuthorizedRequest carries the persisted mailbox checkpoint.
type MailboxResumeAuthorizedRequest struct {
	Ref            mailbox.MailboxContinuationRef `json:"ref"`
	Binding        mailbox.HostPlanBinding        `json:"binding"`
	IdempotencyKey string                         `json:"idempotency_key"`
	Generation     uint64                         `json:"generation"`
	PauseReason    string                         `json:"pause_reason"`
}

// MailboxResumeResult projects the restore outcome. Duplicate marks a resumed
// delivery that had already been committed by an earlier restore.
type MailboxResumeResult struct {
	TerminalOutcome string `json:"terminal_outcome"`
	Duplicate       bool   `json:"duplicate"`
}

// PauseMailboxAuthorized admits the logical message through the native
// host-plan boundary and drives one delivery through the configured handler.
// The mailbox itself is built by the configured continuation owner (the same
// store the resume process reads), so req.Mailbox is not consulted here. The
// handler bound via BindContinuationOwners decides the safe point; when it
// pauses, the checkpoint is durable in the mailbox owner file store.
func (c *Client) PauseMailboxAuthorized(ctx context.Context, req MailboxRequest, reason string) (MailboxPauseResult, error) {
	if c == nil || !validNativePauseReason(reason) || req.ConsumerID == "" || req.Lease <= 0 || req.MessageID == "" || req.IdempotencyKey == "" || req.RunID == "" || req.CorrelationID == "" || req.ParentRunID == "" || req.CausationID == "" || req.AuthorityDigest == "" || req.PlanDigest == "" || req.WriteScope == "" || req.EffectNamespace == "" || req.Occurrence == "" || req.ResourceLimit <= 0 || req.PlanVersion == "" || req.CapabilityDigest == "" || req.ProjectionDigest == "" {
		return MailboxPauseResult{}, ErrMailboxRequest
	}
	mb, worker, err := c.mailboxOwner(req.RunID)
	if err != nil {
		return MailboxPauseResult{}, err
	}
	binding := mailbox.HostPlanBinding{
		ContractVersion: mailbox.HostPlanBindingContractVersion,
		MessageID:       req.MessageID, IdempotencyKey: req.IdempotencyKey, RunID: req.RunID, ParentRunID: req.ParentRunID,
		CausationID: req.CausationID, AuthorityDigest: req.AuthorityDigest, PlanDigest: req.PlanDigest,
		WriteScope: req.WriteScope, EffectNamespace: req.EffectNamespace, Occurrence: req.Occurrence,
		HostCorrelation: req.CorrelationID, ResourceLimit: req.ResourceLimit,
	}
	if _, err := mb.AdmitHostPlan(ctx, binding); err != nil {
		return MailboxPauseResult{}, err
	}
	if _, err := worker.RunOnce(ctx); err != nil {
		return MailboxPauseResult{}, err
	}
	snapshot, err := mb.Snapshot(ctx)
	if err != nil {
		return MailboxPauseResult{}, err
	}
	for _, saved := range snapshot.Continuations {
		if saved.Continuation.MessageID == req.MessageID && saved.Continuation.Ref.CheckpointID != "" {
			continuation := saved.Continuation
			return MailboxPauseResult{
				Ref: continuation.Ref, Binding: binding, PauseReason: string(continuation.PauseReason),
				Generation: continuation.Generation, CheckpointID: continuation.Ref.CheckpointID,
			}, nil
		}
	}
	return MailboxPauseResult{}, fmt.Errorf("mailbox pause for %s did not persist a continuation checkpoint", req.MessageID)
}

// ResumeMailboxAuthorized restores a paused delivery in the calling process
// through the configured resumer. A duplicate resume converges on the stored
// outcome without invoking the resumer again.
func (c *Client) ResumeMailboxAuthorized(ctx context.Context, req MailboxResumeAuthorizedRequest) (MailboxResumeResult, error) {
	if c == nil || req.Ref.MessageID == "" || req.IdempotencyKey == "" || req.PauseReason == "" {
		return MailboxResumeResult{}, ErrMailboxRequest
	}
	_, worker, err := c.mailboxOwner(req.Ref.RunID)
	if err != nil {
		return MailboxResumeResult{}, err
	}
	outcome, err := worker.ResumeHandlerContinuation(ctx, mailbox.MailboxResumeRequest{
		Ref: req.Ref, Binding: req.Binding, IdempotencyKey: req.IdempotencyKey,
		Generation: req.Generation, PauseReason: mailbox.MailboxPauseReason(req.PauseReason),
		Deployment: bmtypes.DeploymentRequest{OwnerNamespace: "mailbox", WriterFence: "fence:mailbox"},
	})
	if err != nil {
		return MailboxResumeResult{}, err
	}
	return MailboxResumeResult{TerminalOutcome: outcome.TerminalOutcome}, nil
}

// teamPlan builds the native plan shared by the pause and resume paths so the
// graph identity stays stable across the process boundary.
func teamPlan(req TeamRequest, strategy teams.Strategy, tasks []teams.Task, workers int) teams.Plan {
	return teams.Plan{
		RunID: req.RunID, SessionID: req.CorrelationID, TeamID: req.TeamID,
		CausationID: req.CorrelationID, AuthorityDigest: req.CapabilityDigest,
		PlanDigest: req.PlanDigest, WriteScopeDigest: req.WriteScopeDigest,
		EffectNamespace: req.EffectNamespace, OccurrenceID: req.CorrelationID + ":occurrence",
		Strategy: strategy, Tasks: tasks, TaskTimeout: req.TaskTimeout,
		ParallelMaxWorkers: workers, Backpressure: req.Backpressure,
		FailurePolicy: req.FailurePolicy, VoteTieBreak: req.VoteTieBreak,
	}
}

func validNativePauseReason(reason string) bool {
	for _, candidate := range NativePauseReasons {
		if candidate == reason {
			return true
		}
	}
	return false
}

// authorizedTeamTasks builds the authorized member tasks shared by the run,
// pause and resume paths. When runs is non-nil it counts actual member model
// executions, so live evidence can prove which members re-ran.
func authorizedTeamTasks(c *Client, req TeamRequest, strategy teams.Strategy, runs *int64) ([]teams.Task, error) {
	tasks := make([]teams.Task, 0, len(req.Members))
	seen := make(map[string]struct{}, len(req.Members))
	for _, member := range req.Members {
		memberID := strings.TrimSpace(member.ID)
		if memberID == "" || member.Role == "" {
			return nil, ErrAuthorizedRequest
		}
		if _, ok := seen[memberID]; ok {
			return nil, fmt.Errorf("duplicate team member %q", memberID)
		}
		seen[memberID] = struct{}{}
		member := member
		tasks = append(tasks, teams.Task{TaskID: req.TeamID + ":" + memberID, AgentID: memberID, Role: teams.Role(member.Role), Target: teams.TaskTargetLocal, Runner: teams.TaskRunnerFunc(func(memberCtx context.Context, task teams.Task) (teams.TaskResult, error) {
			memberRunID := req.TeamID + ":" + member.ID
			if runs != nil {
				atomic.AddInt64(runs, 1)
			}
			out, runErr := c.RunAuthorized(memberCtx, AuthorizedRequest{RunID: memberRunID, ParentRunID: req.RunID, CorrelationID: req.CorrelationID + ":" + member.ID, PlanVersion: req.PlanVersion, PlanDigest: req.PlanDigest, CapabilityDigest: req.CapabilityDigest, ProjectionDigest: req.ProjectionDigest, WriteScopeDigest: req.WriteScopeDigest, EffectNamespace: req.EffectNamespace + ":" + member.ID, Role: member.Role, Input: member.Input, ToolPolicy: req.ToolPolicy})
			if runErr != nil {
				return teams.TaskResult{Output: TeamMemberResult{MemberID: member.ID, RunID: memberRunID, ParentRunID: req.RunID, Status: "failed", FailureClass: runErr.Error(), NativeCursor: out.NativeCursor, NativeHistoryDurable: out.NativeHistoryDurable, Events: out.Events}}, runErr
			}
			if out.Terminal.Status != "succeeded" {
				return teams.TaskResult{Output: TeamMemberResult{MemberID: member.ID, RunID: memberRunID, ParentRunID: req.RunID, Status: out.Terminal.Status, Result: out.Terminal.Result, FailureClass: out.Terminal.FailureClass, NativeCursor: out.NativeCursor, NativeHistoryDurable: out.NativeHistoryDurable, Events: out.Events}}, fmt.Errorf("team member %s terminal status %s", member.ID, out.Terminal.Status)
			}
			memberResult := TeamMemberResult{MemberID: member.ID, RunID: memberRunID, ParentRunID: req.RunID, Status: out.Terminal.Status, Result: out.Terminal.Result, FailureClass: out.Terminal.FailureClass, NativeCursor: out.NativeCursor, NativeHistoryDurable: out.NativeHistoryDurable, Events: out.Events}
			if strategy == teams.StrategyVote {
				vote, ok := normalizeMemberVote(out.Terminal.Result)
				if !ok {
					return teams.TaskResult{Output: memberResult}, fmt.Errorf("team member %s did not provide approve/reject decision evidence", member.ID)
				}
				return teams.TaskResult{Vote: vote, Output: memberResult}, nil
			}
			return teams.TaskResult{Output: memberResult}, nil
		})})
	}
	return tasks, nil
}

// resolveTeamStatus maps the native aggregate to the product status vocabulary
// used by TeamExecutionResult.
func resolveTeamStatus(final teams.Result) teams.TaskStatus {
	if final.TeamTaskFailed > 0 {
		return teams.TaskStatusFailed
	}
	return teams.TaskStatusSucceeded
}
