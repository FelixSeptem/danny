Set-PSReadLineOption: C:\Users\whenh\.powershell\profile.ps1:72
Line |
  72 |  … yAndPlugin } catch { Set-PSReadLineOption -PredictionSource History }
     |                         ~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
     | The predictive suggestion feature cannot be enabled because the console output doesn't support virtual terminal
     | processing or it's redirected.
# storage-durability Specification

## Purpose

This capability provides the durable application state and recovery contracts required by knowledge compilation, task execution, scheduling, context projection, and notification delivery across local SQLite and PostgreSQL deployments.

## Requirements

### Requirement: Shared repository contract across storage drivers

The system SHALL expose driver-neutral repositories for conversations, tasks, schedules, executions, notifications, source metadata, knowledge generations, artifacts, manifests, revisions, idempotency effects, leases, and outbox records. SQLite and PostgreSQL implementations SHALL provide the same observable behavior, error categories, ordering guarantees, and transaction boundaries for the shared contract.

#### Scenario: Repository contract runs against SQLite
- **WHEN** the contract suite creates, reads, updates, and deletes supported entities using the SQLite driver
- **THEN** the suite SHALL pass with the documented transaction, revision, uniqueness, and ordering semantics

#### Scenario: Repository contract runs against PostgreSQL
- **WHEN** the same contract suite runs against PostgreSQL using the configured database URL
- **THEN** it SHALL produce the same observable results and typed conflict or idempotency outcomes as SQLite

#### Scenario: Unsupported driver fails before workers start
- **WHEN** configuration selects a storage driver without an implementation
- **THEN** startup SHALL fail before migrations or application workers are started and SHALL identify the selected driver

### Requirement: Durable schema and migration compatibility

The system SHALL provide versioned migrations for all repository entities and SHALL record the applied schema version in the selected database. Migrations SHALL be repeatable at startup, ordered, and safe to run after a process restart. Existing committed data SHALL remain readable after a forward migration.

#### Scenario: Fresh database is initialized
- **WHEN** startup runs against an empty SQLite or PostgreSQL database
- **THEN** all required tables, indexes, constraints, and schema-version records SHALL be created before durable workers accept work

#### Scenario: Restart reapplies no completed migration
- **WHEN** startup runs again after all migrations have succeeded
- **THEN** completed migrations SHALL not be applied twice and the database SHALL retain its data

#### Scenario: Failed migration blocks service startup
- **WHEN** a migration cannot be applied
- **THEN** startup SHALL report the migration failure and SHALL not expose workers that could write against a partially migrated schema

### Requirement: Committed knowledge generations are authoritative

The selected database SHALL be the source of truth for committed source bytes, generated artifacts, lineage, manifests, and generation status. Each committed generation SHALL have a monotonic generation identifier, an immutable manifest, and a commit marker. Filesystem materialization SHALL be treated as a derived export and SHALL never make an uncommitted or partial generation visible to readers.

#### Scenario: Generation commits as one logical state transition
- **WHEN** a compiler finalizes source metadata, artifacts, lineage, and a manifest
- **THEN** the system SHALL expose the new generation to readers only after its commit marker and all required database rows are durable

#### Scenario: Failed generation preserves the previous committed state
- **WHEN** compilation fails before the new generation commit marker is durable
- **THEN** readers SHALL continue to observe the previous committed generation and SHALL not resolve citations to staged rows

#### Scenario: Materialization is rebuilt from a committed manifest
- **WHEN** derived artifact files are missing or deleted for a committed generation
- **THEN** reconciliation SHALL rebuild them from the committed database manifest and SHALL preserve artifact identities and lineage

### Requirement: Transactional outboxes preserve cross-boundary intents

The system SHALL persist outbox intents in the same database transaction as the state change that requires delivery. It SHALL provide distinct outbox types for schedule dispatch to the Baymax mailbox, Stele context projection, and webhook delivery. Each outbox row SHALL contain a stable idempotency key, payload version, attempt state, and retry metadata.

#### Scenario: State change and outbox intent commit together
- **WHEN** a transaction creates a state change that requires Baymax, Stele, or webhook delivery
- **THEN** either both the state change and its corresponding outbox row SHALL be committed, or neither SHALL be visible after restart

