## 1. Public Contracts and Ownership Gates

- [x] 1.1 Publish `docs/runtime-context-ownership.md` mapping current runtime, context, task, compiler, scheduler, effect, and delivery responsibilities to their owner, public contract, caller, persistence owner, and evidence location; verify every responsibility in the design matrix has an entry and no product authority moves upstream.
- [x] 1.2 Establish the pinned Baymax host/event/control contract fixtures using its real public runner and native fake model; verify admission precedes activation, events and terminal categories come from source evidence, and duplicate admission/cancellation/timeout/causal retry semantics are recorded with the exact pin.
- [x] 1.3 Verify Baymax source retention/replay and checkpoint restoration separately for runner, children, teams, workflow, and mailbox; deliver a matrix covering source identity/order, crash-before-projection recovery, and `input_required`, `approval_required`, and `budget_exhausted` pauses, with executable reproductions or explicit unsupported public-contract evidence.
- [x] 1.4 Establish Stele fixtures against the pinned OpenAPI and real provider/context response; verify response sections/citations, scope/path controls, budget units and byte/reference bounds, authorized diagnostics, runtime/session/turn continuity, and any public snapshot reference rather than assuming current `references`/`role` decoding is correct.
- [x] 1.5 Add a versioned dependency capability/evidence manifest distinguishing verified, unsupported, and blocked capabilities; verify every form and required context/recovery guarantee references its public API, exact dependency pin, and evidence from tasks 1.2-1.4.
- [x] 1.6 Apply the upstream gate to confirmed gaps from tasks 1.2-1.4: add Chinese findings with reproduction, affected numbered tasks, and unblock conditions in the appropriate resolved/unresolved document, or record that no gap was reproduced; verify dependent tasks remain unchecked and `blocked-upstream` until the original reproduction and impacted checks pass a reviewable repaired pin, with no production workaround.

## 2. Versioned Product Boundaries

- [x] 2.1 Add Danny-owned versioned execution request, event, outcome, control, checkpoint, and capability DTOs behind narrow adapter interfaces; verify round-trip fixtures preserve plan, revision, authority, effect, limits, native identity, and correlation, and reject unknown versions or missing required authority before dispatch.
- [x] 2.2 Add the context request/envelope with explicit budget units, scope/session/turn, role/policy constraints, provenance, selection evidence, and application digest semantics; verify canonicalization and validation fixtures distinguish omitted diagnostics, empty selections, source digests, and Danny projection digests.
- [x] 2.3 Update deterministic planner selection to use the verified capability manifest for direct, children, teams, workflow, and mailbox, retaining scheduled as occurrence-aware admission; verify equivalent profile/version/manifest inputs yield equal plans and unavailable requested forms fail before model/tool execution without mode substitution.

## 3. Durable Observation and Authority Repositories

- [x] 3.1 Add forward migrations for native evidence identity, application observation positions, source cursor checkpoints, and runtime/context authority bindings; verify fresh and migrated SQLite/PostgreSQL databases retain existing execution data with explicit legacy provenance and reject unsupported contract versions.
- [x] 3.2 Implement conditional native-event projection persistence and transactional source/application cursor advancement; verify the shared repository suite covers overlap deduplication, conflicting payload identities, concurrent writers, source order where defined, and crash-before/after-commit parity on both drivers.
- [x] 3.3 Implement bounded execution/scope-bound read-after-cursor pages and retention-floor validation; verify restart replay, stable pagination, and typed malformed/foreign/expired/gap errors without invoking execution, resume, retry, or tools.
- [x] 3.4 Persist native checkpoint authority bindings and application pause transitions atomically with expected revisions; verify both drivers preserve plan/capability/context/write/effect authority and reject stale workers without leaving a partial pause transition.
- [x] 3.5 Persist access-controlled bounded admitted-context snapshots or verified public snapshot references and their canonical digest; verify a fresh process reconstructs the same authorized projection, foreign scope cannot read it, and changed context cannot reuse the prior digest.

## 4. Native Execution Integration

Tasks in this group require their mode-specific public evidence from section 1 and the relevant contracts/repositories in sections 2-3. Native final context/handoff wiring in section 6 is required before any production mode is marked fully available.

