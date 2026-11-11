# Baymax Contract Evidence

This document records the public surfaces inspected for the pinned dependency
used by Milestone 8. It is an evidence index, not a claim that every mode is
production-ready.

## Pin

`github.com/FelixSeptem/baymax v0.0.0-20261103091945-d2e08cd6dc8c`

## Verified Public Surfaces

| Capability | Public surface | Evidence | Status |
| --- | --- | --- | --- |
| Direct Run/Stream | `core/runner.Engine.Run`, `Stream` | `internal/agent/baymax/adapter_test.go` | verified for source-owned terminal facts and Run/Stream terminal parity |
| Host admission | `core/runner.Engine.AdmitHostRun` and `types.HostRunStartAdmission` | `TestPinnedHostAdmissionReservesBeforeActivationAndRejectsDuplicate` | verified: admission precedes model activation, duplicate admission is source-rejected, and released admission can be retried |
| Runner native history | `core/runner.NewNativeEventHistory`, `WithNativeEventHistory`, `types.NativeEventHistoryReader` | `internal/agent/baymax/adapter_test.go` and pinned upstream runner history tests | verified for source identity, source sequence, retained terminal fact and opaque cursor with in-memory owner journal |
| Native event categories | runner `run.started`, `run.finished`, `tool.dispatch.*` source facts | `TestAuthorizedRunProjectsSourceIdentityAndOpaqueCursor`, `TestAuthorizedRunProjectsNativeToolLifecycle` | verified: parent/tool/diagnostic/terminal categories derive from retained native types, not callback names or local sequence |
| Authorized tool boundary | `core/runner.WithToolMiddlewares`, local dispatcher middleware chain | `TestAuthorizedToolPolicyRejectsUndeclaredMutationBeforeInvoke`, `TestAuthorizedToolPolicyRequiresLiveAuthorityForMutation`, `TestAuthorizedToolPolicyPassesFullyAuthorizedMutation` | verified: server capability allowlist, write scope, approval, task revision, effect namespace, and live mutation gate are checked before tool invocation |
| Child dispatch | `orchestration/composer.Composer.DispatchChild` | `TestAuthorizedChildDispatchesNativeAssignmentAndPreservesParent`, `TestAuthorizedChildHonorsAssignedBudget`, `TestAuthorizedChildBatchUsesStableAssignmentsAndNativeEvidence`, `TestHarnessUsesNativeBoundedChildBatchBoundary` | verified for product-authorized bounded batch dispatch, execution-derived `parent:child:N` identities, per-child deadline, cancellation propagation, per-child source terminal evidence, and failed-child commit blocking; pause-specific continuation remains separately limited |
| Parallel team | `orchestration/teams.Engine.Run`, `teams.WithNativeEventHistory` | `TestAuthorizedParallelTeamUsesExecutionDerivedMembersAndNativeAggregate`, `TestAuthorizedParallelTeamRetainsConcurrentNativeMemberAndTeamEvidence`, `TestAuthorizedParallelTeamAppliesNativeWorkerLimitAndTimeout`, `TestAuthorizedParallelTeamPropagatesCancellationToNativeMembers`, `TestAuthorizedParallelTeamRetainsFailureAndDoesNotFabricateSuccess` | verified for execution-derived team/member identity, actual concurrent native member activation, member and team source evidence/cursors, inherited authority, native aggregate, worker/timeout limits, cancellation, and failure propagation; pause-specific continuation remains unsupported |
| Vote team boundary | `orchestration/teams.Engine.Run` with `StrategyVote` | `TestAuthorizedVoteTeamUsesMemberEvidenceAndNativeTieBreak`, `TestAuthorizedVoteTeamRejectsMissingDecisionEvidence`, `TestAuthorizedVoteTeamRejectsOverBoundedParticipation` | verified: votes come from member terminal evidence, conflicting decisions use the requested native tie-break, each member retains native provenance, missing evidence/failure is rejected, and participation is bounded; pause-specific continuation remains unsupported |
| Workflow execution | `orchestration/workflow.Engine.Run`, `workflow.WithNativeEventHistory`, `workflow.WithWorkflowContinuationStore`, `workflow.ResumeContinuation` | `TestAuthorizedWorkflowUsesNativeDependencyOrderAndCorrelatedAssignments`, `TestAuthorizedWorkflowBlocksDependentStepAfterRequiredFailure`, `TestAuthorizedWorkflowReturnsNativeCheckpointAuthority`, `TestAuthorizedWorkflowEnforcesStepAndTotalResourceLimits` | verified for explicit file-backed workflow source history, native dependency ordering, real authorized step runs, bounded step/aggregate budgets, native failure propagation, and authority-bound continuation; deployment remains same-host single-writer |
| Mailbox continuation and scheduled caller | `orchestration/mailbox.AdmitHostPlan`, `ConsumeWithLease`, `Ack`, `Requeue`; `orchestration/scheduler.Claim/Complete` | `TestAuthorizedMailboxContinuationUsesOneNativeAdmissionAndAcksSuccess`, `TestAuthorizedMailboxContinuationRequeuesFailedExecution`, `TestAuthorizedScheduledCallerUsesOccurrenceAndCommonMailboxBoundary` | verified: one host-plan admission preserves occurrence/authority, native lease and retry/DLQ own lifecycle, and scheduled dispatch enters the same authorized execution boundary; mailbox is not an Agent pause-restore owner |
| Compilation fanout/aggregate/vote | `composer.DispatchChild`; `teams.Engine.Run` through `RunChildAuthorized` and `RunTeamAuthorized` | `TestCompileExecutorUsesBaymaxTeamAndChildBoundaries`, `TestCompileExecutorRejectsMissingDecisionEvidence`, `knowledge.TestCompileCoordinatorBlocksCommitWhenChildFails` | production compiler no longer calls runtimeprobe or uses a fixed `compile` team. Child output carries execution-derived run/parent identity, section source reference and native result; aggregate and vote derive team identity and inherited authority from that evidence. A vote requires actual `approve`/`reject` terminal output and failure blocks commit. |
| Cancellation and timeout | `core/runner.Engine.CancelRun`, standard context deadline terminalization | `TestAuthorizedControlCancelsActiveRunThroughNativeEngine`, `TestPinnedRunnerPreservesDeadlineTerminal` | adapter exposes authorized cancel with native accepted/duplicate status; context expiry remains source terminalization; durable product control binding pending |
| Causal retry | `core/runner.Engine.RetryRun` | `TestAuthorizedRetryUsesNativeCausationAndFreshRun`, `TestPinnedHostCancelAndCausalRetryContracts` | adapter starts a distinct native attempt with source causation and preserves the source terminal; durable product retry binding pending |
| Dynamic action checkpoint | `core/runner.Engine.ResumeDynamicAction` | cached upstream `core/runner/dynamic_action_resume.go` tests | limited same-run action path |