#### Scenario: Outbox survives a publisher crash
- **WHEN** a publisher process exits after an outbox row is committed but before delivery is acknowledged
- **THEN** startup reconciliation SHALL find the pending row and make it eligible for retry with the same idempotency key

#### Scenario: Duplicate delivery is logically safe
- **WHEN** the same outbox row is delivered more than once due to a lease loss or publisher restart
- **THEN** the downstream operation SHALL receive the same idempotency key and the application SHALL record one logical delivery result rather than creating a second application effect

### Requirement: Leases and recovery are restart-safe

The system SHALL represent worker ownership with durable leases containing an owner, expiry, heartbeat or renewal metadata, and a fencing value. A worker SHALL process only a lease it currently owns. Startup reconciliation SHALL identify expired leases, pending outboxes, incomplete materialization, and committed generations needing projection or export.

#### Scenario: Expired lease becomes claimable
- **WHEN** a worker lease expires without a valid renewal
- **THEN** another worker SHALL be able to claim the work with a higher fencing value and the expired owner SHALL be unable to commit a later result

#### Scenario: Restart resumes pending durable work
- **WHEN** the process restarts with pending outbox rows or incomplete materialization
- **THEN** reconciliation SHALL classify each item and enqueue it for safe retry or rebuild without losing the original idempotency key

#### Scenario: Reconciliation is repeatable
- **WHEN** reconciliation runs more than once against the same database state
- **THEN** it SHALL not create duplicate outbox intents, generations, artifacts, or effect results

### Requirement: Conditional idempotent effects prevent duplicate mutations

The system SHALL provide an effect-key repository scoped by operation type, logical subject, and occurrence or request key. A mutation using an existing effect key SHALL return the original result, while a concurrent mutation using the same key SHALL serialize to one logical effect. Effect records SHALL be committed atomically with the mutation they protect.

#### Scenario: Replayed mutation returns the original result
- **WHEN** a task, memory projection, notification, or compilation mutation is retried with the same effect key after an uncertain outcome
- **THEN** the repository SHALL return the original effect result without applying a second mutation

#### Scenario: Concurrent same-key mutations converge
- **WHEN** two workers attempt the same effect key concurrently
- **THEN** exactly one mutation SHALL commit and both callers SHALL observe the same logical effect result or a typed in-progress outcome

#### Scenario: Different keys remain independent
- **WHEN** two valid mutations use different effect keys for the same subject
- **THEN** each mutation SHALL be evaluated independently according to the repository's revision and business constraints

### Requirement: Optimistic revisions protect concurrent task and entity updates

The system SHALL maintain a monotonic revision for mutable entities that can be changed by the Web workbench, Agent runs, scheduled actions, or recovery workers. Conditional updates SHALL require an expected revision and SHALL return a typed revision conflict without overwriting a newer state when the expected revision is stale.

#### Scenario: Update with current revision succeeds
- **WHEN** a caller updates an entity with its current expected revision
- **THEN** the update SHALL commit and increment the entity revision exactly once

#### Scenario: Stale update is rejected
- **WHEN** a caller updates an entity with an older expected revision
- **THEN** the repository SHALL return a revision conflict and SHALL preserve the newer stored state

### Requirement: Crash-boundary evidence verifies recoverability

The system SHALL provide deterministic tests for each documented transaction boundary, including state-plus-outbox commit, lease claim, effect commit, generation commit, and derived materialization. Each crash test SHALL verify that recovery yields either the previous committed state or the complete new state, with no reader-visible partial generation.

#### Scenario: Crash before commit marker
- **WHEN** a deterministic failure occurs before a generation commit marker is durable
- **THEN** the previous committed generation SHALL remain the only reader-visible generation

#### Scenario: Crash after commit marker
- **WHEN** a deterministic failure occurs after a generation commit marker is durable but before filesystem export completes
- **THEN** readers SHALL observe the new committed generation and reconciliation SHALL complete the missing derived export

#### Scenario: Crash around outbox publication
- **WHEN** a deterministic failure occurs before or after an outbox publisher sends its payload
- **THEN** restart SHALL retain the outbox intent and repeated publication SHALL be safe through the stable idempotency key

### Requirement: Task execution state is durable and driver-neutral

