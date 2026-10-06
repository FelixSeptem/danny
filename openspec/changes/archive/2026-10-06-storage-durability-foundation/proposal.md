## Why

The next roadmap milestone needs a durable application boundary before knowledge compilation, task mutation, scheduling, and delivery are implemented. The current repository has only configuration and package skeletons, so it cannot guarantee recoverable state across SQLite/PostgreSQL, the filesystem, Stele projections, or Baymax mailbox dispatch.

## What Changes

- Define a driver-neutral storage contract for product entities and transactional boundaries.
- Add SQLite and PostgreSQL migration plans with one shared repository behavior contract.
- Persist conversations, tasks, schedules, executions, notifications, source metadata, knowledge generations, artifacts, manifests, revisions, idempotency records, leases, and outbox records.
- Add durable outboxes for schedule dispatch to Baymax mailbox, Stele context projection, and webhook delivery.
- Add startup reconciliation for pending outboxes, expired leases, incomplete materialization, and committed knowledge generations.
- Define generation manifests, monotonic generation identifiers, commit markers, and rebuildable derived materialization.
- Define effect-key storage and conditional mutation semantics for idempotent scheduled effects.
- Add crash-boundary, restart, repository-contract, and materialization-rebuild evidence requirements.

## Capabilities

### New Capabilities

- `storage-durability`: Driver-neutral repositories, migrations, transactional outboxes, leases, idempotency effects, generation manifests, and startup recovery semantics.

### Modified Capabilities

- None.

## Impact

- Affected code: `internal/storage` and future task, knowledge, scheduler, notification, and context adapters that consume the storage contracts.
- Affected runtime configuration: existing `DANNY_STORAGE_DRIVER` and `DANNY_DATABASE_URL` become backed by concrete SQLite/PostgreSQL implementations.
- Affected dependencies: SQL drivers and migration tooling will be selected during implementation; Baymax and Stele remain integration targets behind durable outboxes rather than being reimplemented by the application.
- Affected operations: startup must run migrations and reconciliation before workers expose durable work; the mounted filesystem remains derived materialization and input, while committed database generations are authoritative.