## Not Yet Verified

- Native history is explicit per source owner. Authorized runner, child member,
  and parallel-team paths now bind source-owned journals in-process; a callback
  handler or local sequence is not native history evidence. Durable multi-process
  source stores and product projection transactions remain separate gaps.
- The new file journal is durable only for same-host single-writer sequential
  reopen. It does not provide multi-writer CAS, cross-machine coordination, or
  multi-instance recovery.
- Mailbox history is source-specific lifecycle history; acknowledgement or DLQ
  terminal facts are not runner completion or a product commit.
- Public checkpoint restore coverage for `input_required`,
  `approval_required`, and `budget_exhausted` across runner, child, team,
  workflow and mailbox paths is not established by package presence.
- Native source retention and replay after a process restart require a concrete
  source store or a documented store extension point; the in-memory runner
  fixture proves source identity/cursor projection only, not cross-process
  durability.
- Danny authorized Run/Stream projects the in-memory runner journal in the
  current process. It is source-owned and satisfies the native activation
  projection contract, but is not a durable recovery claim; persistence and
  replay transaction wiring remain recovery tasks.

## Retention, Replay, and Continuation Matrix

The rows below describe the exact pinned public source contract, not Danny
production availability. File-backed history/continuation claims are limited to
same-host, single-writer sequential reopen after the previous writer releases
ownership. None of these contracts establishes multi-writer CAS, cross-machine
coordination, or multi-instance recovery.

