# agent-ui-protocol-boundary Specification (Delta)

## ADDED Requirements

### Requirement: Execution events project to a Danny-owned AG-UI-compatible stream

The system SHALL project durable execution records and verified Baymax-native evidence into an ordered, versioned, AG-UI-compatible event stream owned by Danny. Each event SHALL carry execution and thread/run identity, a monotonic observation sequence, a typed kind, a sanitized payload, and a terminal marker. The projection SHALL cover run lifecycle (start, finished, error, pause-as-interrupt), steps, text and tool-call streams, child runs, teams, workflows, mailbox state, planner decisions, checkpoints, citations, cancellation status, and failures. Baymax event types and internal event structures SHALL remain behind the adapter so no client imports them, and unknown kinds SHALL render as documented custom events instead of failing the stream.

#### Scenario: Run lifecycle maps to AG-UI boundaries

- **WHEN** an admitted execution advances from start through steps to a terminal outcome
- **THEN** the stream SHALL begin with a run-start event carrying thread and run identity, and end with exactly one terminal event whose result or error reflects the product-verified outcome

#### Scenario: Pause projects as interrupt, not terminal

- **WHEN** an execution pauses with `input_required`, `approval_required`, or `budget_exhausted`
- **THEN** the stream SHALL project an interrupt terminal that preserves the pause reason and checkpoint reference without claiming success or failure

#### Scenario: Native identity stays behind the adapter

- **WHEN** a client consumes the event stream
- **THEN** it SHALL see Danny correlation and typed projections only, with no Baymax package types or internal event structures on the wire

### Requirement: Reconnect and replay converge without duplicate side effects

The system SHALL persist the projection to a durable event log keyed by execution identity with idempotent native-event deduplication and a terminal marker, and SHALL serve bounded, ordered replay after a caller-supplied cursor. Reconnect SHALL honor a reconnect cursor (such as `Last-Event-ID`), SHALL emit a refresh-reconciliation snapshot derived from execution records before replaying the tail, and SHALL project cancellation status. Replaying SHALL NOT start, resume, or mutate an execution, and expired, foreign, malformed, or gap-affected cursors SHALL fail with typed errors.

#### Scenario: Offline client converges on the same terminal state

- **WHEN** a browser client reconnects to a running execution with a stale cursor and the execution later reaches a terminal outcome
- **THEN** the client SHALL converge on the same terminal state through snapshot plus bounded replay without re-triggering any logical side effect

#### Scenario: Replay is read-only and execution-scoped

- **WHEN** a cursor is replayed for a finished execution in a fresh process
- **THEN** the same ordered retained events and terminal marker SHALL be returned without starting or resuming the execution, and a cursor bound to another execution SHALL be rejected with a typed scope error

### Requirement: Dynamic surfaces render only server-registered catalog entries

The system SHALL maintain version-independent application models `DynamicSurface`, `SurfaceAction`, and `SurfaceSubmission`, and a restricted Danny catalog containing only server-registered components and actions. A surface referencing an unregistered component, function, or prop SHALL be rejected at creation. An A2UI v0.9.1 adapter SHALL render approved surfaces to a wire-compatible format, and the application models SHALL remain independent of A2UI wire versions so a future adapter can be added without changing Agent runtime or business APIs.

#### Scenario: Model-generated component names are not executable

- **WHEN** model output or untrusted content references a component, function, or prop that is not in the registered catalog
- **THEN** surface creation SHALL reject it with a typed error and no renderer SHALL receive an executable definition for it

#### Scenario: Fixture renders with the approved catalog

- **WHEN** the shipped A2UI v0.9.1 wire-compatible fixture is validated
- **THEN** every referenced component and action SHALL resolve to a registered catalog entry

### Requirement: Action submissions validate at the authorization and idempotency boundary

The system SHALL validate every surface action submission against execution ID, surface ID, surface revision, action ID, user activation, action input schema, authorization policy, task revision, and an idempotency key before any mutation. Stale surface revisions, stale task revisions, unknown actions, schema-violating payloads, missing activations, and unauthorized policies SHALL fail with typed errors. A replayed idempotency key SHALL return the recorded result without re-executing, and accepted actions SHALL enter Danny's existing authorization and commit gates rather than bypassing them.

#### Scenario: Stale surface submission is rejected

- **WHEN** a submission carries a surface revision older than the current surface revision
- **THEN** the submission SHALL be rejected with a typed stale-surface error and no effect SHALL occur

#### Scenario: Replayed submission converges

- **WHEN** the same submission idempotency key is submitted twice
- **THEN** the second attempt SHALL return the recorded first result without executing the action again

#### Scenario: Accepted action passes the product gate

- **WHEN** a valid submission references an authorized action
- **THEN** the mutation SHALL be executed through Danny's existing authorization and commit gates with the task revision and effect identity preserved

### Requirement: Untrusted content cannot expand UI or execution authority

The system SHALL treat labels, options, source excerpts, retrieved memory, and imported documents appearing in or around dynamic surfaces as untrusted data. No such content SHALL register a catalog entry, create an action, expand Baymax tool capabilities, alter an authorization policy, or bypass a Danny commit gate, and attempts SHALL be rejected with typed errors and disclosed in diagnostics.

#### Scenario: Imported instructions cannot mint an action

- **WHEN** imported or retrieved instruction content attempts to add an action or component to a surface or catalog
- **THEN** the request SHALL be rejected as untrusted data and the catalog SHALL remain unchanged

#### Scenario: Untrusted payload cannot expand authority

- **WHEN** a submission payload claims a different write scope, tool capability, or policy than the surface action registered
- **THEN** the submission SHALL be rejected and the registered authority SHALL be preserved

### Requirement: Protocol version claims stay verified

The system SHALL document and enforce its protocol version policy: AG-UI compatibility as implemented and fixture-verified, A2UI v0.9.1 as the only claimed dynamic-surface version, and no claim of A2UI v1.0 support or React renderer v1.0 certification until the release and renderer status are verified. The compatibility note SHALL record the v1.0 Candidate status, the published Q4 2026 roadmap target, and the conditions for upgrading.

#### Scenario: No unverified version claim

- **WHEN** the compatibility note or any feature claim is reviewed
- **THEN** it SHALL NOT assert A2UI v1.0 support or renderer certification, and SHALL point to the verification conditions instead
