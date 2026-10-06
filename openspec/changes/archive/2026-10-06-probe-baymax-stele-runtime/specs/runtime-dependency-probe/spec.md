## Purpose

Define repeatable runtime evidence for the pinned Baymax and Stele dependencies before product features rely on Agent orchestration or context continuity.

## ADDED Requirements

### Requirement: Baymax runtime evidence is correlated
The system SHALL exercise the pinned Baymax dependency through its public runtime interfaces and emit a correlation record that joins the parent run, stream terminal state, typed tool lifecycle, bounded child run, team parallel run, team vote, mailbox message, scheduler occurrence, and Context Assembler projection when each probe is enabled. Each record SHALL include the exact Baymax version, probe case, stable correlation ID, terminal result, and ordered event evidence.

#### Scenario: Deterministic parent, tool, and child probe succeeds
- **WHEN** the deterministic probe uses its fake model to run a parent request that invokes a typed local tool and dispatches one bounded child
- **THEN** the system SHALL record the parent and child IDs, tool lifecycle evidence, one terminal outcome per run, and an unbroken parent-child correlation chain

#### Scenario: Run and Stream terminal evidence agrees
- **WHEN** the deterministic probe executes equivalent parent inputs through Baymax Run and Stream
- **THEN** the system SHALL record terminal status and aggregate result evidence that agree for the two executions

#### Scenario: Team, mailbox, scheduler, and context probes run
- **WHEN** the selected deterministic probe suite enables team, mailbox, scheduler, and Context Assembler cases
- **THEN** the system SHALL record parallel and vote outcomes, mailbox publish/lease/ack/requeue/dead-letter transitions, scheduler lease and delayed-dispatch transitions, and a context projection containing a prefix guard, handoff digest, and derived budget view

### Requirement: Stele context continuity is evidenced
The system SHALL exercise the pinned Stele dependency through its public interfaces for scoped context, long-term memory, task/run references, bounded reconstruction, and idempotent scheduled-write behavior. Each record SHALL include the exact Stele version, scope, context and memory identifiers where applicable, operation result, and correlation ID linking the operation to its Agent run.

#### Scenario: Scoped context and memory round trip
- **WHEN** the deterministic probe creates a scoped context, appends long-term memory, and attaches task and run references
- **THEN** the system SHALL read each record through the same scope and record their identifiers and returned values in the correlated evidence

#### Scenario: Bounded projection reconstructs after restart
- **WHEN** the deterministic probe reconstructs a context projection using a fresh adapter process over persisted Stele state
- **THEN** the system SHALL return a bounded projection that retains the recorded references and identifies the restored context scope

#### Scenario: Scheduled memory write is replayed
- **WHEN** the deterministic probe repeats a scheduled memory-write request with the same occurrence-scoped idempotency key
- **THEN** the system SHALL record whether Stele returns the original logical result without duplicate durable memory effects

### Requirement: Probe evidence supports offline contracts and opt-in live smoke
The system SHALL run deterministic probe contracts without a live model endpoint. It SHALL provide a separately invoked OpenAI-compatible smoke command that runs only when endpoint and model configuration are explicitly present, never logs credentials or unredacted provider payloads, and records the same redacted evidence schema.

#### Scenario: Offline contracts require no provider endpoint
- **WHEN** a contributor runs the deterministic runtime probe suite without model endpoint configuration
- **THEN** the suite SHALL complete using the fake model and fake clock without contacting a provider

#### Scenario: Live smoke is explicitly enabled
- **WHEN** a contributor invokes the live smoke command with a configured OpenAI-compatible endpoint and model
- **THEN** the system SHALL run the selected real adapter path and write redacted terminal and correlation evidence without exposing credentials

### Requirement: Required upstream defects block dependent work
The system SHALL classify a deterministic probe failure caused by a missing or incorrect Baymax or Stele contract as an upstream finding. The finding SHALL include the pinned version, deterministic reproduction, observed behavior, expected behavior, evidence location, affected roadmap tasks, upstream reference, and explicit unblock condition. Dependent tasks SHALL be marked `blocked-upstream` and SHALL not use a production workaround.

#### Scenario: Baymax contract failure is recorded
- **WHEN** a deterministic Baymax probe proves that a required runtime contract is missing or incorrect
- **THEN** the system SHALL append an actionable record to `docs/upstream/baymax-findings.md` and mark the affected probe and roadmap tasks `blocked-upstream`

#### Scenario: Stele contract failure is recorded
- **WHEN** a deterministic Stele probe proves that a required context or idempotency contract is missing or incorrect
- **THEN** the system SHALL append an actionable record to `docs/upstream/stele-findings.md` and mark the affected probe and roadmap tasks `blocked-upstream`

#### Scenario: Verified repair unblocks a finding
- **WHEN** a pinned upstream repair is available for a recorded finding
- **THEN** the system SHALL rerun the recorded reproduction and affected contracts before marking the finding `fixed-upstream` and removing the dependent block
