# Baymax Contract Evidence

This document records the public surfaces inspected for the pinned dependency
used by Milestone 8. It is an evidence index, not a claim that every mode is
production-ready.

## Pin

`github.com/FelixSeptem/baymax v0.0.0-20261110150429-cca95f31f71b`

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
  and parallel-team paths bind source-owned journals; a callback handler or local
  sequence is not native history evidence. With `BindOwnerStore` and
  `BindContinuationOwners` the journals are file-backed, but the atomicity limit
  below still applies.
- The file journal is durable only for same-host single-writer sequential
  reopen. It does not provide multi-writer CAS, cross-machine coordination, or
  multi-instance recovery; `core/history.OpenOwnerStore` rejects a wider
  deployment request instead of degrading silently.
- Mailbox history is source-specific lifecycle history; acknowledgement or DLQ
  terminal facts are not runner completion or a product commit.
- The pin publishes per-pause-reason continuation capability evidence for child,
  team and mailbox handler owners (upstream 194/198 and the team continuation
  contract), and Danny now wires all three. That is deterministic adapter
  evidence, not a live acceptance result: `internal/runtimeboundary/contracts.go`
  keeps child/team pause restore `unsupported` and mailbox handler restore
  unclaimed until the Milestone 8 live smoke (task 7.3) runs the real
  integration path.
- Cross-process replay requires the caller to bind an owner root. Without one,
  the adapter keeps its prior in-process behavior; nothing upgrades to durability
  implicitly.
- `Reconcile` only replays and deduplicates source facts. It never re-runs an
  effect whose commit is unknown; a missing or retention-gapped source returns
  recovery-required instead.

## Retention, Replay, and Continuation Matrix

The rows below describe the exact pinned public source contract, not Danny
production availability. File-backed history/continuation claims are limited to
same-host, single-writer sequential reopen after the previous writer releases
ownership. None of these contracts establishes multi-writer CAS, cross-machine
coordination, or multi-instance recovery.

| Form | Source identity and order | Retention, replay, and crash-before-projection | Checkpoint restore and pause coverage |
| --- | --- | --- | --- |
| Runner | `runner` source ref; source-local sequence and opaque cursor through `core/runner.WithNativeEventHistory` | Explicit owner journal only. Memory is process-local; file journal reopens sequentially, reports a retention floor, and rejects expired cursors. If append committed before Danny projection, the file owner can replay that fact; no atomic transaction joins the source append and Danny projection. | `Engine.ContinuationCapabilities` advertises restore only with a configured cross-process checkpoint store, dependency binding, approval and budget producers. The pinned `core/runner/native_continuation_process_test.go` exercises all three pauses (`input_required`, `approval_required`, `budget_exhausted`) in a new process. Deployment configuration remains required. |
| Child / composer | `child` source ref; source-local sequence and opaque cursor through `composer.WithNativeEventHistory` | Explicit child owner journal; same bounded memory/file journal semantics. Reopen/replay is source-fact recovery only and is not atomic with Danny projection. `BindContinuationOwners` makes the adapter bind a file history, a file child continuation store and a file scheduler store, so the production composer no longer runs on an in-memory journal. | Upstream 194 exposes `Composer.ContinuationCapabilities` with all three pause reasons supported when a file child continuation store, native history and executor are configured (`TestComposerRestoresChildContinuationWithFreshOwnersForEachPauseReason`). Danny now wires that owner and reproduces each pause reason across a fresh client in `TestChildOwnerRestoresEachPauseReasonAfterFreshProcess`. Product availability is still gated by the Milestone 8 live smoke. |
| Team | `team` source ref; source-local sequence and opaque cursor through `teams.WithNativeEventHistory` | Explicit team journal; same bounded memory/file journal semantics. Source facts can be replayed after sequential reopen when file-backed; no source/projection atomicity. The adapter binds a file team journal plus a file continuation store for every run once an owner is bound. | The team continuation contract exposes `Engine.ContinuationCapabilities` with bounded deployment evidence. Danny wires the owner plus an opt-in pause boundary and reproduces `input_required`, `approval_required` and `budget_exhausted` across a fresh client/engine in `TestTeamOwnerRestoresEachPauseReasonAfterFreshProcess`, including duplicate-resume convergence without rerunning members. Product availability is still gated by the live smoke. |
| Workflow | `workflow` source ref; source-local sequence and opaque cursor through `workflow.WithNativeEventHistory` | Explicit workflow journal; same bounded memory/file journal semantics. Source facts can be replayed after sequential reopen when file-backed; no source/projection atomicity. | `Engine.ContinuationCapabilities` advertises file-store cross-process restore under the workflow owner namespace. Pinned workflow continuation tests cover `input_required`, `approval_required`, and `budget_exhausted`, including new-engine restore. This is workflow-specific and same-host/single-writer only. |
| Mailbox | `mailbox.history.v1` source instance; lifecycle sequence and opaque cursor from `Mailbox.HistorySource`/`ReadAfter` | FileStore persists mailbox snapshot and reference-only lifecycle journal atomically. The pinned mailbox tests prove new-process replay, retention-floor/gap errors, and read-only replay. Replay covers mailbox transitions only, not native Agent events or application projection atomicity. | Upstream 198 adds `NewContinuationWorker` handler safe points for the three canonical pause reasons within a same-host single-writer deployment scope; lifecycle-only owners still fail closed. Danny wires a file mailbox owner whose handler, resumer and revalidation callback are supplied by the caller, and reproduces each pause reason across a fresh process in `TestMailboxOwnerRestoresEachPauseReasonAfterFreshProcess` (resumer runs once, the fresh handler never reruns). Product availability is still gated by the live smoke. `acked`, `dead_letter`, and `expired` remain mailbox lifecycle outcomes, not runner completion or product commit. |

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
- `internal/agent/baymax/pinned_upstream_contract_test.go` re-runs the two
  Baymax findings against this pin: explicit owner-store binding, fresh-open
  replay, projection reconciliation idempotency, cross-machine rejection, and
  fail-closed continuation capability matrices.
