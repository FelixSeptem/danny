package baymax

import (
	"context"
	"errors"
	"testing"
	"time"

	bmtypes "github.com/FelixSeptem/baymax/core/types"
	"github.com/FelixSeptem/baymax/integration/fakes"
	"github.com/FelixSeptem/baymax/orchestration/composer"
	"github.com/FelixSeptem/baymax/orchestration/mailbox"
	"github.com/FelixSeptem/baymax/orchestration/scheduler"
	"github.com/FelixSeptem/baymax/orchestration/teams"
)

// TestChildOwnerRestoresEachPauseReasonAfterFreshProcess reproduces the exact
// checkpoint/restore condition from finding 2 on Danny's production adapter:
// a source-owned pause checkpoint is persisted by one process, then restored by
// a fresh set of owners without re-running completed effects.
//
// This is deterministic source evidence. It is NOT the task 7.3 live smoke and
// does not by itself promote the capability manifest entries.
func TestChildOwnerRestoresEachPauseReasonAfterFreshProcess(t *testing.T) {
	for _, reason := range NativePauseReasons {
		t.Run(reason, func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			const runID = "restore-parent-run"
			config := ContinuationOwnerConfig{Root: root, OwnerNamespace: "danny"}

			first, err := New(fakes.NewModel([]fakes.ModelStep{{Response: bmtypes.ModelResponse{FinalAnswer: "first"}}}))
			if err != nil {
				t.Fatal(err)
			}
			if err := first.BindContinuationOwners(config); err != nil {
				t.Fatal(err)
			}
			comp, err := first.childComposer(runID)
			if err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC()
			task := scheduler.Task{TaskID: "child-task-" + reason, RunID: runID, ParentRunID: "root", MaxAttempts: 2}
			if _, err := comp.Scheduler().Enqueue(ctx, task); err != nil {
				t.Fatal(err)
			}
			claimed, ok, err := comp.Scheduler().Claim(ctx, "danny-worker")
			if err != nil || !ok {
				t.Fatalf("scheduler claim = %+v, %v", claimed, err)
			}

			continuation, err := comp.PersistChildPause(ctx, composer.ChildContinuation{
				Ref:                    composer.ChildContinuationRef{OwnerNamespace: "child"},
				Target:                 composer.ChildTargetLocal,
				Claimed:                claimed,
				GraphID:                "danny-fanout",
				GraphVersion:           "v1",
				GraphDigest:            "graph-digest-" + reason,
				ParentRunID:            "root",
				RunID:                  runID,
				SessionID:              "session-1",
				CausationID:            "cause-1",
				AuthorityDigest:        "authority-1",
				PlanDigest:             "plan-1",
				WriteScopeDigest:       "scope-1",
				EffectNamespace:        "effect-1",
				OccurrenceID:           "occurrence-1",
				PendingActionReference: "action-1",
				CompletedEffectRefs:    []string{"effect:already-done"},
				Generation:             1,
				CreatedAt:              now,
				ExpiresAt:              now.Add(time.Hour),
			}, composer.ChildPauseError{Reason: composer.ChildPauseReason(reason), PendingActionReference: "action-1", ConsumedBudget: 7, OriginalDeadline: now.Add(time.Hour)})
			if err != nil {
				t.Fatal(err)
			}
			if len(continuation.CompletedEffectRefs) != 1 || continuation.PauseReason != composer.ChildPauseReason(reason) {
				t.Fatalf("persisted checkpoint lost pause/effect evidence: %+v", continuation)
			}

			// Fresh process: a new client and new owner instances bound to the
			// same root. No in-memory scheduler, journal or store is reused.
			fresh, err := New(fakes.NewModel([]fakes.ModelStep{{Response: bmtypes.ModelResponse{FinalAnswer: "fresh"}}}))
			if err != nil {
				t.Fatal(err)
			}
			if err := fresh.BindContinuationOwners(config); err != nil {
				t.Fatal(err)
			}
			freshComp, err := fresh.childComposer(runID)
			if err != nil {
				t.Fatal(err)
			}
			request := composer.ChildResumeRequest{
				Ref: continuation.Ref, GraphDigest: continuation.GraphDigest,
				Generation: continuation.Generation, Target: continuation.Target,
				IdempotencyKey: "fresh-process-" + reason,
			}
			result, err := freshComp.ResumeChildContinuation(ctx, request)
			if err != nil {
				t.Fatalf("fresh restore for %s: %v", reason, err)
			}
			if result.Commit.Status != scheduler.TaskStateSucceeded {
				t.Fatalf("fresh restore commit = %+v, want succeeded", result.Commit)
			}

			// A duplicate resume with the same idempotency key converges instead
			// of re-running the completed child effect.
			duplicate, err := freshComp.ResumeChildContinuation(ctx, request)
			if err != nil {
				t.Fatalf("duplicate restore for %s: %v", reason, err)
			}
			if !duplicate.CommitMeta.Duplicate {
				t.Fatalf("duplicate resume did not converge: %+v", duplicate.CommitMeta)
			}
		})
	}
}

