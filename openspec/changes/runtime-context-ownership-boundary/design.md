## Context

See `proposal.md` for motivation and the three delta specifications for observable contracts. This design implements roadmap Milestone 8 before the UI protocol boundary.

### Observed implementation

- `internal/agent/baymax/adapter.go` passes a nil event handler to authorized Run/Stream and builds parent/terminal events after execution. `KnowledgeExecutor.RunChild` calls a probe path whose local runner returns a fixed completion value with a one-second budget.
- `internal/agent/baymax/compile.go` also invokes probe child/context paths. Its team callbacks aggregate identifiers and unconditionally vote approve, with fixed `compile` run/team identifiers. These calls exercise public packages but do not establish production analysis, voting, or event evidence.
- `internal/tasks/harness.go` owns application execution transitions, a sequential child loop, and application-only checkpoints. Resume validates local digests and calls Execute again. The repository already provides conditional execution writes, plans, effects, and evidence.
- `internal/context/assembly/projection.go` correctly performs role disclosure and source trust marking, but also performs local pressure pruning and builds a local `handoff.v1` structure. These generic assembly operations overlap Baymax.
- `internal/context/stele/adapter.go` posts an undocumented `role` input, decodes `references` and `content`, and hashes only copied references. The pinned OpenAPI `ContextAssembleRequest` declares query, budget, path/path-prefix and selection options, not role. Its context response declares memory sections, citations, and optional diagnostics. DTO presence or permissive JSON decoding does not establish role enforcement, a context watermark, or repeatable projection reads.
- The inspected Stele retrieval implementation uses `Budget` as an item selection limit and has a separate `CharacterBudget`; the provider clamps against a configured limit named `MaxContextBytes`. The public HTTP budget unit and serialized-size guarantee therefore require explicit contract evidence. Danny's current 4096-byte projection limit cannot be called a Stele token budget.
- `internal/agent/baymax/scheduled.go` already uses native scheduler/mailbox operations. Application occurrence and dispatch/delivery outboxes close product cross-store gaps. Preserve these responsibilities.

### Public dependency surfaces inspected

| Dependency pin | Public surface | What the planning inspection establishes |
| --- | --- | --- |
| Baymax `v0.0.0-20261110150429-cca95f31f71b` | `core/types/host_contract.go`, `core/runner/control.go` | Embedded host protocol, source admission/activation separation, cancellation, timeout and causal retry exist; Danny fixtures cover admission/control semantics. |
| Same Baymax pin | `core/types/protocol.go`, native execution/history contracts | Native run/step/event/checkpoint references, source-owned event identity and terminal projection exist. Generic callback order remains distinct from retained source sequence. |
| Same Baymax pin | `core/history/deployment.go`, runner/composer/team/workflow/mailbox history APIs | `core/history.OpenOwnerStore` binds an explicit source-owned journal, optional projection ledger, coordinator and deployment scope; unbound and cross-machine requests fail closed. Durability remains the same-host single-writer sequential reopen profile (or same-host multi-instance CAS with a coordinator). There is no cross-machine coordination and no atomic transaction between a source append and a Danny projection. |
| Same Baymax pin | `core/types/continuation_capabilities.go`, composer/teams/mailbox continuation owners | Upstream changes 194/196/198 publish per-pause-reason capability evidence for child, team and mailbox handler continuations; `input_required`, `approval_required` and `budget_exhausted` are declared separately and unconfigured owners fail closed. Danny has not wired or revalidated these owners, so child/team/mailbox pause restore stays unsupported in the capability manifest. |
| Stele `v0.0.0-20261111135338-19fc90134492` | `openapi/spec.go`, provider runtime/context/memory endpoints | Scope binding, context sections/citations and memory operations exist. `STELE-006` and `STELE-007` are repaired; Stele assurance and Danny's 2026-10-26 fresh PostgreSQL/pgvector smoke pass context, task/run references, compilation, restart, and idempotency **on the historical `f38016e` pin**. The current pin adds `corrections` to `ProviderCapabilityDocument.required`, governed memory intents and versioned context projection specs; `exact_snapshot_replay` remains `false`. Omitted event source metadata defaults to untrusted/restricted and is excluded from public retrieve/context. Role semantics, exact budget units, snapshot continuity, and selection diagnostics are enforced by the adapter and evidence fixtures; final model handoff remains Baymax-owned. |

