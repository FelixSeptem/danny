## 1. Durable execution storage

- [x] 1.1 Add an additive execution-harness migration for executions, plan versions, notebook entries, checkpoints, pause states, replans, and execution evidence, and verify startup migration succeeds on an empty SQLite database and preserves existing task and compilation tables.
- [x] 1.2 Define driver-neutral execution, plan notebook, checkpoint, pause, replan, and evidence repository interfaces with typed not-found, revision-conflict, idempotency, and unsupported-mode errors, and verify the interfaces compile without importing a concrete database driver.
- [x] 1.3 Implement SQLite repositories with transaction-scoped admission, conditional revision updates, stable effect-key deduplication, and immutable plan history, and verify repository unit tests cover commit, rollback, conflict, replay, and restart reads.
- [x] 1.4 Implement PostgreSQL repositories using the same transaction and conditional-write contract, and verify the shared repository contract suite produces the same statuses, errors, and idempotent results for SQLite and PostgreSQL.
- [x] 1.5 Add deterministic serialization and digest helpers for task profiles, plans, capabilities, projections, checkpoints, and terminal evidence, and verify equal normalized inputs produce equal digests across both storage drivers.

## 2. Execution and plan notebook domain

- [x] 2.1 Define the durable execution state machine, transition guards, pause reasons, terminal outcomes, and typed unsupported execution modes for `direct`, `subagent_fanout`, `team_parallel`, `team_vote`, `workflow`, `mailbox_async`, and `scheduled`, and verify invalid transitions are rejected before persistence.
- [x] 2.2 Extend the plan notebook model with immutable plan versions, parent-plan links, capability and projection references, replan reasons, completed effect references, and bounded diagnostics, and verify a replan retains the complete prior-plan lineage.
- [x] 2.3 Define checkpoint recovery validation for execution identity, plan version and digest, capability digest, projection digest, current step, pause reason, and completed effects, and verify mismatches return typed recovery errors without dispatch.
- [x] 2.4 Define conditional task revision integration that rereads the current task before resume or commit and converts a stale revision into an explicit replan request, and verify a stale worker cannot overwrite a newer task revision.

## 3. Deterministic planning

- [x] 3.1 Implement normalized task-profile classification and deterministic mode selection for direct execution versus bounded subagent fanout, including explicit typed results for unsupported modes, and verify planner fixtures are stable across repeated runs.
- [x] 3.2 Add plan bounds for child count, budget, capability scope, projection scope, and effect-key namespace, and verify generated plans never exceed profile limits or silently widen authority.
- [x] 3.3 Add plan comparison and authority-expansion detection for replans, and verify write-scope, external-side-effect, or budget expansion produces an approval-required pause before dispatch.
- [x] 3.4 Persist the selected plan and initial notebook entry through the admission repository contract, and verify no Baymax call is made when admission validation or the admission transaction fails.

## 4. Durable execution harness

- [x] 4.1 Implement harness admission that atomically records the execution, initial plan version, capability and projection digests, idempotency key, and notebook entry before dispatch, and verify a restart can recover an admitted run that has not yet dispatched.
- [x] 4.2 Implement direct execution through the existing authorized Baymax parent boundary, record parent correlation and terminal evidence, and verify a successful run reaches exactly one idempotent terminal commit.
- [x] 4.3 Implement bounded `subagent_fanout` through the existing Baymax parent and child adapters, enforce the persisted child count and budget, and verify any failed, missing, or unauthorized child blocks the commit gate.
- [x] 4.4 Implement safe-point checkpointing and bounded retry with execution-revision checks, and verify a stale retry returns a typed conflict without duplicating terminal evidence or effects.
- [x] 4.5 Implement pause and resume for `input_required`, `approval_required`, and `budget_exhausted`, and verify a fresh process reconstructs the checkpoint and resumes only after all digest and authority checks pass.
- [x] 4.6 Implement replan after task revision change or an authorized safe-point decision, create a new immutable plan version, retain prior effect references, and verify the prior plan cannot be mutated in place.
- [x] 4.7 Return typed unsupported-mode results without invoking Baymax for `team_parallel`, `team_vote`, `workflow`, `mailbox_async`, and `scheduled`, and verify callers can distinguish unsupported execution from execution failure.
- [x] 4.8 Record uncertain dispatch outcomes as recoverable execution state using the persisted admission idempotency key and correlation fields, and verify a simulated crash before returned-run-id persistence creates a recovery-required state rather than a duplicate dispatch.

## 5. Integration and recovery fixtures

- [x] 5.1 Add fake Baymax parent and child executors that expose deterministic terminal outcomes, child failures, dispatch uncertainty, and correlation identifiers, and verify harness tests do not depend on live Baymax services.
- [x] 5.2 Add SQLite restart fixtures covering admitted, checkpointed, paused, retryable, replanned, and terminal executions, and verify recovery uses durable records rather than logs or in-memory state.
- [x] 5.3 Add PostgreSQL integration fixtures for the same restart, conditional-write, and idempotency scenarios, and verify the driver-neutral contract suite passes against both configured stores.
- [x] 5.4 Add concurrency fixtures for two workers racing on checkpoint, retry, replan, and terminal writes, and verify exactly one expected-revision update commits while all stale writers receive typed conflicts.
- [x] 5.5 Add planner and harness fixtures for unchanged authority, task revision change, authority expansion, child failure, unsupported modes, and replayed effects, and verify each scenario matches the delta specifications.

## 6. Acceptance and documentation

- [x] 6.1 Update the task execution and storage documentation with state transitions, repository boundaries, recovery guarantees, unsupported modes, and SQLite/PostgreSQL configuration, and verify the documentation matches the implemented public contracts.
- [x] 6.2 Run `go test ./...`, the storage-driver contract suite, and `golangci-lint run ./...`, and verify all pass with no new findings in the execution harness packages.
- [x] 6.3 Run `openspec validate task-execution-planner --strict --type change` and `openspec validate --specs --strict`, and verify the change artifacts and main specifications remain structurally valid.