The system SHALL expose shared SQLite/PostgreSQL repositories for execution records, plan versions, checkpoints, pause states, replan history, and execution evidence. These records SHALL reference task revisions, plan and capability digests, parent and child correlations, and terminal outcomes using the same observable error and transaction semantics on both drivers.

#### Scenario: Execution admission is durable
- **WHEN** a task run is admitted with a validated ExecutionPlan
- **THEN** the execution record and its initial plan notebook entry SHALL commit together before a worker dispatches Baymax work

#### Scenario: Checkpoint survives restart
- **WHEN** a worker persists a safe-point checkpoint and the process exits
- **THEN** a fresh process SHALL retrieve the checkpoint, pause reason, plan version, and completed effect references without reconstructing them from logs

#### Scenario: SQLite and PostgreSQL expose the same execution result
- **WHEN** the execution repository contract runs against SQLite and PostgreSQL
- **THEN** both drivers SHALL return the same execution status, revision conflict, not-found, and idempotency outcomes

### Requirement: Plan notebook mutations are conditional and replay-safe

The system SHALL update execution records and plan notebook entries with expected revisions and stable effect keys. A stale checkpoint, terminal result, retry, or replan write SHALL return a typed conflict without overwriting newer execution state, and replaying a completed execution effect SHALL return the original result.

#### Scenario: Stale execution update is rejected
- **WHEN** two workers update the same execution with the same expected revision
- **THEN** exactly one update SHALL commit and the other SHALL receive a typed execution revision conflict

#### Scenario: Replayed terminal result is idempotent
- **WHEN** a worker retries a terminal result after losing its lease
- **THEN** the repository SHALL return the original terminal result for the same effect key and SHALL not append a duplicate terminal transition

#### Scenario: Replan retains prior evidence
- **WHEN** a new plan version is committed for an execution
- **THEN** the notebook SHALL retain the previous plan reference, replan reason, completed effect references, and new plan digest in one recoverable history

### Requirement: Schedule and delivery state share durable transaction boundaries

The system SHALL expose SQLite/PostgreSQL repositories for schedule definitions, resolved occurrences, dispatch attempts, webhook delivery records, and delivery outbox intents. A schedule state change that requires dispatch or notification SHALL commit its durable intent in the same transaction, and both drivers SHALL expose the same revision, idempotency, ordering, and recovery behavior.

#### Scenario: Schedule occurrence and dispatch intent commit together
- **WHEN** a schedule occurrence is admitted for delivery
- **THEN** the occurrence row and its dispatch outbox intent SHALL either both be visible after restart or neither SHALL be visible

#### Scenario: SQLite and PostgreSQL agree on occurrence replay
- **WHEN** the same occurrence key is admitted twice through either storage driver
- **THEN** both calls SHALL return one logical occurrence and the same dispatch intent identity

#### Scenario: Delivery status survives restart
- **WHEN** a webhook delivery has pending attempts and the process exits
- **THEN** a fresh process SHALL recover the delivery lease state, attempt count, next retry time, and terminal status from durable records

### Requirement: Occurrence and delivery mutations are conditionally idempotent

The system SHALL enforce uniqueness for schedule revision plus occurrence identity and for delivery effect keys. Conditional updates SHALL require the expected occurrence or delivery revision, and stale workers SHALL receive typed conflicts without overwriting newer dispatch or delivery state.

#### Scenario: Stale occurrence worker cannot advance dispatch
- **WHEN** two workers update one occurrence with the same expected revision
- **THEN** exactly one update SHALL commit and the other SHALL receive a typed revision conflict

#### Scenario: Replayed delivery returns original result
- **WHEN** a worker retries a completed webhook or scheduled mutation using the same effect key
- **THEN** the repository SHALL return the original result and SHALL not append a duplicate terminal transition

#### Scenario: Lease fencing protects delivery commits
- **WHEN** a replacement worker claims an expired delivery lease
- **THEN** the previous worker SHALL be unable to acknowledge, retry, or terminally fail the delivery with its stale fencing value

### Requirement: Parser jobs and results are durable and driver neutral

The storage layer SHALL persist parser job identity, idempotency key, source fingerprint, status, attempts, resource policy, result reference, failure details, and cleanup state. SQLite and PostgreSQL SHALL expose the same observable behavior for parser job creation, duplicate submission, status transitions, result lookup, cancellation, and recovery.