- [x] 4.1 Replace authorized Run/Stream's nil handler and synthesized events with native host admission/activation and event projection; verify source parent/tool/diagnostic/terminal facts, Run/Stream parity, and abandoned admission release using native fake-model fixtures.
- [x] 4.2 Wire policy-authorized tools into the native execution boundary while retaining server-side capability, write scope, approval, revision, and effect checks on every mutation; verify instruction-like source/memory cannot invoke an undeclared tool or expand authority.
- [x] 4.3 Replace production child calls into runtimeprobe and the harness's child dispatch loop with native bounded child orchestration of actual authorized assignments; verify child IDs, role/projection constraints, per-child limits, cancellation, result aggregation, and failed-required-child commit blocking.
- [x] 4.4 Wire native parallel teams with execution-derived team/member identities and authorized member execution; verify concurrent member evidence, inherited authority, native aggregate semantics, limits, and member failure without fixed probe callbacks.
- [x] 4.5 Wire native vote teams using member evidence rather than unconditional approve votes; verify conflicting decisions, native tie-break policy, bounded participation, provenance, and rejection keeping compilation uncommitted.
- [x] 4.6 Wire native workflow execution from versioned dependent assignments; verify native step correlation, dependency order, step failure, resource limits, and checkpoint compatibility without a Danny workflow engine.
- [x] 4.7 Wire mailbox continuation and scheduled callers through the common execution boundary while retaining existing Baymax scheduler/mailbox APIs and Danny outboxes; verify stable message/occurrence identity, lease-loss reconciliation, retry/DLQ evidence, and one logical admission with the original authority.
- [x] 4.8 Migrate compilation fanout/aggregation/vote to the verified execution boundary and remove production probe routing, fixed team IDs, and fabricated completions; verify real section findings/citations, failed-child rejection, and unchanged authoritative generation commit/outbox contracts.

## 5. Native Recovery and Product Harness

Recovery tasks depend on verified form/pause-specific source contracts from task 1.3; an action-resume DTO alone does not satisfy arbitrary pause recovery.

- [x] 5.1 Refactor the task harness to own admission, current revision, approval, replan, and conditional product commit while delegating runtime mechanics; verify a native success followed by a stale task revision preserves runtime evidence and returns a product conflict without repeating execution.
- [x] 5.2 Bind each supported native safe point to a durable authority checkpoint and resume through its public restore API; verify fresh-process resume preserves native checkpoint integrity, plan/capability/projection digests, write scope, occurrence namespace, and completed effects for all claimed pause paths.
- [x] 5.3 Route cancellation, timeout, and authorized retry through verified native controls; verify request acceptance remains separate from terminal completion, retry identity follows source causation, earlier terminal evidence is immutable, and logical effect keys remain stable.
- [x] 5.4 Reconcile dispatch, native checkpoint creation, application binding, and event projection crash windows; verify recovery finds actual source facts or returns typed recovery-required/gap errors without rerunning uncertain parent/child effects or inventing a checkpoint.
- [x] 5.5 Preserve explicit task/context replan and authority expansion approval; verify changed revision/context creates a new notebook/projection binding with completed-effect references, expanded authority remains paused, and legacy application-only checkpoints cannot resume from the beginning.

## 6. Stele Selection and Baymax Assembly

This group requires the Stele public-contract gates from task 1.4. It can progress independently of unrelated execution-form blockers; native assembly/handoff blockers still prevent marking affected forms available.

- [x] 6.1 Decode the real Stele provider/context response into the versioned context envelope, removing unsupported role-field reliance; verify schema-based fixtures and the pinned service preserve memory citations/provenance and distinguish malformed responses from a valid empty result.
- [x] 6.2 Map server-owned scope, memory path, session/turn, role disclosure, selection options, and budgets only through verified public controls; verify foreign scope/path, undocumented controls, incorrect budget units, missing provenance, and oversized responses fail before model execution without local reranking or compaction repairs.
- [x] 6.3 Wire documented Stele session/turn continuity and admitted projection persistence; verify restart preserves binding/correlation and projection digest, while context refresh creates a new authorized projection instead of assuming context-read idempotency means immutable selection.
- [x] 6.4 Keep Danny's projection builder focused on role disclosure, trust, and product references and delegate final history pressure/token accounting and handoff to Baymax; verify coordinator and all supported child roles preserve capabilities and citations through native assembly and remove the local generic pruning/handoff emulation.
- [x] 6.5 Verify memory effects and compile/scheduled Stele projection delivery through the documented exact-scope idempotency contract; verify replay after an uncertain response/service restart returns one original logical result, conflicting payloads are typed, and delivery failure does not replay Agent execution or invalidate a committed knowledge generation.

## 7. Integrated Evidence and Quality Gate

Status note (2026-11-12): dependencies are refreshed to Baymax
`v0.0.0-20261110150429-cca95f31f71b` and Stele
`v0.0.0-20261111135338-19fc90134492`. Pinned public-contract fixtures,
`go build ./...`, `go test ./... -count=1` and the strict OpenSpec validation
pass on both new pins.

