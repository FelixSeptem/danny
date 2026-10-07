## Why

Milestones 0 through 5 provide durable task execution, deterministic planning, and Baymax/Stele adapter boundaries, but scheduled work still has no restart-safe occurrence identity or reliable handoff from the application database to Baymax scheduler and mailbox execution. This change adds the durable scheduling and delivery boundary needed to validate long-running personal-agent tasks, idempotent Stele memory writes, and independently retryable webhook notifications in real crash and time-zone scenarios.

## What Changes

- Add schedule definitions with cron, IANA timezone, action type, instruction or payload, enabled state, retry policy, misfire policy, backfill limit, and optimistic revision.
- Add deterministic schedule occurrence calculation with local timestamp, resolved UTC instant, schedule revision, tzdata version, DST decision, and stable occurrence key.
- Add durable schedule dispatch outbox and a Baymax scheduler/mailbox adapter for delayed dispatch, lease, retry, acknowledge, requeue, and dead-letter handling.
- Persist occurrence state before dispatch and reconcile occurrence rows and outbox intents after restart without losing the original idempotency key.
- Scope every scheduled mutation by occurrence effect key, including task, Stele memory, compilation, notification, and webhook effects.
- Add durable webhook delivery records and an independent delivery worker contract so Agent execution status remains separate from notification delivery status.
- Define bounded `skip`, `coalesce_latest`, and `backfill` misfire behavior and explicit DST handling for nonexistent and ambiguous local times.
- Add deterministic fake Baymax scheduler/mailbox fixtures, time-zone fixtures, crash-window tests, and optional PostgreSQL integration coverage.
- Keep HTTP API, Web workbench, Docker worker decomposition, and UI schedule authoring outside this change.

## Capabilities

### New Capabilities

- `scheduler-mailbox-reliable-delivery`: schedule definitions, occurrence identity, Baymax scheduler/mailbox dispatch, occurrence-scoped effects, and durable webhook delivery.

### Modified Capabilities

- `storage-durability`: extend the repository and transaction contracts for schedule occurrences, dispatch intents, webhook delivery records, and restart reconciliation.
- `knowledge-agent-context-assembly`: extend scheduled Agent execution with occurrence-scoped authority, effect keys, and explicit separation between Agent terminal status and delivery status.

## Impact

- Affected code: `internal/storage`, `internal/tasks`, `internal/agent/baymax`, `internal/context/stele`, configuration, and deterministic fixtures.
- Affected persistence: schedules, occurrences, delivery records, and outbox payloads will use the existing SQLite/PostgreSQL migration and revision boundaries.
- Affected Baymax integration: Danny will call Baymax's scheduler and mailbox boundaries and will not reimplement lease, retry, QoS, or dead-letter semantics.
- Affected Stele integration: scheduled memory writes will use the existing public Stele adapter and occurrence-scoped idempotency metadata; no local reference or memory store will be introduced.
- Upstream dependency risk: any missing Baymax mailbox/scheduler or Stele idempotency contract will be recorded in the corresponding upstream findings document and will block the dependent task without a production workaround.
