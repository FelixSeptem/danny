## ADDED Requirements

### Requirement: Native execution projections have durable replay cursors

The storage layer SHALL persist versioned product projections of actual runtime evidence with execution identity, native source/event identity, source ordering when defined, application observation sequence, plan and authority correlation, and sanitized payload. Repeated delivery of the same native event SHALL be idempotent; conflicting content for that identity SHALL return a typed conflict. Projection persistence and application cursor advancement SHALL commit atomically on SQLite and PostgreSQL. Replay SHALL be bounded, ordered, read-only, execution-scoped, and independent of model/tool execution; expired, foreign, malformed, or missing-history cursors SHALL return typed errors without claiming complete replay.

#### Scenario: Overlapping delivery has one durable event
- **WHEN** two deliveries contain the same native event identity and equivalent normalized content
- **THEN** both drivers SHALL retain one projection and return the same observation sequence without duplicating a product effect

#### Scenario: Projection crash does not advance the cursor
- **WHEN** a failure occurs before the event projection and cursor transaction commits
- **THEN** the prior application cursor SHALL remain durable and reconciliation SHALL use verified source replay or report a typed history gap

#### Scenario: Replay survives process restart
- **WHEN** a fresh process receives a valid cursor for retained execution evidence
- **THEN** it SHALL return the same ordered retained projections and terminal evidence after that cursor without starting or resuming an execution

#### Scenario: Foreign cursor is rejected
- **WHEN** a caller supplies a cursor bound to another execution or scope
- **THEN** replay SHALL return a typed cursor/scope error and disclose no foreign evidence

#### Scenario: Conflicting native identity is rejected
- **WHEN** a repeated native event identity carries different normalized content
- **THEN** the repository SHALL preserve the existing evidence and return a typed evidence conflict without advancing the cursor

### Requirement: Runtime and context bindings preserve application authority

The storage layer SHALL persist versioned bindings between application authority records, native checkpoint references, source replay positions, and bounded context projection references or snapshots. Bindings SHALL retain execution identity, plan version/digest, capability digest, projection digest, task/schedule revision where applicable, write scope, effect namespace, completed effect keys, and native checkpoint identity/version/digest. Saving an application checkpoint and pause transition SHALL be one conditional transaction. Both drivers SHALL preserve existing committed executions and distinguish legacy application-only checkpoints from verified native recovery bindings. Cross-store reconciliation SHALL NOT invent a runtime checkpoint, advance an unconfirmed source position, or replay an uncertain mutation.

#### Scenario: Authority checkpoint and pause commit together
- **WHEN** a validated native checkpoint reference is bound to a paused application execution
- **THEN** the application checkpoint and pause transition SHALL either both commit with the expected revision or neither become visible after restart

#### Scenario: Missing upstream reference fails closed
- **WHEN** reconciliation finds an application recovery record whose native checkpoint or context evidence is missing or invalid
- **THEN** the execution SHALL remain recoverable only through a typed recovery-required or replan boundary and SHALL NOT silently restart tools

#### Scenario: Legacy record is not upgraded into native evidence
- **WHEN** a forward migration encounters an existing application-only checkpoint or synthesized probe event
- **THEN** the record SHALL remain readable with explicit legacy provenance and SHALL NOT be labeled as a verified native checkpoint or replayable native event

#### Scenario: Both drivers reject stale authority updates
- **WHEN** a stale worker attempts to replace a binding using an old expected execution revision
- **THEN** SQLite and PostgreSQL SHALL return the same typed revision conflict and preserve the newer binding, completed effects, and execution state