These are inspected surfaces, not a claim that this milestone already passes live or restart verification. `docs/upstream/baymax-findings.md` retains two unresolved entries (owner-injected source history and path-specific continuation). The 2026-11-11 pin refresh narrows but does not close them: Danny has not wired the new owners, and upstream still excludes cross-machine/multi-writer coordination and cross-store atomicity. `docs/upstream/stele-findings.md` has no unresolved defect, but its live-service evidence is bound to the previous pin and must be repeated. Record a new finding only after a focused reproduction distinguishes a Danny adapter defect from an upstream defect or missing required public guarantee.

## Goals / Non-Goals

**Goals:**

- Translate immutable authorized product plans into native runtime assignments without implementing execution loops in product modules.
- Establish one native execution truth and a separate durable product observation/commit model.
- Compose Stele memory selection, Danny disclosure/trust checks, and Baymax final model assembly with explicit versions, budget units, and provenance.
- Supply protocol-neutral replay and recovery contracts for later browser adapters.
- Make mode-specific prerequisites and upstream blockers reviewable before production wiring.

**Non-Goals:**

- AG-UI, A2UI, MCP UI, SSE, browser workbench, or new frontend dependencies.
- Moving tasks, generation authority, schedules, business effect keys, app outboxes, or source-trust decisions upstream.
- A distributed transaction across Danny, Baymax and Stele, or an exactly-once claim for arbitrary model execution/external effects.
- Implementing missing runtime/context algorithms locally or expanding the parser feature set.

## Decisions

### 1. Separate source mechanics from product authority

Publish `docs/runtime-context-ownership.md` during implementation with each current responsibility, its owner, public API, caller, persistence owner, and evidence reference. The initial matrix is:

| Responsibility | Owner | Danny's integration duty |
| --- | --- | --- |
| Model Run/Stream, registered-tool invocation | Baymax | Supply authorized policy tools; every mutation callback checks product authority. |
| Child dispatch/budget, team strategies, workflow steps | Baymax | Project assignments and limits; validate product findings/citations and commits. |
| Mailbox lease/retry/ack/DLQ, delayed runtime dispatch | Baymax | Preserve occurrence identity and reconcile app dispatch intent. |
| Runtime safe points, checkpoint schema/restore, cancellation/timeout/retry | Baymax | Approve controls and bind runtime references to durable authority. |
| Native event identities, source order, terminal arbitration | Baymax | Persist sanitized projections and source positions without changing semantics. |
| Memory ingestion/idempotency/path/provenance | Stele | Provide stable request/effect identity and app correlation through public calls. |
| Memory retrieval/ranking/selection and memory context budget | Stele | Request authorized scope/paths/options; reject invalid disclosure or bounds. |
| Memory sessions/turns and context continuity | Stele | Bind application conversation/run to verified public continuity references. |
| Final model context, history compaction and native handoff | Baymax | Pass approved Stele context and product references with trust labels. |
| Plan selection, capability/write policy, revision and approval | Danny | Retain server-side decisions and commit checks. |
| Generations/source bytes/citations/tasks/schedules | Danny | Retain database authority and conditional mutations. |
| Product observation log, replay cursor and control/commit intents | Danny | Persist observations and durable intents; never manufacture native facts. |
| Effect ledger, Stele projection/webhook/schedule outboxes | Danny | Retain cross-store idempotency and independent delivery status. |

Alternative considered: moving the entire task harness upstream would also move product revisions and commit authority. Keeping the existing harness execution loop would duplicate runtime responsibility. Use a product authority harness that delegates mechanics instead.

