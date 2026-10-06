## Context

The repository currently contains the configuration contract and an empty storage package. Roadmap Milestone 2 is a prerequisite for knowledge compilation, tasks, scheduling, notifications, and Stele projections. The product design spans a database, derived filesystem materialization, Stele context state, and Baymax mailbox delivery; these boundaries cannot share one native transaction. See `proposal.md` and `specs/storage-durability/spec.md` for the motivation and observable requirements.

## Goals / Non-Goals

**Goals:**

- Make SQLite and PostgreSQL interchangeable through one repository contract.
- Make every cross-boundary intent durable before a publisher or worker acts.
- Make committed knowledge generations recoverable and independently rematerializable.
- Provide stable leases, fencing, effect keys, revisions, and crash-test seams for later workers.
- Keep Baymax responsible for mailbox execution mechanics and Stele responsible for context persistence; application storage records intents and correlation state.

**Non-Goals:**

- Implementing the KnowledgeAgent, compiler, scheduler policy, HTTP API, or Web UI.
- Reimplementing Baymax mailbox, scheduler, team, or Context Assembler internals.
- Making filesystem, database, Stele, and Baymax a single distributed transaction.
- Adding production workarounds for an upstream Baymax or Stele defect; a required upstream change blocks the affected task and is recorded in the corresponding findings document.

## Decisions

### 1. Use a database-first source of truth with derived filesystem export

Committed source bytes, generated artifacts, manifests, lineage, and commit markers live in the database. A generation becomes readable only after a database commit marker is durable. Files under the configured documents and artifacts directories are inputs or derived exports and are reconciled from the committed manifest. This avoids claiming atomicity across a SQL transaction and filesystem rename while still permitting convenient local browsing and Docker mounts.

**Alternative considered:** filesystem-first publication with database indexes. Rejected because a crash can expose files whose indexes, lineage, or generation status were never committed, and because PostgreSQL cannot roll back an arbitrary filesystem mutation.

### 2. Define repositories around transactional use cases

Repositories expose transaction scopes for state-plus-outbox changes rather than leaking driver-specific SQL transactions to domain packages. Each transaction boundary names the state rows, outbox rows, revision checks, and effect records that commit together. Driver implementations use `database/sql`; the SQLite driver is selected for CGO-free local and Docker operation, and the PostgreSQL driver supports the configured PostgreSQL profile.

**Alternative considered:** one generic key-value repository. Rejected because it cannot express foreign keys, uniqueness, revision conflicts, ordered outbox claims, or generation invariants consistently across both databases.

### 3. Keep three outbox types with a shared lifecycle

Schedule dispatch, Stele projection, and webhook delivery use separate typed outbox records even though they share claim, lease, retry, and idempotency fields. Payloads include a version, correlation identifiers, and an immutable logical key. Publishers acknowledge delivery only after the downstream adapter returns a durable or explicitly accepted result; uncertain outcomes leave the row retryable.

**Alternative considered:** publish directly inside request or worker code. Rejected because a process exit between the application commit and external publish permanently loses work.

### 4. Use fencing leases plus optimistic revisions

Leases protect worker ownership and fencing prevents an expired worker from committing late. Revisions protect mutable product entities from stale Web, Agent, scheduler, and recovery writes. They solve different races and are both required. A stale revision produces a typed conflict; it is not silently retried as an overwrite.

**Alternative considered:** process-local mutexes or leases without fencing. Rejected because they do not protect across restarts or multiple PostgreSQL processes.

### 5. Make effect keys first-class and commit them with mutations

An effect key is unique within an operation namespace and logical subject. The effect result and protected mutation are written in one transaction. Replays return the stored result, while callers can distinguish completed, in-progress, and rejected outcomes. Later scheduled operations will derive keys from the schedule occurrence and action identity.

**Alternative considered:** deduplicate only execution rows. Rejected because an execution record cannot prevent a mutation that succeeded immediately before a lease loss from being applied again.

### 6. Reconcile deterministically at startup

Startup runs migrations, then scans pending outboxes, expired leases, incomplete materialization, and committed generations missing derived exports or Stele projection intents. Reconciliation is idempotent and uses existing keys and manifest identities. Workers start only after migration and reconciliation establish a valid durable baseline.

**Alternative considered:** rely on each worker to discover abandoned work. Rejected because it leaves recovery behavior distributed and makes a clean restart unable to prove that all classes of incomplete work were considered.

### 7. Keep upstream boundaries explicit

The storage layer records Baymax mailbox and Stele projection intents, but does not emulate either dependency. If the pinned upstream API cannot honor the required idempotency or acknowledgement contract, the implementation task is marked `blocked-upstream` and the reproduction is added to `docs/upstream/baymax-findings.md` or `docs/upstream/stele-findings.md`.

## Risks / Trade-offs

- [Risk] Database blobs increase database size for source and generated content. → Mitigation: keep content-addressed bytes and metadata separate, expose retention/export policies in a later operations milestone, and retain manifests as the recovery index.
- [Risk] SQLite locking differs from PostgreSQL concurrency. → Mitigation: contract tests cover revision conflicts, lease claims, outbox claims, and effect-key races on both drivers; implementation uses short transactions and explicit busy/retry behavior documented by the repository contract.
- [Risk] A downstream Baymax or Stele acknowledgement can remain uncertain after a network failure. → Mitigation: retain the outbox row with the same idempotency key and require upstream or adapter evidence for safe replay; do not add a duplicate-suppressing application workaround.
- [Risk] Reconciliation can delay startup on a large database. → Mitigation: process bounded batches, expose progress and health state, and keep workers stopped until the durable baseline is valid.
- [Risk] Schema evolution can invalidate manifests or payloads. → Mitigation: version migrations, manifest format, outbox payloads, and effect-result envelopes; reject unknown versions rather than guessing.

## Migration Plan

1. Add the repository interfaces, migration runner, schema version table, and SQLite implementation.
2. Run the shared repository and crash-boundary contract suite against SQLite.
3. Add PostgreSQL migrations and implementation, then run the same suite against a disposable PostgreSQL instance.
4. Enable startup reconciliation in dry-run/reporting mode, verify pending work classification, then make it authoritative before later workers are introduced.
5. Roll back by stopping workers and restoring the prior database snapshot; never delete a committed generation or reuse a generation identifier during rollback.

## Open Questions

- The exact SQL driver versions and PostgreSQL minimum version can be selected during implementation as long as they satisfy the shared contract and Docker profile constraints.
- Retention and compaction policies for old committed generations are deferred to the operations milestone and do not change the commit or recovery contract.
