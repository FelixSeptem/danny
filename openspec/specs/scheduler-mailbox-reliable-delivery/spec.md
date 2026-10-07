# scheduler-mailbox-reliable-delivery Specification

## Purpose
This capability provides restart-safe scheduled Agent execution with deterministic time semantics, native Baymax scheduler and mailbox boundaries, occurrence-scoped effects, and independently retryable notification delivery.

## Requirements

### Requirement: Schedule definitions produce deterministic occurrences

The system SHALL persist schedule definitions with cron expression, IANA timezone, action type, instruction or payload, enabled state, retry policy, misfire policy, backfill limit, and optimistic revision. Each resolved occurrence SHALL contain the schedule revision, local timestamp, resolved UTC instant, tzdata version, DST decision, and a stable occurrence key.

#### Scenario: Normal occurrence has stable identity
- **WHEN** an enabled schedule is evaluated for a valid local time
- **THEN** the system SHALL persist one occurrence with the schedule revision, timezone data version, resolved instant, and a key that remains stable across restart

#### Scenario: Nonexistent and ambiguous DST times are explicit
- **WHEN** a schedule resolves to a nonexistent or ambiguous local time
- **THEN** the system SHALL skip a nonexistent time and select the earlier UTC instant for an ambiguous time, recording the decision in the occurrence

#### Scenario: Stale schedule update is rejected
- **WHEN** a caller updates a schedule using an older expected revision
- **THEN** the system SHALL return a typed revision conflict and SHALL preserve the newer schedule definition

### Requirement: Schedule dispatch uses Baymax scheduler and mailbox boundaries

The system SHALL persist an occurrence and its schedule-dispatch outbox intent before publishing work to Baymax. Dispatch SHALL use the existing Baymax scheduler and mailbox contracts for delayed delivery, lease ownership, retry, acknowledge, requeue, and dead-letter handling, and SHALL correlate the occurrence key with the Agent execution.

#### Scenario: Occurrence dispatch survives a publisher crash
- **WHEN** the application commits an occurrence and exits before publishing its mailbox message
- **THEN** restart reconciliation SHALL find the pending outbox intent and retry the same occurrence with the same idempotency key

#### Scenario: Mailbox lease loss does not duplicate the occurrence
- **WHEN** a mailbox worker loses its lease after dispatching an occurrence
- **THEN** a replacement worker SHALL reconcile the same occurrence key and SHALL not create a second logical execution

#### Scenario: Terminal mailbox failure is recorded
- **WHEN** Baymax exhausts retry attempts and moves an occurrence to its dead-letter boundary
- **THEN** the application SHALL record a terminal dispatch failure linked to the occurrence and SHALL leave the schedule available for later occurrences

### Requirement: Scheduled effects are occurrence-scoped and idempotent

The system SHALL derive a stable effect key for every scheduled mutation from the schedule identity, occurrence identity, and operation name. Replaying a task, Stele memory, compilation, notification, or other scheduled mutation with the same effect key SHALL return the original result without applying a second logical effect.

#### Scenario: Scheduled memory replay returns the original result
- **WHEN** the same occurrence retries a Stele memory write after an uncertain response
- **THEN** the application SHALL reuse the occurrence effect key and SHALL expose the original logical result

#### Scenario: Different occurrences remain independent
- **WHEN** two occurrences of the same schedule execute the same operation
- **THEN** each occurrence SHALL receive a distinct effect key and SHALL be evaluated independently

#### Scenario: Effect authority cannot expand during retry
- **WHEN** a retry resumes an occurrence after a process restart
- **THEN** the Agent SHALL use the persisted policy, capability digest, projection digest, and plan version or enter an explicit approval pause before expanding them

### Requirement: Webhook delivery is durable and independent from Agent execution

The system SHALL persist webhook delivery intent with the Agent terminal result in a durable delivery record or outbox transaction. Delivery SHALL have its own lease, attempt count, retry schedule, and terminal failure state, and SHALL not change a previously committed Agent success or failure.

#### Scenario: Crash before webhook enqueue is recoverable
- **WHEN** an Agent execution reaches a terminal result and the process exits during notification enqueue
- **THEN** restart reconciliation SHALL recover the durable delivery intent or report a transaction failure without losing the notification permanently

#### Scenario: Webhook retries preserve logical identity
- **WHEN** a webhook sender retries after timeout or lease loss
- **THEN** every attempt SHALL use the same delivery idempotency key and the application SHALL record one logical delivery result

#### Scenario: Webhook terminal failure is separate
- **WHEN** all webhook delivery attempts are exhausted
- **THEN** the delivery record SHALL become terminally failed while the associated Agent execution status remains unchanged

### Requirement: Misfire handling is bounded and explicit

The system SHALL support `skip`, `coalesce_latest`, and bounded `backfill` misfire policies. A restart or downtime SHALL apply the persisted policy and backfill limit, record which occurrences were skipped, coalesced, or backfilled, and SHALL never create an unbounded burst.

#### Scenario: Coalesce latest creates one dispatch
- **WHEN** several schedule occurrences are missed and the policy is `coalesce_latest`
- **THEN** reconciliation SHALL create one occurrence for the latest eligible time and record the earlier missed times as coalesced

#### Scenario: Backfill honors its limit
- **WHEN** the policy is `backfill` with a finite limit
- **THEN** reconciliation SHALL create no more than the configured number of occurrence dispatches and SHALL record the remaining missed times
