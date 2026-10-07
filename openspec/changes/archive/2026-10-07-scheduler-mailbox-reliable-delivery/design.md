## Context

The project already has SQLite/PostgreSQL repositories, transactional outboxes, leases with fencing, effect-key storage, a durable task execution harness, a Baymax adapter exposing scheduler and mailbox probes, and a Stele adapter that sends public idempotency metadata. The missing boundary is schedule-specific time resolution and the durable handoff from a schedule occurrence to Baymax execution and later delivery effects.

This change builds on the existing task harness and repository contracts. Baymax remains the owner of scheduler and mailbox mechanics; Stele remains the owner of context and memory persistence. Danny owns schedule definitions, occurrence identity, application transaction boundaries, authorization policy, and delivery records.

## Goals / Non-Goals

**Goals:**

- Make occurrence admission, dispatch, retry, misfire handling, and delivery recovery deterministic and restart-safe.
- Preserve the schedule revision, timezone decision, occurrence key, policy digest, plan version, and effect namespace across every retry.
- Use native Baymax scheduler/mailbox boundaries and public Stele idempotency metadata through narrow adapters.
- Keep Agent execution status independent from Stele projection and webhook delivery status.
- Provide deterministic fake-clock, fake-Baymax, fake-delivery, SQLite, and optional PostgreSQL fixtures for crash windows.

**Non-Goals:**

- HTTP API, Web workbench, schedule authoring UI, or Docker worker decomposition.
- Reimplementing Baymax scheduler, mailbox lease, retry, QoS, or dead-letter internals.
- Reimplementing Stele memory or context storage, or adding a local reference store.
- Implementing unbounded catch-up, arbitrary calendar rules beyond the declared cron and timezone contract, or automatic Agent replay after delivery failure.

## Decisions

### 1. Store schedule occurrences as application records before dispatch

The application database is the source of truth for schedule revision, occurrence identity, and dispatch intent. Admission inserts the occurrence and schedule outbox row in one transaction before calling Baymax. Publishing first was rejected because a crash could create a mailbox message with no recoverable application occurrence.

### 2. Resolve local time with an explicit time policy

Use the Go timezone database available to the configured process and record the tzdata version with each occurrence. Nonexistent local times are skipped. Ambiguous local times select the earlier UTC instant. Misfire reconciliation applies the persisted policy (`skip`, `coalesce_latest`, or bounded `backfill`) and records the decision rather than deriving a new key from restart timing.

### 3. Keep occurrence dispatch and Agent execution distinct

The scheduler worker claims an application occurrence and publishes a Baymax mailbox command containing the occurrence key, policy digest, plan reference, and effect namespace. The task harness owns the Agent execution record and terminal commit. A mailbox retry may redeliver the command, but the occurrence effect key and execution admission idempotency key converge it to one logical Agent run.

### 4. Use occurrence-scoped effect keys for every mutation

Every scheduled side effect derives its key from schedule identity, occurrence identity, and operation name. The existing effect repository and public Stele idempotency metadata receive the same logical key. This keeps retries safe without requiring a local replacement for Stele behavior.

### 5. Persist webhook delivery intent with the terminal result

When an Agent execution produces a notification, the execution terminal result and webhook delivery intent are committed in one application transaction. A separate delivery worker claims the delivery row, sends the webhook, and records retry or terminal failure. Delivery state never changes the Agent execution status.

### 6. Treat missing upstream contracts as blockers

If Baymax does not expose the scheduler or mailbox behavior required by the specs, or Stele does not preserve the supplied idempotency key for the required mutation, add a deterministic reproduction to the corresponding upstream findings document and block the dependent implementation task. A Danny-side retry, path rewrite, duplicate suppression layer, or alternate storage path is not an accepted workaround.

## Risks / Trade-offs

- [Risk] Cron libraries and timezone database versions can calculate different occurrences. → Mitigation: record the resolved instant, decision, and tzdata version and test with a pinned clock and timezone fixture.
- [Risk] A mailbox message can be delivered more than once. → Mitigation: admission idempotency and occurrence-scoped effect keys converge duplicate deliveries to one logical execution.
- [Risk] A crash can leave Baymax execution status and application occurrence state temporarily uncertain. → Mitigation: retain recovery-required state, correlation identifiers, and the original outbox key for reconciliation.
- [Risk] Webhook retries can outlive the Agent execution process. → Mitigation: delivery records and leases are durable and independently recoverable.
- [Risk] Provider or Stele idempotency semantics may be weaker than required. → Mitigation: block the dependent task and record the exact upstream version and reproduction instead of adding a production workaround.

## Migration Plan

1. Add schedule, occurrence, dispatch, and delivery migrations and repository contracts without changing existing task or compilation readers.
2. Implement deterministic occurrence calculation and schedule revision checks with fake-clock fixtures.
3. Implement the schedule dispatch outbox and Baymax scheduler/mailbox adapter; verify real adapter behavior and record any upstream blocker before proceeding.
4. Connect occurrence commands to the durable task harness and enforce occurrence-scoped effect keys for scheduled mutations.
5. Add webhook delivery records, retry worker boundaries, crash recovery fixtures, and optional PostgreSQL contract coverage.
6. Enable scheduled execution only after the full SQLite contract, Baymax fixture, Stele idempotency fixture, and crash-window suite pass.

Rollback disables new schedule admission and workers while preserving readable schedule, occurrence, execution, and delivery rows. Existing task harness and compilation paths remain usable.

## Open Questions

None. The DST, misfire, effect-key, and delivery policies are part of this proposal and are intentionally fixed before implementation.
