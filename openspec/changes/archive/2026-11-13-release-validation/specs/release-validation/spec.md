# release-validation Specification (Delta)

## ADDED Requirements

### Requirement: Every required scenario is registered with a declared classification

The system SHALL maintain a machine-readable scenario registry in which every required roadmap scenario from S01 through S16 has exactly one entry with a stable identifier, a title, a classification of `executed`, `live-required`, `blocked-upstream`, or `not-executable`, a reference to the check that proves it, and the location of its evidence. A registry invariant test SHALL fail when a required scenario has no entry, when an entry has an empty reason or proof reference, or when an `executed` entry names no executable check. The registry SHALL be the single source for the release-gate report so the gate cannot drift from the roadmap.

#### Scenario: A missing scenario fails the registry

- **WHEN** the registry is checked and a required scenario identifier has no entry
- **THEN** the check SHALL fail and name the missing scenario identifier

#### Scenario: An executed scenario must name a real check

- **WHEN** an entry is classified `executed`
- **THEN** it SHALL reference an executable check, and an entry with an empty proof reference SHALL fail the registry

#### Scenario: A non-executable scenario stays visible

- **WHEN** a scenario cannot be executed
- **THEN** it SHALL still be registered with its classification and a non-empty reason, and SHALL NOT be removed from the registry

### Requirement: Deterministic doubles reproduce contract behaviour without a provider

The system SHALL provide reusable deterministic doubles, in production-shaped code rather than test-only helpers, that implement the planner, the `agent/knowledge.Executor` boundary, and the `knowledge.CompileExecutor` boundary. Each double SHALL be scripted by the caller, SHALL record the calls it received, and SHALL never reach a network provider. The contract suite SHALL assert on the recorded calls, covering plan digest propagation, capability digest, citation payloads, aggregate ordering, and single-commit behavior. A double that attempts a provider dial SHALL fail the check rather than fall back silently.

#### Scenario: A scripted run is reproducible

- **WHEN** the same scenario runs twice with the same script
- **THEN** it SHALL produce the same recorded calls and the same durable outcome

#### Scenario: A provider dial fails the check

- **WHEN** a double is asked to perform a provider call
- **THEN** the check SHALL fail with a typed error instead of contacting a provider

#### Scenario: Commit happens exactly once

- **WHEN** a compile scenario aggregates child results
- **THEN** the recorded calls SHALL show a single commit, and the durable state SHALL contain exactly one committed generation

### Requirement: Crash recovery is proven by reopening the store

The system SHALL prove crash recovery by injecting a failure at each documented outbox, lease, materialization, and webhook boundary, closing the store without a graceful shutdown path, opening a second store instance over the same data, and asserting that reconciliation restored the invariant: no expired lease remains owned, pending work is available again, and derived artifacts exist for every committed generation. The recovery checks SHALL run against both storage drivers, using a file-backed database for SQLite so that a reopen is meaningful.

#### Scenario: An expired lease is reclaimed after a crash

- **WHEN** a crash is injected while a lease is held and the store is reopened
- **THEN** the lease SHALL no longer be owned by the crashed worker and the pending work SHALL be available again

#### Scenario: Derived artifacts are rebuilt after a crash

- **WHEN** a crash is injected during materialization and the store is reopened
- **THEN** the derived artifacts for every committed generation SHALL exist again

### Requirement: The same suite runs against both storage drivers

The system SHALL run the scenario suite against SQLite and PostgreSQL and SHALL compare the results, so a behavior that depends on the driver is visible. PostgreSQL SHALL run through the Milestone 11 Compose override. A driver mode that cannot be executed SHALL be recorded as unverified with its reason rather than skipped silently, and no scenario SHALL be reported as passing on the strength of the other driver alone.

#### Scenario: Both drivers agree

- **WHEN** the suite runs against SQLite and PostgreSQL
- **THEN** every scenario that is classified `executed` SHALL have produced a result in both modes, and a mismatch SHALL fail the gate

#### Scenario: An unavailable driver is reported, not hidden

- **WHEN** a storage driver mode cannot run in the current environment
- **THEN** that mode SHALL be listed as unverified with the reason, and the other mode's result SHALL NOT be reported as covering it

### Requirement: The browser boundary converges across refresh, reconnect, retry, and restart

The system SHALL verify at the HTTP, SSE, and workbench contract layer that the execution projection, the dynamic surface actions, and the durable product state agree across refresh, reconnect, retry, and restart boundaries. Reconnect SHALL be exercised with `Last-Event-ID` in direct, child, team, workflow, mailbox, and scheduled run modes, and the observed projection SHALL be compared with the durable execution record and its terminal result. The surface submission matrix SHALL cover stale, replayed, unauthorized, malformed, and untrusted submissions, none of which SHALL create an effect or expand Agent authority. A real browser run SHALL be recorded as an explicitly unverified step and SHALL NOT be claimed.

#### Scenario: Reconnect converges with the durable record

- **WHEN** a client reconnects with `Last-Event-ID` during a run mode
- **THEN** the replayed projection SHALL match the durable execution record and SHALL end at the same terminal result

#### Scenario: Untrusted submissions create no effect

- **WHEN** a stale, replayed, unauthorized, malformed, or untrusted surface submission is sent
- **THEN** it SHALL create no effect and SHALL NOT widen Agent authority

#### Scenario: The real browser run is not claimed

- **WHEN** the evidence matrix is produced without a real browser run
- **THEN** the browser-rendered step SHALL be listed as unverified with its reason

### Requirement: Upstream findings stay linked to regression evidence

The system SHALL check the upstream findings documents and assert that every finding entry records a dependency version or commit, a deterministic reproduction, an evidence location, the affected scenario, and an explicit unblock condition, and that a regression fixture or evidence bundle exists for it. A finding whose state is `blocked-upstream` SHALL be reported as blocking and SHALL be excluded from the claimed feature set; the gate SHALL fail when a blocked finding sits inside a claimed scenario. The finding documents remain the source of truth for finding state.

#### Scenario: A finding missing a required field fails the linkage check

- **WHEN** a finding entry omits its version, reproduction, evidence, affected scenario, or unblock condition
- **THEN** the linkage check SHALL fail and name the finding

#### Scenario: A blocked finding cannot sit in a claimed scenario

- **WHEN** a `blocked-upstream` finding affects a scenario that the gate claims
- **THEN** the gate SHALL fail and report the finding as blocking

### Requirement: One command produces the release-gate evidence matrix

The system SHALL provide a single release-gate command that runs the Go suite, the parser format matrix, and the storage-mode matrix, collects the results, and writes a machine-readable result file together with a rendered evidence matrix covering all sixteen scenarios and all six release-gate clauses. A check that cannot run in the current environment SHALL be recorded as unverified with its reason and SHALL NOT be counted as passing, and no feature claim SHALL depend on an unexecuted check.

#### Scenario: The gate reports every scenario

- **WHEN** the release-gate command completes
- **THEN** every registered scenario SHALL appear in the evidence matrix with its classification, its result, and its evidence location

#### Scenario: An unexecuted check is labelled

- **WHEN** a check cannot run in the current environment
- **THEN** it SHALL appear as unverified with the reason, and SHALL NOT appear as passing