// TestTeamOwnerRestoresEachPauseReasonAfterFreshProcess reproduces finding 2's
// checkpoint/restore condition for the team execution form through Danny's
// production team engine: the pause is produced by the engine that
// teamEngine builds, the checkpoint lands in the configured owner file, and a
// fresh client bound to the same root resumes it without replaying settled
// members.
func TestTeamOwnerRestoresEachPauseReasonAfterFreshProcess(t *testing.T) {
	const runID = "restore-team-run"
	const namespace = "danny.team.owner"
	for _, reason := range []teams.TeamPauseReason{teams.TeamPauseInputRequired, teams.TeamPauseApprovalRequired, teams.TeamPauseBudgetExhausted} {
		t.Run(string(reason), func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			source := bmtypes.SourceRef{Kind: bmtypes.HistorySourceTeam, Namespace: namespace + ".team.v1", ID: runID}
			memberRuns := 0
			plan := teamRestorePlan(runID, &memberRuns)

			first, err := New(fakes.NewModel([]fakes.ModelStep{{Response: bmtypes.ModelResponse{FinalAnswer: "paused"}}}))
			if err != nil {
				t.Fatal(err)
			}
			if err := first.BindContinuationOwners(ContinuationOwnerConfig{
				Root: root, OwnerNamespace: namespace,
				PauseBoundary: teams.TeamPauseControllerFunc(func(context.Context, teams.TeamPauseBoundary) error {
					return teams.TeamPauseError{Reason: reason, PendingActionReference: "action-" + string(reason), ConsumedBudget: 9}
				}),
			}); err != nil {
				t.Fatal(err)
			}
			pausedEngine, _, err := first.teamEngine(runID, source)
			if err != nil {
				t.Fatal(err)
			}
			paused, err := pausedEngine.Run(ctx, plan)
			if !errors.Is(err, teams.ErrTeamPaused) || paused.TeamCheckpoint == nil {
				t.Fatalf("pause result=%+v err=%v", paused, err)
			}
			if memberRuns != 0 {
				t.Fatalf("members ran before the pause boundary: %d", memberRuns)
			}

			// Read the checkpoint back through the same store the production
			// engine owns; the graph digest lives on the stored continuation.
			store, err := first.teamContinuationStore(runID)
			if err != nil {
				t.Fatal(err)
			}
			continuation, err := store.Load(ctx, *paused.TeamCheckpoint)
			if err != nil {
				t.Fatalf("load paused continuation: %v", err)
			}
			if continuation.PauseReason != reason || continuation.Generation != 1 {
				t.Fatalf("continuation pause=%s generation=%d", continuation.PauseReason, continuation.Generation)
			}

			// Fresh process: a new client, a new engine and a new store handle
			// over the same owner root. Nothing in memory is reused.
			fresh, err := New(fakes.NewModel([]fakes.ModelStep{{Response: bmtypes.ModelResponse{FinalAnswer: "fresh"}}}))
			if err != nil {
				t.Fatal(err)
			}
			if err := fresh.BindContinuationOwners(ContinuationOwnerConfig{Root: root, OwnerNamespace: namespace}); err != nil {
				t.Fatal(err)
			}
			freshEngine, _, err := fresh.teamEngine(runID, source)
			if err != nil {
				t.Fatal(err)
			}
			request := teams.TeamResumeRequest{
				Ref: continuation.Ref, GraphDigest: continuation.GraphDigest,
				Generation: continuation.Generation, Strategy: continuation.Strategy,
				IdempotencyKey:   "fresh-team-" + string(reason),
				AuthorityDigest:  continuation.AuthorityDigest,
				PlanDigest:       continuation.PlanDigest,
				WriteScopeDigest: continuation.WriteScopeDigest,
				EffectNamespace:  continuation.EffectNamespace,
				OccurrenceID:     continuation.OccurrenceID,
				CausationID:      continuation.CausationID,
				Deployment:       bmtypes.DeploymentRequest{OwnerNamespace: namespace, WriterFence: "fence:" + namespace},
			}
			final, err := freshEngine.ResumeContinuation(ctx, plan, request, nil)
			if err != nil {
				t.Fatalf("fresh restore for %s: %v", reason, err)
			}
			if final.TeamTaskTotal != 2 || final.TeamTaskFailed != 0 || final.Tasks[0].Status != teams.TaskStatusSucceeded || final.Tasks[1].Status != teams.TaskStatusSucceeded {
				t.Fatalf("restored team result = %+v", final)
			}
			if memberRuns != 2 {
				t.Fatalf("restored run executed %d members, want 2", memberRuns)
			}

			// A duplicate resume converges on the stored result without
			// re-running the members.
			duplicate, err := freshEngine.ResumeContinuation(ctx, plan, request, nil)
			if err != nil {
				t.Fatalf("duplicate restore for %s: %v", reason, err)
			}
			if duplicate.TeamTaskTotal != final.TeamTaskTotal || memberRuns != 2 {
				t.Fatalf("duplicate restore reran members: result=%+v runs=%d", duplicate, memberRuns)
			}
		})
	}
}