- Upstream `orchestration/composer/capability.go` and the archived 194/198
  changes now advertise complete three-pause capability matrices for configured
  child, team and mailbox owners. Those are source contracts, not Danny product
  availability; see `docs/upstream/baymax-findings.md`.
- `TestOwnerStoreReplaysTerminalAcrossFreshProcess`,
  `TestOwnerStoreReconcilesProjectionIdempotentlyAfterCrashBeforeProjection`,
  `TestOwnerStoreReportsGapWhenSourceFactsAreUnavailable` and
  `TestOwnerStoreRetentionBoundsReplayAndRejectsExpiredCursor` exercise the
  production adapter's owner-store binding and crash recovery paths.
- `TestChildOwnerRestoresEachPauseReasonAfterFreshProcess`,
  `TestTeamOwnerRestoresEachPauseReasonAfterFreshProcess` and
  `TestMailboxOwnerRestoresEachPauseReasonAfterFreshProcess` cover the three
  canonical pause reasons per execution form through the adapter-built owners.
- `cmd/runtime-probe live-smoke` runs direct, child, parallel team, vote team,
  workflow and mailbox through the authorized adapter against the configured
  endpoint, with per-form `passed`/`failed-contract` evidence. Deterministic
  coverage is complete; there is still no real-provider artifact for this pin
  because no credential is configured.

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

## Pin Refresh (2026-11-11)

`go get github.com/FelixSeptem/baymax@cca95f31f71b` moved this document from
`v0.0.0-20261103091945-d2e08cd6dc8c` to
`v0.0.0-20261110150429-cca95f31f71b` (10 upstream commits). The new public
surfaces are `core/history.OpenOwnerStore` (explicit source binding with a
projection ledger, coordinator and deployment-scope checks), the child/team
continuation capability sources, and `mailbox.NewContinuationWorker` handler
safe points. `go build ./...`, `go test ./...` and the three new pinned fixtures
pass on the new pin.

None of these contracts changes Danny's product availability: the capability
manifest still marks child/team pause restore unsupported, source append and
Danny projection remain non-atomic, and the deployment scope is still
same-host single-writer sequential reopen (or same-host multi-instance CAS with
a coordinator). The two unresolved Baymax findings are narrowed, not closed.
See `docs/upstream/baymax-findings.md`.

The 2026-11-11 refresh also rechecked the live-smoke prerequisites: no
`OPENAI_BASE_URL`, `OPENAI_MODEL`, or `OPENAI_API_KEY` is configured, so no
real OpenAI-compatible endpoint was contacted. The existing
`testdata/runtimeprobe/baymax-live-smoke-unavailable.evidence.json` records
that state for the previous pin.

On 2026-11-12 the live gate ran for real against an OpenAI-compatible gateway
serving `agnes-3.0-flash` over `POST /v1/responses`. All six forms — direct,
child, parallel team, vote team, workflow, mailbox — returned `passed` through
Danny's authorized adapter on this pin:
`testdata/runtimeprobe/baymax-live-smoke.evidence.json`. Two qualifications keep
this from being a clean pass: the gateway requires an explicit `stream` field
(the bundle carries `"provider_compat": "stream-field-injected"`, see
`docs/runtime-probe.md`), and the Stele case in the same bundle is
`skipped-unconfigured`. Live *pause restore* was not exercised either, so the
capability manifest keeps child/team pause restore `unsupported` even though the
running paths are now backed by real provider evidence. Task 7.3 therefore
remains incomplete acceptance on the Stele side.

## Integration Wiring (2026-11-12)

Danny's adapter now consumes the 196/194/198 contracts instead of only
inspecting them:

- `Client.BindOwnerStore` opts into `history.OpenOwnerStore`. Execution uses the
  source-owned file journal; `Reconcile` reopens the store in a fresh owner,
  checks capabilities, replays after a bounded cursor, and reconciles the
  projection ledger per event. Unknown or retention-gapped sources return
  recovery-required instead of re-running effects.
- `Client.BindContinuationOwners` wires durable child, team and mailbox owners.
  Child and team get a file history, a file continuation store and (for child) a
  file scheduler store; mailbox gets a file store plus caller-supplied handler,
  resumer and revalidation callbacks.
- `Client.ContinuationCapabilities` returns a detached per-form,
  per-pause-reason matrix for those owners. It is read-only and does not create
  checkpoints or resume effects.

Both bindings are opt-in. A zero owner root keeps the previous in-process
behavior, so no existing caller silently changes durability class.

What this does **not** change: source append and Danny projection are still not
one transaction. `OpenOwnerStore` reconciliation is replay-and-dedupe at the
product boundary, which is the documented upstream non-goal, not a gap Danny can
close by adding a runtime journal.
