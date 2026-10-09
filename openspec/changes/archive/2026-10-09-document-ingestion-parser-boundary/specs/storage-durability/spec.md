## ADDED Requirements

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
