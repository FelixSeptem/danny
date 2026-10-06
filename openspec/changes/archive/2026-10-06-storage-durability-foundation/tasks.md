## 1. Repository contracts and schema model

- [x] 1.1 Define driver-neutral repository interfaces, transaction scopes, typed conflict/error categories, and test fixtures for conversations, tasks, schedules, executions, notifications, source metadata, knowledge generations, artifacts, manifests, revisions, idempotency effects, leases, and outboxes; verify the interfaces compile without importing a SQL driver into domain packages.
- [x] 1.2 Define the canonical schema model, foreign keys, uniqueness constraints, indexes, revision columns, generation commit markers, outbox payload versions, and effect-result envelopes; verify a schema design review checklist covers every requirement in `specs/storage-durability/spec.md`.
- [x] 1.3 Add migration runner and schema-version tracking with ordered, repeatable migrations; verify a fresh database reaches the expected schema version and a restart does not reapply completed migrations.

## 2. SQLite implementation

- [x] 2.1 Add the SQLite driver, connection configuration, transaction helpers, and migration integration; verify the application can initialize a clean local database using the existing `DANNY_STORAGE_DRIVER=sqlite` defaults.
- [x] 2.2 Implement SQLite repositories for core entities and optimistic revision updates; verify create/read/update/delete contract tests and stale revision conflicts.
- [x] 2.3 Implement SQLite generation, artifact, manifest, and commit-marker repositories; verify uncommitted rows are hidden from committed readers and generation identifiers are monotonic.
- [x] 2.4 Implement SQLite lease, fencing, effect-key, and outbox repositories; verify concurrent claim, duplicate-key, expiry, and conditional mutation behavior with deterministic tests.

## 3. PostgreSQL implementation

- [x] 3.1 Add the PostgreSQL driver, connection configuration, migrations, and health checks; verify a disposable PostgreSQL database reaches the same schema version as SQLite.
- [x] 3.2 Implement PostgreSQL repositories using the same interfaces and transaction semantics; verify the shared repository contract suite produces the same typed outcomes as SQLite.
- [x] 3.3 Exercise PostgreSQL concurrency for revision conflicts, lease fencing, effect-key serialization, and ordered outbox claims; verify no stale owner or stale revision can overwrite newer state.

## 4. Transactional outboxes and recovery

- [x] 4.1 Implement state-plus-outbox transaction helpers for Baymax mailbox schedule dispatch, Stele context projection, and webhook delivery; verify a forced failure before commit leaves neither the state change nor outbox row and a successful commit leaves both.
- [x] 4.2 Implement outbox claim, lease renewal, retry, acknowledgement, dead-letter metadata, and stable idempotency-key handling; verify publisher restart leaves rows retryable and repeated publication preserves one logical application effect.
- [x] 4.3 Implement startup reconciliation for pending outboxes, expired leases, incomplete materialization, and committed generations missing derived exports or projection intents; verify reconciliation is repeatable and does not create duplicates.
- [x] 4.4 Add explicit adapter boundaries for Baymax mailbox and Stele projection delivery; verify the storage layer records durable intents without reimplementing upstream execution mechanics, and record a `blocked-upstream` finding if required upstream idempotency or acknowledgement behavior is unavailable.

## 5. Generation materialization and crash evidence

- [x] 5.1 Implement database-authoritative generation reads and derived filesystem materialization from immutable manifests; verify readers never expose staged generations and deleted exports can be rebuilt with identical artifact identities and lineage.
- [x] 5.2 Add deterministic crash injection points around state-plus-outbox commit, lease claim, effect commit, generation commit marker, and filesystem export; verify each crash recovers to the previous committed state or the complete new state.
- [x] 5.3 Add repository contract tests that run the same suite against SQLite and PostgreSQL, including migration, revision, lease, effect-key, outbox, generation, and restart cases; verify both drivers pass without driver-specific behavior exceptions.

## 6. Startup integration and documentation

- [x] 6.1 Integrate migration and reconciliation into process startup before durable workers are exposed; verify startup fails closed on migration or reconciliation errors and reports the affected database or recovery class.
- [x] 6.2 Document storage selection, PostgreSQL setup, transaction boundaries, outbox lifecycle, generation authority, recovery commands, and crash-test evidence; verify documentation consistency checks pass.
- [x] 6.3 Run formatting, static checks, unit tests, repository contract tests, and the full OpenSpec validation; verify `openspec validate storage-durability-foundation --strict` passes and no unrelated files such as `.tmp/` or `debug.log` are modified.
