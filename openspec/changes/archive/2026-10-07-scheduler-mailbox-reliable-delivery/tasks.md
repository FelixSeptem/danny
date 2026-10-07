## 1. Schedule and occurrence domain

- [x] 1.1 Define schedule definitions, action types, retry policies, misfire policies, and revision-aware repository contracts, and verify invalid cron, timezone, policy, and revision inputs return typed errors.
- [x] 1.2 Add versioned SQLite/PostgreSQL migrations for schedules, occurrences, dispatch attempts, webhook deliveries, and delivery leases, and verify fresh startup and forward migration preserve all existing task, compilation, and harness tables.
- [x] 1.3 Implement schedule repositories with expected-revision updates and stable schedule identity, and verify stale updates return a revision conflict without overwriting newer definitions.
- [x] 1.4 Implement a timezone-aware occurrence calculator using IANA timezone data, recorded tzdata version, explicit DST decisions, and stable occurrence keys, and verify normal, nonexistent, and ambiguous local-time fixtures.
- [x] 1.5 Implement `skip`, `coalesce_latest`, and bounded `backfill` misfire reconciliation, and verify downtime fixtures record skipped, coalesced, and backfilled decisions without unbounded dispatch.

## 2. Transactional dispatch boundary

- [x] 2.1 Define occurrence state transitions, dispatch attempt records, and typed terminal, retryable, lease-lost, and recovery-required errors, and verify invalid transitions are rejected before persistence.
- [x] 2.2 Implement atomic occurrence admission with schedule revision, occurrence identity, dispatch outbox intent, policy digest, and correlation fields, and verify a failure rolls back both occurrence and outbox rows.
- [x] 2.3 Implement occurrence-scoped effect-key derivation for task, memory, compilation, notification, and webhook operations, and verify equivalent retries produce identical keys while different occurrences remain independent.
- [x] 2.4 Implement restart reconciliation for committed occurrences whose dispatch intent is pending, leased, expired, or uncertain, and verify reconciliation reuses the original outbox idempotency key.
- [x] 2.5 Add SQLite and PostgreSQL repository contract fixtures for occurrence replay, revision conflicts, ordering, lease fencing, and restart recovery, and verify both drivers expose the same observable results.

## 3. Baymax scheduler and mailbox integration

- [x] 3.1 Audit the pinned Baymax scheduler and mailbox APIs against the required delayed-dispatch, lease, retry, acknowledge, requeue, QoS, and DLQ behaviors, and verify the deterministic adapter fixture covers every required transition.
- [x] 3.2 Audit found no missing Baymax contract in the pinned version; no upstream blocker or workaround was introduced.
- [x] 3.3 Implement the narrow Baymax scheduler/mailbox adapter carrying occurrence key, schedule revision, policy digest, plan version, capability digest, projection digest, and effect namespace, and verify payload correlation survives publish and consume.
- [x] 3.4 Implement the mailbox worker boundary that claims an occurrence lease, dispatches the task harness once, acknowledges successful delivery, and requeues or dead-letters failures, and verify lease loss cannot create a second logical execution.
- [x] 3.5 Add fake Baymax scheduler and mailbox fixtures for delayed dispatch, retry, requeue, dead-letter, duplicate delivery, and crash-before-ack windows, and verify each fixture maps to one durable occurrence state.

## 4. Scheduled KnowledgeAgent and Stele effects

- [x] 4.1 Extend scheduled Agent admission to persist and validate occurrence-scoped policy, capability, projection, plan, and effect namespace data, and verify a retry cannot silently widen authority.
- [x] 4.2 Connect scheduled mailbox commands to the existing durable task harness and verify Agent success, failure, pause, and recovery-required states retain occurrence and execution correlations.
- [x] 4.3 Audit the pinned Stele public idempotency behavior for occurrence-scoped memory writes and restart replay, and verify the real adapter accepts and preserves the same effect key.
- [x] 4.4 Audit found no missing Stele idempotency contract in the pinned version; no upstream blocker or local memory workaround was introduced.
- [x] 4.5 Implement scheduled Stele projection and memory effect handling through the existing adapter only, and verify repeated occurrence delivery returns the original Stele result without a duplicate logical mutation.
- [x] 4.6 Add prompt-injection and authority fixtures for scheduled instructions and retrieved documents, and verify untrusted content cannot authorize undeclared task, memory, compilation, notification, or webhook mutations.

## 5. Webhook and notification delivery

- [x] 5.1 Define durable webhook delivery records, attempt states, retry schedule, lease fencing, terminal failure, and delivery effect-key contracts, and verify Agent terminal status is not mutated by delivery transitions.
- [x] 5.2 Implement an atomic terminal-result plus webhook-intent transaction, and verify a crash before enqueue either rolls back the notification intent or leaves a recoverable durable delivery record.
- [x] 5.3 Implement the independent webhook delivery worker boundary with lease claim, retry, backoff, acknowledgement, and terminal failure, and verify the same delivery key is used across all attempts.
- [x] 5.4 Add fake webhook fixtures for timeout, non-2xx response, lease loss, successful retry, duplicate acknowledgement, and terminal exhaustion, and verify each result is durable and replay-safe.

## 6. Recovery and acceptance validation

- [x] 6.1 Add end-to-end SQLite crash-window fixtures covering occurrence commit, outbox publication, mailbox acknowledgement, Agent terminal write, Stele effect replay, and webhook enqueue, and verify recovery yields one logical occurrence and effect per key.
- [x] 6.2 Add optional PostgreSQL end-to-end fixtures for the same schedule, mailbox, effect, and delivery scenarios, and verify the configured driver produces the same statuses, conflicts, and idempotency outcomes.
- [x] 6.3 Add documentation for schedule configuration, timezone and DST policy, misfire behavior, occurrence keys, Baymax mailbox boundaries, Stele effect keys, and independent delivery status, and verify it matches the public repository and adapter contracts.
- [x] 6.4 Run `go test ./...`, the SQLite/PostgreSQL contract suites, and `golangci-lint run ./...`, and verify all scheduled harness packages pass with no new lint findings.
- [x] 6.5 Run `openspec validate scheduler-mailbox-reliable-delivery --strict --type change` and `openspec validate --specs --strict`, and verify the change artifacts and main specifications remain structurally valid.