#### Scenario: Parser job and source metadata commit together
- **WHEN** an ingestion request creates a parser job and its source metadata
- **THEN** the job identity and source record SHALL either both be visible after restart or neither SHALL be visible

#### Scenario: Duplicate parser request returns the original result
- **WHEN** a caller retries a request with the same parser idempotency key
- **THEN** the repository SHALL return the original job or result without creating a second parser job

#### Scenario: SQLite and PostgreSQL agree on recovery
- **WHEN** the parser repository contract runs against SQLite and PostgreSQL after an interrupted job
- **THEN** both drivers SHALL expose the same retryable, terminal, cancellation, and cleanup states

### Requirement: Native execution projections have durable replay cursors

The storage layer SHALL persist versioned product projections of actual runtime evidence with execution identity, native source/event identity, source ordering when defined, application observation sequence, plan and authority correlation, and sanitized payload. Repeated delivery of the same native event SHALL be idempotent; conflicting content for that identity SHALL return a typed conflict. Projection persistence and application cursor advancement SHALL commit atomically on SQLite and PostgreSQL. Replay SHALL be bounded, ordered, read-only, execution-scoped, and independent of model/tool execution; expired, foreign, malformed, or missing-history cursors SHALL return typed errors without claiming complete replay.

#### Scenario: Overlapping delivery has one durable event
- **WHEN** two deliveries contain the same native event identity and equivalent normalized content
- **THEN** both drivers SHALL retain one projection and return the same observation sequence without duplicating a product effect

#### Scenario: Projection crash does not advance the cursor
- **WHEN** a failure occurs before the event projection and cursor transaction commits
- **THEN** the prior application cursor SHALL remain durable and reconciliation SHALL use verified source replay or report a typed history gap

#### Scenario: Replay survives process restart
- **WHEN** a fresh process receives a valid cursor for retained execution evidence
- **THEN** it SHALL return the same ordered retained projections and terminal evidence after that cursor without starting or resuming an execution

#### Scenario: Foreign cursor is rejected
- **WHEN** a caller supplies a cursor bound to another execution or scope
- **THEN** replay SHALL return a typed cursor/scope error and disclose no foreign evidence

#### Scenario: Conflicting native identity is rejected
- **WHEN** a repeated native event identity carries different normalized content
- **THEN** the repository SHALL preserve the existing evidence and return a typed evidence conflict without advancing the cursor

### Requirement: Runtime and context bindings preserve application authority

The storage layer SHALL persist versioned bindings between application authority records, native checkpoint references, source replay positions, and bounded context projection references or snapshots. Bindings SHALL retain execution identity, plan version/digest, capability digest, projection digest, task/schedule revision where applicable, write scope, effect namespace, completed effect keys, and native checkpoint identity/version/digest. Saving an application checkpoint and pause transition SHALL be one conditional transaction. Both drivers SHALL preserve existing committed executions and distinguish legacy application-only checkpoints from verified native recovery bindings. Cross-store reconciliation SHALL NOT invent a runtime checkpoint, advance an unconfirmed source position, or replay an uncertain mutation.

#### Scenario: Authority checkpoint and pause commit together
- **WHEN** a validated native checkpoint reference is bound to a paused application execution
- **THEN** the application checkpoint and pause transition SHALL either both commit with the expected revision or neither become visible after restart

#### Scenario: Missing upstream reference fails closed
- **WHEN** reconciliation finds an application recovery record whose native checkpoint or context evidence is missing or invalid
- **THEN** the execution SHALL remain recoverable only through a typed recovery-required or replan boundary and SHALL NOT silently restart tools

#### Scenario: Legacy record is not upgraded into native evidence
- **WHEN** a forward migration encounters an existing application-only checkpoint or synthesized probe event
- **THEN** the record SHALL remain readable with explicit legacy provenance and SHALL NOT be labeled as a verified native checkpoint or replayable native event

#### Scenario: Both drivers reject stale authority updates
- **WHEN** a stale worker attempts to replace a binding using an old expected execution revision
- **THEN** SQLite and PostgreSQL SHALL return the same typed revision conflict and preserve the newer binding, completed effects, and execution state
