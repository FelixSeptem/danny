## ADDED Requirements

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