### 2. Versioned application contracts wrap public upstream contracts

Add narrow adapter interfaces and Danny-owned DTOs with explicit v1 schema identifiers. Domain modules import those interfaces; only dependency adapter packages import Baymax public packages or decode Stele HTTP wire structures. Probe fixtures remain independently callable and cannot provide production execution results.

The execution request contains execution/task identity, task revision, conversation/session, run/parent identity, correlation/causation, immutable plan version/digest, verified capability-set digest, tool capability digest, projection reference/digest, write scope, effect namespace, assignments, and limits. Scheduled requests add schedule revision, occurrence ID/key, and admitted policy. Missing required authority is rejected before source admission or model execution.

Native host admission precedes activation where the public source supports it. Release abandoned admissions. The product plan must be committed before activation. Persist the application dispatch intent and source identifiers so a crash between stores is reconciled against actual source admission/checkpoint/terminal evidence. Native in-memory duplicate admission alone is insufficient for restart safety.

Maintain a versioned capability manifest, with each form marked available, unsupported, or blocked and linked to evidence. Planner determinism includes the manifest digest. Implement direct, children, parallel/vote teams, workflow, and mailbox continuation through their public owners; native orchestration callbacks execute real authorized member assignments. Deterministic fake models are valid fixtures, but fixed probe completions or unconditional production votes are not.

Scheduled is an admission/trigger wrapper over a verified execution form. Preserve the existing public scheduled API and occurrence fields; do not invent a second runtime. Team/workflow identities derive from the product execution and assignment, never the fixed string `compile`.

Alternative considered: exposing upstream types everywhere would couple business and future browser models to source wire changes. A local replacement runtime would violate ownership. A translating adapter permits version upgrades while preserving verified semantics.

### 3. Record real source facts with two distinct cursor domains

Install event handlers and consume source-owned orchestration outcomes. Normalize them into a versioned envelope with native event/source identity, native run/step/parent/cause, native sequence when defined, event kind, sanitized payload, and Danny execution/plan/authority correlation. Keep Danny admission, approval, revision conflict, and commit observations explicitly marked as product events. Source operation records can be projected with their record identity; a product observer must not invent a native event identity absent a source fact.

Persist observations using a driver-neutral repository. The unique key includes source identity and its documented event identity namespace. Allocate a monotonic application observation sequence transactionally per product execution; this is replay order, not an assertion of global native order across concurrent child runs. Preserve per-source order/causation and reject sequence drift where the source defines a sequence. Same identity plus different normalized payload is an evidence conflict.

An application cursor binds schema version, product execution/scope, and durable observation position. A native cursor remains opaque and is stored separately. Use bounded read-after-cursor pages with a configured retention floor; invalid, foreign, expired, or missing-history positions return typed errors. Subsequent Milestone 9 transports can encode this contract without becoming its source of truth.

Atomically store a projection and its consumed source position where available. Resume consumption through a verified native replay surface or a source store extension point. The inspected realtime binding DTOs are not a generic all-mode durable journal: prove durable storage, replay completeness, terminal recovery and checkpoint correlation for each claimed path. A crash before projection commit must be replayable from actual retained source evidence; if the public source cannot support this requirement, record a blocker. Hosting an upstream-defined store interface is allowed integration only when the interface and native ownership semantics are verified; adding an application replacement runtime history algorithm is prohibited.

Keep native terminal evidence, product commit outcome, and observer/delivery status separate. Failed persistence returns a projection/recovery error and cannot invent a model failure. Cancellation acceptance is not terminal completion. Reconnect is a read path and cannot call execution retry or resume.

Alternative considered: directly using upstream realtime sequence as the product cursor fails for other sources and parallel orchestration. Assigning local sequences and calling them native order misrepresents evidence. Store both domains explicitly.

### 4. Resume binds native recovery to durable authorization

