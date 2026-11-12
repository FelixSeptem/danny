## Purpose

This capability defines verifiable ownership and versioned product boundaries between Danny, Baymax execution, and Stele context so downstream clients can consume durable evidence without acquiring runtime or mutation authority.

## ADDED Requirements

### Requirement: Runtime and product authority have explicit owners

The system SHALL expose a versioned ownership contract under which Baymax owns execution, orchestration, runtime safe points, checkpoint restoration, retry, cancellation, timeout, and runtime terminal semantics; Stele owns scoped memory retrieval, ranking, memory context selection, provenance, and session/turn continuity; and Danny owns admission, plan selection, authorization, task revisions, effect keys, product commit gates, and cross-store durable intents. Danny SHALL retain role disclosure, untrusted-source classification, and the meaning of task and knowledge references. Consuming dependency content or events SHALL NOT grant mutation authority.

#### Scenario: Runtime success does not bypass product validation
- **WHEN** Baymax reports a successful run whose requested product mutation has a stale revision or invalid citation
- **THEN** Danny SHALL preserve the runtime success evidence and reject the product commit with a typed product error

#### Scenario: Context cannot redefine policy
- **WHEN** Stele context or a source excerpt requests new tools, a different write scope, or a different task authority
- **THEN** Danny SHALL treat the request as untrusted data and SHALL preserve the server-authorized capabilities

### Requirement: Execution forms require verified public contracts

The system SHALL provide a versioned execution boundary for `direct`, bounded `subagent_fanout`, `team_parallel`, `team_vote`, `workflow`, and `mailbox_async` using verified Baymax public execution and orchestration contracts. Every admitted form SHALL preserve execution and parent/child identities, plan version/digest, capability and projection digests, write scope, effect namespace, and resource limits. Scheduled admission SHALL additionally preserve the occurrence identity and schedule revision while delegating execution to a verified form. Unsupported or unverified forms SHALL return typed admission failures before model execution or tool effects and SHALL NOT silently downgrade to another form.

#### Scenario: Native team executes an admitted plan
- **WHEN** a bounded parallel or vote team plan is admitted with verified capabilities
- **THEN** Baymax SHALL execute its member assignments and aggregation, and Danny SHALL expose correlated member evidence and the native aggregate outcome within the authorized limits

#### Scenario: Workflow and mailbox preserve identity
- **WHEN** an admitted workflow or mailbox continuation advances a run
- **THEN** the execution evidence SHALL preserve its native step or message identity, causation, and the original product authority without admitting a second logical execution

#### Scenario: Required upstream capability is unavailable
- **WHEN** an execution form lacks a verified required public contract at the pinned version
- **THEN** admission SHALL return a typed unavailable result and the dependent implementation task SHALL remain blocked without emulation

### Requirement: Execution evidence is native and protocol neutral

The system SHALL project actual Baymax run, tool, child, team, workflow, mailbox, diagnostic, safe-point, and terminal evidence through a versioned product envelope. Each envelope SHALL distinguish native event identity and source ordering from application observation order and product correlation. Transport observer failure, cancellation request admission, and delivery outcomes SHALL remain distinct from the source-owned runtime terminal outcome. Production evidence SHALL NOT be fabricated from probe outcomes or inferred solely from error text.

#### Scenario: Tool failure retains source evidence
- **WHEN** Baymax emits a tool failure during an authorized execution
- **THEN** the product projection SHALL retain its native identity, failure category, run/step correlation, and authorized context references with sensitive payloads removed

#### Scenario: Observer disconnect is not execution failure
- **WHEN** an event observer disconnects while a run continues
- **THEN** the product SHALL expose observer disconnection separately and SHALL NOT manufacture an execution terminal event

#### Scenario: Cancellation uses the source outcome
- **WHEN** Danny authorizes a cancellation request and Baymax accepts it
- **THEN** the projection SHALL distinguish request acceptance from the eventual native terminal outcome and SHALL NOT report cancellation completion before source evidence exists

### Requirement: Context disclosure is scoped bounded and provenance carrying

The system SHALL expose a versioned context boundary carrying a server-selected scope, session/turn correlation, query, role/policy disclosure constraints, and budgets with explicit units. Stele SHALL perform memory retrieval, ranking, and budget selection through its verified public contract. Danny SHALL validate a bounded response containing authorized references, provenance, available selection diagnostics, source version or reproducible snapshot evidence, and an application projection digest before Baymax final context assembly. Undocumented fields SHALL NOT be treated as effective controls, and missing required scope, budget, provenance, or continuity guarantees SHALL produce a typed failure.

#### Scenario: Scoped memory enters an authorized role projection
- **WHEN** Stele returns bounded memory context for an authorized role and binding
- **THEN** the product projection SHALL preserve memory/source citations, validated scope and continuity evidence, explicit budget accounting, and trust labels without granting tools or copying the entire memory store

#### Scenario: Undocumented role hint is insufficient
- **WHEN** a role or policy restriction has no supported public Stele mapping
- **THEN** the system SHALL NOT claim that an arbitrary role field enforces disclosure and SHALL reject any request whose required disclosure cannot be enforced by verified controls

#### Scenario: Invalid response is not locally repaired
- **WHEN** a required context response exceeds its contract bounds, lacks required provenance, or contains a reference outside the authorized scope
- **THEN** the adapter SHALL return a typed contract failure before model execution and SHALL NOT recreate selection, ranking, or provenance locally

### Requirement: Compatibility claims require pinned evidence

Each execution, recovery, replay, and context capability claim SHALL reference a pinned dependency version, public contract, deterministic evidence, and applicable real runtime/service evidence. A confirmed defect or missing required upstream contract SHALL be recorded in the corresponding Chinese findings document with reproduction, expected/actual behavior, evidence, affected task, status, and unblock condition. Dependent tasks SHALL remain `blocked-upstream` until a pinned repair passes the original reproduction and affected integration checks. Unverified observations SHALL NOT be presented as confirmed upstream bugs or completed milestones.

#### Scenario: Missing public recovery contract blocks the task
- **WHEN** a required recovery guarantee cannot be verified through the pinned public runtime contract
- **THEN** the finding SHALL identify the affected task and exact unblock condition, and that task SHALL remain incomplete without a production workaround

#### Scenario: Upstream repair is verified before reopening work
- **WHEN** a pinned upstream update becomes available for a blocked contract
- **THEN** the original reproduction and affected integration checks SHALL be rerun before the finding moves to the resolved section and implementation resumes

#### Scenario: Browser protocol remains a later consumer
- **WHEN** Milestone 8 evidence is consumed by a later UI adapter
- **THEN** that consumer SHALL use the versioned product contracts without requiring Baymax or Stele internal structures or treating protocol data as authority
