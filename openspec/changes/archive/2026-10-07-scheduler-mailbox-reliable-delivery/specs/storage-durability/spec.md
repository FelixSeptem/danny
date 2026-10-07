## ADDED Requirements

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