func teamRestorePlan(runID string, memberRuns *int) teams.Plan {
	runner := func(id string) teams.TaskRunnerFunc {
		return func(context.Context, teams.Task) (teams.TaskResult, error) {
			(*memberRuns)++
			return teams.TaskResult{Output: id}, nil
		}
	}
	return teams.Plan{
		RunID: runID, SessionID: "session-" + runID, TeamID: "team-" + runID,
		Strategy:              teams.StrategyParallel,
		ParallelMaxWorkers:    2,
		FailurePolicy:         teams.FailurePolicyFailFast,
		VoteTieBreak:          teams.VoteTieBreakFirstTaskID,
		TimeoutTerminalStatus: teams.TaskStatusFailed,
		Tasks: []teams.Task{
			{TaskID: "member-a", AgentID: "agent-a", Role: teams.RoleWorker, Target: teams.TaskTargetLocal, Runner: runner("member-a")},
			{TaskID: "member-b", AgentID: "agent-b", Role: teams.RoleWorker, Target: teams.TaskTargetLocal, Runner: runner("member-b")},
		},
	}
}

// TestMailboxOwnerRestoresEachPauseReasonAfterFreshProcess reproduces finding
// 2's checkpoint/restore condition for the mailbox handler form: the pause is
// produced by the handler Danny configures, the checkpoint is durable in the
// owner file store, and a fresh process resumer continues it exactly once.
func TestMailboxOwnerRestoresEachPauseReasonAfterFreshProcess(t *testing.T) {
	const runID = "restore-mailbox-run"
	for _, reason := range []mailbox.MailboxPauseReason{mailbox.MailboxPauseInputRequired, mailbox.MailboxPauseApprovalRequired, mailbox.MailboxPauseBudgetExhausted} {
		t.Run(string(reason), func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			now := time.Now().UTC()
			binding := mailbox.HostPlanBinding{
				ContractVersion: mailbox.HostPlanBindingContractVersion,
				MessageID:       "message-" + string(reason), IdempotencyKey: "logical-" + string(reason),
				RunID: runID, ParentRunID: "parent", CausationID: "cause",
				AuthorityDigest: "authority", PlanDigest: "sha256:plan", WriteScope: "scope",
				EffectNamespace: "effects", Occurrence: "occurrence", HostCorrelation: "host", ResourceLimit: 1,
			}
			handlerCalls := 0
			first, err := New(fakes.NewModel([]fakes.ModelStep{{Response: bmtypes.ModelResponse{FinalAnswer: "paused"}}}))
			if err != nil {
				t.Fatal(err)
			}
			if err := first.BindContinuationOwners(ContinuationOwnerConfig{
				Root: root, OwnerNamespace: "danny",
				Handler: func(context.Context, mailbox.Record) (mailbox.HandlerResult, error) {
					handlerCalls++
					return mailbox.HandlerResult{Pause: &mailbox.HandlerPause{
						Reason:              reason,
						CompletedEffectRefs: []string{"effect:done"},
						ConsumedBudget:      5,
						CheckpointReference: "handler-ckpt-" + string(reason),
						OriginalDeadline:    now.Add(time.Hour),
						ExpiresAt:           now.Add(time.Hour),
					}}, nil
				},
			}); err != nil {
				t.Fatal(err)
			}
			mb, worker, err := first.mailboxOwner(runID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := mb.AdmitHostPlan(ctx, binding); err != nil {
				t.Fatal(err)
			}
			if _, err := worker.RunOnce(ctx); err != nil {
				t.Fatalf("pause delivery: %v", err)
			}
			if handlerCalls != 1 {
				t.Fatalf("handler calls=%d, want 1", handlerCalls)
			}
			snapshot, err := mb.Snapshot(ctx)
			if err != nil {
				t.Fatal(err)
			}
			var continuation mailbox.MailboxContinuation
			for _, saved := range snapshot.Continuations {
				if saved.Continuation.MessageID == binding.MessageID {
					continuation = saved.Continuation
					break
				}
			}
			if continuation.Ref.CheckpointID == "" || continuation.PauseReason != reason || continuation.HandlerCheckpointReference == "" {
				t.Fatalf("persisted mailbox checkpoint = %+v", continuation)
			}

			// Fresh process: new client, new mailbox handle, new worker.
			freshHandlerCalls := 0
			resumerCalls := 0
			fresh, err := New(fakes.NewModel([]fakes.ModelStep{{Response: bmtypes.ModelResponse{FinalAnswer: "fresh"}}}))
			if err != nil {
				t.Fatal(err)
			}
			if err := fresh.BindContinuationOwners(ContinuationOwnerConfig{
				Root: root, OwnerNamespace: "danny",
				Handler: func(context.Context, mailbox.Record) (mailbox.HandlerResult, error) {
					freshHandlerCalls++
					return mailbox.HandlerResult{}, errors.New("fresh handler must not run during restore")
				},
				Resumer: func(_ context.Context, restored mailbox.MailboxContinuation, record mailbox.Record) (mailbox.HandlerResult, error) {
					resumerCalls++
					if restored.PauseReason != reason || record.AttemptCausation != continuation.AttemptCausation {
						t.Fatalf("restored pause/source mismatch: %+v %+v", restored, record)
					}
					if len(restored.CompletedEffectRefs) != 1 || restored.CompletedEffectRefs[0] != "effect:done" {
						t.Fatalf("completed effects were not retained: %+v", restored.CompletedEffectRefs)
					}
					return mailbox.HandlerResult{}, nil
				},
			}); err != nil {
				t.Fatal(err)
			}
			_, freshWorker, err := fresh.mailboxOwner(runID)
			if err != nil {
				t.Fatal(err)
			}
			request := mailbox.MailboxResumeRequest{
				Ref: continuation.Ref, Binding: binding,
				IdempotencyKey: "fresh-mailbox-" + string(reason),
				Generation:     continuation.Generation,
				PauseReason:    continuation.PauseReason,
				Deployment:     bmtypes.DeploymentRequest{OwnerNamespace: "mailbox", WriterFence: "fence:mailbox"},
			}
			outcome, err := freshWorker.ResumeHandlerContinuation(ctx, request)
			if err != nil {
				t.Fatalf("fresh restore for %s: %v", reason, err)
			}
			duplicate, err := freshWorker.ResumeHandlerContinuation(ctx, request)
			if err != nil {
				t.Fatalf("duplicate restore for %s: %v", reason, err)
			}
			if outcome.TerminalOutcome != "acked" || duplicate.TerminalOutcome != "acked" || resumerCalls != 1 || freshHandlerCalls != 0 {
				t.Fatalf("restore outcome=%+v duplicate=%+v resumerCalls=%d freshHandlerCalls=%d", outcome, duplicate, resumerCalls, freshHandlerCalls)
			}
		})
	}
}
