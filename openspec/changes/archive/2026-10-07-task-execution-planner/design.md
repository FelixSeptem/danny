## Context

The project already has driver-neutral task and effect repositories, optimistic task revisions, a KnowledgeAgent with deterministic `direct` and `subagent_fanout` planning primitives, Baymax parent/child adapters, and an in-memory checkpoint store. The missing boundary is durable coordination: a process restart must be able to reconstruct an admitted plan, a safe-point checkpoint, completed effects, and the authorized context projection without treating logs or Baymax state as the application source of truth.

This change adds that coordination layer without moving execution mechanics into Danny. SQLite and PostgreSQL remain the durable source of application execution state; Baymax remains the owner of parent/child execution and terminal semantics; Stele remains the owner of context continuity.

## Goals / Non-Goals

**Goals:**

- Persist task executions, plan versions, checkpoints, pause states, replans, and execution evidence through a shared storage contract.
- Make admission, direct execution, bounded subagent fanout, retry, pause, resume, replan, and commit transitions explicit and recoverable.
- Preserve plan, capability, projection, correlation, revision, and effect-key constraints across restart and replan.
- Keep unsupported Baymax shapes explicit until their roadmap milestones provide the required mailbox, workflow, or scheduler contracts.
- Provide deterministic fake-executor and SQLite/PostgreSQL contract fixtures for the harness.

**Non-Goals:**

- Implementing scheduler, cron, DST, misfire, mailbox workers, webhook delivery, HTTP endpoints, or Web UI.
- Implementing a second task state machine inside the plan notebook.
- Reimplementing Baymax runner, subagent, budget, team, workflow, terminal, or Context Assembler semantics.
- Adding a local context or reference store alongside Stele.

## Decisions

### 1. Use an execution state machine with append-only plan history

The execution record owns the current status and revision. Plan notebook entries and checkpoint records retain immutable versions and transition evidence. Conditional updates require the current execution revision, so a stale recovery worker cannot overwrite a newer pause, retry, or terminal result. A single mutable JSON blob was rejected because it would make conflict detection and audit of replan decisions ambiguous.

### 2. Commit admission and the initial plan atomically

Admission writes the execution row, plan version, capability/projection references, and initial notebook entry in one database transaction before invoking Baymax. This prevents a parent run from existing without an application-owned plan. The Baymax run ID is correlated after dispatch; a dispatch failure leaves a durable retryable execution state rather than an invisible run.

### 3. Keep checkpoints reference-first

Checkpoints store plan and projection digests, step identity, pause reason, correlation IDs, completed effect keys, and bounded diagnostics. They do not copy Stele context, source documents, provider payloads, or full Agent prompts. Resume reconstructs the authorized projection through the existing adapters and rejects digest mismatches before execution.

### 4. Replan by version, never by mutation

A replan creates a new plan version linked to the prior version and records the changed profile digest, rationale, and authority comparison. Completed effect keys remain reusable. If the new plan expands write scope, external side effects, or budget, the harness enters an approval pause before dispatch. Mutating the existing plan in place was rejected because it would make recovery unable to distinguish the authority used by completed work.

### 5. Use the existing Baymax adapter boundary

The harness calls the existing authorized parent and child methods and records the returned terminal outcomes. It supplies the persisted plan, capability, projection, and correlation fields but does not aggregate events or enforce Baymax child semantics itself. The first implementation supports only `direct` and bounded `subagent_fanout`; unsupported modes return typed results so later milestones can add their native Baymax boundaries without changing the planner contract.

### 6. Share repository behavior across SQLite and PostgreSQL

Execution, plan, checkpoint, evidence, and replan repositories use the existing bind, migration, transaction, lease, revision, and effect-key patterns. SQLite remains the default local store, while PostgreSQL runs the same contract suite. A new schema migration is additive and startup-safe; rollback is disabling harness admission while preserving readable execution records.

## Risks / Trade-offs

- [Risk] A crash after Baymax dispatch but before the application records the returned run ID can create an uncertain external run. → Mitigation: persist an admission idempotency key and dispatch correlation before the call, then reconcile the same key and record an explicit recovery-required pause when the outcome cannot be proven.
- [Risk] A child may finish after the parent lease is lost. → Mitigation: require execution revision and plan/capability digests on terminal writes; stale workers receive a conflict and cannot commit a result.
- [Risk] Replanning can make previously completed work semantically stale. → Mitigation: retain prior plan and effect references, require a changed profile digest, and reuse only effects whose keys and authority remain valid.
- [Risk] In-memory checkpoint behavior may differ from durable recovery. → Mitigation: make the durable repository the production boundary and run restart fixtures against both storage drivers; keep memory stores only for unit tests.

## Migration Plan

1. Add the execution schema migration and driver-neutral repositories without changing existing task or compilation readers.
2. Add plan notebook, checkpoint, evidence, and replan domain contracts with deterministic serialization and revision checks.
3. Implement harness admission and direct execution, then bounded subagent fanout through the existing Baymax adapter.
4. Add restart, pause/resume, task revision conflict, retry, and authority-expansion fixtures for SQLite and PostgreSQL.
5. Enable task harness admission behind the existing application boundary; existing task repositories remain usable if harness admission is disabled.

## Open Questions

None. Scheduler, mailbox, workflow, and Web API decisions are intentionally deferred to their roadmap proposals.