The adapter now consumes the 196/194/198 contracts instead of only inspecting
them. `BindOwnerStore` binds `history.OpenOwnerStore` for execution and for
`Reconcile` (bounded replay, idempotent projection ledger, retention/gap
recovery). `BindContinuationOwners` wires durable child, team and mailbox
owners, and `ContinuationCapabilities` exposes a read-only per-form,
per-pause-reason matrix. `owner_store_test.go` and `continuation_restore_test.go`
reproduce each condition on the production adapter, including
`input_required`/`approval_required`/`budget_exhausted` restore in a fresh
process for all three forms. The first Baymax finding is therefore resolved;
the second is narrowed to the missing live gate, not to missing Danny wiring.
The capability manifest still marks child/team pause restore `unsupported`
because promoting it would need the real integration path.

Task 7.3 is complete as of 2026-11-12, in two halves. The Baymax half ran for
real against an OpenAI-compatible gateway (`agnes-3.0-flash`,
`POST /v1/responses`) with all six forms passing through the authorized adapter
(`testdata/runtimeprobe/baymax-live-smoke.evidence.json`); it is not a clean
pass — the gateway needs an explicit `stream` field, so the bundle records
`"provider_compat": "stream-field-injected"`. The Stele half ran against a real
pinned service: a fresh PostgreSQL 18 + pgvector 0.8.6 compose stack served the
`19fc901` pin from image `danny-stele-verify:local`, and all six capabilities
(context continuity, memory retrieve, references, compilation replay, restart,
idempotent replay) passed
(`testdata/runtimeprobe/stele-19fc901-danny-smoke.evidence.json`). Live *pause
restore* was not exercised in either half, so the capability manifest keeps
child/team pause restore `unsupported`; tasks 8.2 and 8.3 stay open because the
Milestone 9 handoff still needs the complete exit gate.

The live harness is no longer direct-only. `cmd/runtime-probe live-smoke` now
runs `direct`, `child`, `team_parallel`, `team_vote`, `workflow` and `mailbox`
through Danny's authorized adapter against the configured endpoint, with
`DANNY_LIVE_FORMS` to re-run a subset. A failing form records
`failed-contract`, keeps the evidence of the forms that passed, and exits
non-zero. The Stele service half is now covered by the same harness family:
`go run ./cmd/runtime-probe stele-smoke` against the pinned service produced
`testdata/runtimeprobe/stele-19fc901-danny-smoke.evidence.json`.

`DANNY_LIVE_PROVIDER_SHIM=stream-default` adds a loopback proxy that fills in
the spec-default `"stream": false` for gateways that mis-handle an omitted flag;
the bundle then records `"provider_compat": "stream-field-injected"` so the
adaptation is visible instead of looking like conformance.

- [x] 7.1 Extend deterministic contracts for all six execution forms, native tools/events, planning, context trust/budgets, product revision conflicts, and Run/Stream aggregation/terminal parity; verify each new or modified spec scenario is covered by an assertion or explicit pinned contract evidence.
- [x] 7.2 Run restart/crash/concurrency acceptance on SQLite and PostgreSQL for event/cursor transactions, native recovery bindings, context snapshots, scheduled effects, generation commits, and pending outboxes; verify shared observable results and no duplicate logical mutations or fabricated replay history.
- [x] 7.3 Capture real OpenAI-compatible smoke evidence for applicable direct/child/team/workflow/mailbox paths and a real pinned Stele service for scoped context, provenance, session/turn continuity, and restart-idempotent memory; verify every availability claim has real correlation/pin/evidence and unavailable credentials or services are recorded as incomplete acceptance.
- [x] 7.4 Run Go formatting/static checks, `go test ./...`, the applicable race/integration checks, `golangci-lint run ./...` using `.golangci.yml`, and existing provider/documentation checks; verify all required checks pass and new imports remain within public adapter boundaries.

## 8. Documentation and Milestone Handoff

- [x] 8.1 Update harness, runtime probe, context, recovery, and configuration documentation with the ownership matrix, source/product state distinction, cursor/retention semantics, legacy handling, dependency pins, and evidence references; verify examples describe implemented public contracts and findings remain Chinese with resolved/unresolved sections.
- [ ] 8.2 Update the roadmap Milestone 8 status and provide the protocol-neutral handoff for Milestone 9 only after the complete exit gate passes; verify all required mode/context/recovery evidence exists, no required upstream task remains blocked, and no AG-UI/A2UI/frontend dependency was introduced by this change.
- [ ] 8.3 Run `openspec validate runtime-context-ownership-boundary --strict` and `git diff --check`, review the complete task/evidence matrix, and confirm proposal/spec/design/task consistency; verify all implementation checkboxes are backed by acceptance evidence before requesting archive or integration.