Replace application resume-from-the-beginning with validation plus the appropriate native checkpoint/restore contract. An authority checkpoint stores the native checkpoint ID/component/schema/digest, source position, context reference/digest, product plan/capability/version, task/schedule revision, write scope, effect namespace, completed effects, and pause reason. Commit its application binding and pause transition conditionally in one database transaction.

Native checkpoint durability precedes a usable application binding. A crash after source checkpoint creation leaves an unbound native record to reconcile; a missing/damaged reference leaves the app execution recovery-required. Do not claim a transaction spanning stores. An uncertain dispatch is reconciled to existing source state or blocked; it cannot automatically become a new parent run.

Resume validates current task revision and authority, retained context digest, source checkpoint integrity, and completed effects before asking Baymax to restore. Changed task/context requires an explicit refresh/replan record; expanded authority requires approval. A retry follows native causal attempt/run identity rules and preserves the logical effect namespace, so it cannot duplicate completed product effects. Each mutable tool checks the effect ledger and revision boundary even after runtime restoration.

Native control/checkpoint support can differ by runner, composer, workflow and mailbox path. Prove the path appropriate to `input_required`, `approval_required`, and `budget_exhausted`; do not assume that an action-resume checkpoint API resumes arbitrary pauses. Any missing required public restore/safe-point capability blocks only the dependent tasks and prevents the full milestone exit claim.

Alternative considered: persisting only Danny fields cannot reconstruct native runtime state. Treating every restart as a new run repeats uncertain effects and loses safe-point semantics. Persist a binding to the source-owned recovery record.

### 5. Stele selects memory; Danny authorizes it; Baymax assembles the model context

Define a context v1 request/envelope independently of Stele internals. Its request carries a server-derived scope and binding, conversation/session/turn/run correlation, query, authorized memory path selection, role/policy constraints, memory selection budget with verified units, maximum serialized response bytes/reference count, and requested supported diagnostics. Role/policy is an application constraint; translate it only into documented supported scope/path/options. Never rely on the current arbitrary `role` JSON field.

Decode the actual documented provider/context response and preserve bounded memory references, citation/provenance identities, selection diagnostics available to the caller, source schema/version, continuity references, and explicit units. An omitted diagnostic differs from an empty selection. Do not reconstruct hidden provenance or ranking explanations from internal packages. A required watermark unavailable on a query endpoint must use a verified public snapshot/projection reference or the application snapshot mechanism below, not a fabricated upstream watermark.

Apply Danny role/tool disclosure and source trust checks before final assembly. Retain product-generation/task reference meaning in Danny. Scope/path verification and redaction are authorization, not replacement memory ranking. Bound HTTP decoding, reference fields and metadata; reject invalid/oversized results rather than pruning/reranking a broken upstream result locally. If required public controls cannot produce a usable bounded projection, block the integration.

The application projection digest hashes a canonical, authorized envelope including scope/session/turn, role/policy constraints, references/provenance, selected bounded data, budget units, and source evidence. Label it Danny-owned; do not call the existing local hash an upstream digest. Persist the admitted envelope as a bounded, access-controlled application projection snapshot when necessary for restart equality. This is the authorized run input, not a second memory database or ranker. Refreshing memory creates a new projection/digest with a recorded refresh/replan decision.

Send this approved envelope through Baymax's public context/reference and handoff contracts. Keep the existing projection builder as an authorization/trust validator and envelope serializer; remove local generic pressure pruning/handoff emulation once the corresponding native contracts are proven. Baymax owns token accounting and final model-history pressure management. Stele item/character/byte budgets and Baymax token budgets are separate typed quantities with no guessed conversion.

Use documented provider runtime/session/turn and memory-write contracts for continuity. A context read is not assumed immutable because it accepts an idempotency field. Memory writes reuse existing stable effect/outbox keys and preserve exact-scope conflict semantics across restart; context continuity records must not grant product authority or change task/generation commits.