| Form | Source identity and order | Retention, replay, and crash-before-projection | Checkpoint restore and pause coverage |
| --- | --- | --- | --- |
| Runner | `runner` source ref; source-local sequence and opaque cursor through `core/runner.WithNativeEventHistory` | Explicit owner journal only. Memory is process-local; file journal reopens sequentially, reports a retention floor, and rejects expired cursors. If append committed before Danny projection, the file owner can replay that fact; no atomic transaction joins the source append and Danny projection. | `Engine.ContinuationCapabilities` advertises restore only with a configured cross-process checkpoint store, dependency binding, approval and budget producers. The pinned `core/runner/native_continuation_process_test.go` exercises all three pauses (`input_required`, `approval_required`, `budget_exhausted`) in a new process. Deployment configuration remains required. |
| Child / composer | `child` source ref; source-local sequence and opaque cursor through `composer.WithNativeEventHistory` | Explicit child owner journal; same bounded memory/file journal semantics. Reopen/replay is source-fact recovery only and is not atomic with Danny projection. | Child continuation has memory/file stores and recovery contract tests, but composer README requires independent source evidence per pause reason and explicitly does not equate scheduler retry/recovery snapshots with pause continuation. No continuation capability source advertises all three pause reasons at this pin. Treat all three pause restores as unsupported for admission until such evidence is exposed. |
| Team | `team` source ref; source-local sequence and opaque cursor through `teams.WithNativeEventHistory` | Explicit team journal; same bounded memory/file journal semantics. Source facts can be replayed after sequential reopen when file-backed; no source/projection atomicity. | No team native continuation/checkpoint restore contract or pause capability declaration in the pinned public surface. `input_required`, `approval_required`, and `budget_exhausted` restore are unsupported. |
| Workflow | `workflow` source ref; source-local sequence and opaque cursor through `workflow.WithNativeEventHistory` | Explicit workflow journal; same bounded memory/file journal semantics. Source facts can be replayed after sequential reopen when file-backed; no source/projection atomicity. | `Engine.ContinuationCapabilities` advertises file-store cross-process restore under the workflow owner namespace. Pinned workflow continuation tests cover `input_required`, `approval_required`, and `budget_exhausted`, including new-engine restore. This is workflow-specific and same-host/single-writer only. |
| Mailbox | `mailbox.history.v1` source instance; lifecycle sequence and opaque cursor from `Mailbox.HistorySource`/`ReadAfter` | FileStore persists mailbox snapshot and reference-only lifecycle journal atomically. The pinned mailbox tests prove new-process replay, retention-floor/gap errors, and read-only replay. Replay covers mailbox transitions only, not native Agent events or application projection atomicity. | Mailbox is not an Agent continuation owner. `acked`, `dead_letter`, and `expired` are mailbox lifecycle outcomes, not runner completion or product commit. All three Agent pause restore reasons are unsupported by this contract. |

Executable evidence:

- Danny's `TestPinnedSourceHistoryOwnersPreserveIdentityOrderAndFileReplay`
  checks runner, child, team, and workflow source identity, source order,
  bounded retention, expired cursors, payload omission, and same-host
  sequential file reopen at the exact pin.
- Pinned mailbox `TestFileMailboxHistoryReplaysAfterRestartWithoutPayload`,
  `TestMailboxHistoryTerminalRestartProcess`, and
  `TestMailboxHistoryLifecycleAndCursorErrorsAreReadOnly` cover its distinct
  lifecycle journal contract.
- Pinned runner `TestNativeContinuationCrossProcess` and workflow
  `TestEngineResumeContinuationRestoresFromFileStoreOnNewEngine` are the
  pause-specific restore evidence named by their capability declarations.
- Pinned composer `orchestration/composer/README.md` and continuation tests are
  the explicit limit evidence: child continuation primitives exist, but the
  public form does not advertise a complete three-pause capability matrix.

No form provides a transaction spanning source history append and Danny
projection persistence. A source append that committed before projection can
be reconciled only by replaying that same owner source and deduplicating at the
product boundary; when the source store is unavailable, the correct result is
recovery-required rather than rerunning uncertain work.

## Evidence Rule

Every capability marked `verified` in product configuration must link to a
deterministic fake-model fixture or a real smoke record with the same pin.
Missing credentials or a missing native store is incomplete evidence. Danny
does not add a production runtime journal, fake checkpoint, unconditional vote,
or probe completion to hide an upstream gap.