Alternative considered: copying Stele ranking/selection into Danny prevents this project from validating upstream usefulness. Sending unsupported hints creates false guarantees. Map supported contracts and block guarantees that require an upstream change.

### 6. Evidence gates precede feature claims

Create focused public-contract fixtures and pinned evidence for every execution form, source cursor/store behavior, recovery pause/control path, and Stele response/continuity guarantee. Reuse native fake models for deterministic execution and run real OpenAI-compatible smoke scenarios plus a real pinned Stele service where applicable. Exercise both SQLite and PostgreSQL, including crash windows, conflicts, replay, and same-key effects.

A confirmed missing/incorrect upstream guarantee produces a Chinese finding in `docs/upstream/baymax-findings.md` or `stele-findings.md`, including the pin, public contract, reproduction, actual/expected results, evidence path, affected numbered tasks, and unblock condition. Keep resolved/unresolved sections. Mark dependent tasks `blocked-upstream` without checking them off; independent tasks can continue. Do not mark Milestone 8 complete or claim readiness for its dependent UI contracts while required blockers remain.

The acceptance boundary includes formatting/static checks, provider-boundary checks, relevant deterministic and live evidence, both database drivers, and `golangci-lint run ./...` under `.golangci.yml`. Missing credentials/service evidence is recorded as unavailable, never treated as passing. Test and smoke commands are implementation acceptance tasks; this proposal does not execute them.

## Risks / Trade-offs

- [Pure event binding projections may not expose durable all-mode replay] -> Prove storage/replay through the actual native public source; block affected replay tasks when absent.
- [Native checkpoint coverage differs across forms and pause reasons] -> Keep path-specific evidence and reject unsupported restore before tool dispatch.
- [Stele public budget and selection metadata can diverge from internal behavior] -> Pin exact HTTP schema/service evidence, label units, and reject undocumented guarantees.
- [Native success and product rejection create two visible outcomes] -> Preserve both with typed ownership rather than overwrite terminal runtime truth.
- [Retained context contains private memory] -> Use scoped access, bounded snapshots, sanitized observation payloads, and explicit retention; never expose raw snapshots as event payloads.
- [Added event persistence increases write volume] -> Bounded pages/batches and explicit retention; apply backpressure as an observer/source contract, never silently drop required evidence.
- [Legacy evidence cannot satisfy new recovery guarantees] -> Preserve readability and legacy provenance; require explicit recovery/replan for interrupted legacy runs.
- [Integration defects can be mistaken for upstream issues] -> Reproduce at the public boundary before filing an upstream blocker and fix Danny decoding/mapping errors locally.

## Migration Plan

1. Add versioned interfaces, ownership/evidence documents, public-contract fixtures, and capability manifests. Resolve confirmed upstream blockers before wiring dependent production paths.
2. Add forward-only migrations and driver-neutral event/cursor/checkpoint/context binding repositories. Keep old records readable and explicitly marked legacy; never backfill fabricated native facts.
3. Wire authorized native Run/Stream and tool events, then children, teams, workflows and mailbox continuation as their gates pass. Migrate compiler and scheduled callers to the common boundary and remove production calls into probe helpers.
4. Introduce verified native safe-point bindings and controls. Treat interrupted legacy executions as recovery-required; do not resume them through the old from-start path.
5. Migrate Stele decoding/selection evidence and Danny disclosure into the context envelope, persist admitted snapshots/references, and wire Baymax native final assembly/handoff.
6. Exercise repository migration/restart contracts on both drivers and deterministic/live integration evidence, run lint and documentation/provider checks, and update the roadmap only when its complete exit gate passes.

Rollback stops admission and workers before reverting an application binary. Retain the database and additive schemas. New executions/bindings must not be consumed by a binary lacking their contract version; unsupported versions fail startup/admission or remain recovery-required. Do not enable a legacy synthetic/probe execution path as fallback, delete evidence, or automatically restart uncertain work. Preserve pending outboxes and effect keys for forward recovery.
