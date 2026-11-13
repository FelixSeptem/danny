# release-validation

## Why

Milestone 11 made the validated local system reproducible in Docker, and the
roadmap's Milestone 12 requires proof that the verified domain capabilities
behave correctly in real user workflows: sixteen required scenarios and six
release-gate clauses. Today those proofs are scattered. Unit and contract
tests live inside packages, the fixture manifest verifies a handful of
checked-in files, and live upstream evidence is stored as sanitized bundles
in `testdata/runtimeprobe/`. Nothing ties a roadmap scenario to an executable
check, so "ready for the next development cycle" is an assertion rather than
a result, and the release gate cannot be evaluated at all.

## What Changes

- Add `internal/scenarios`: a scenario suite in which every required
  scenario has a stable identifier, a declared classification
  (`executed`, `live-required`, `blocked-upstream`, `not-executable`), and an
  executable check whenever the scenario can run without external
  credentials. The registry is machine-readable so the gate cannot drift
  from the roadmap.
- Add `internal/deterministic`: reusable deterministic fakes for the
  planner, execution, and compile-execution boundaries. Contract tests
  reproduce the same run every time without a model provider or an upstream
  service, and the fakes record the calls the contract requires.
- Add crash-recovery coverage that reopens the store after an injected
  failure at each documented outbox, lease, materialization, and webhook
  boundary, instead of asserting recovery only inside a single process.
- Run the same scenario suite against both storage drivers: SQLite locally
  and PostgreSQL through the Milestone 11 Compose override.
- Extend the parser service format matrix with deterministic sample
  documents and assert canonical output or a structured failure for every
  supported format.
- Add `cmd/release-gate`: one command that runs every suite, collects the
  results, and writes the evidence matrix, marking any check that could not
  run as unverified with its reason.
- Add `docs/release-gate.md`: the sixteen-scenario map, the six release-gate
  clauses, the upstream finding linkage, and an explicit list of what is not
  verified.

## Capabilities

### New Capabilities

- `release-validation`: a scenario suite with stable identifiers and
  declared classifications, deterministic contract-test doubles, crash
  recovery proven by store reopen, dual-storage execution of the same suite,
  a browser-boundary convergence check, upstream-finding regression linkage,
  and a single-command evidence matrix for the release gate.

### Modified Capabilities

- None. The release gate is an evidence producer; it changes no product
  behavior and no existing capability.

## Impact

- New packages: `internal/scenarios` (suite and registry),
  `internal/deterministic` (contract-test doubles).
- New command: `cmd/release-gate`.
- New files: `docs/release-gate.md`, parser sample documents and parser
  matrix checks.
- No schema migration, no new production write path, and no change to
  authorization, revision, idempotency, or commit-gate semantics.
- A2UI remains v0.9.1 only; `a2ui-1-0-upgrade` stays BLOCKED.

## Non-goals

- **Live upstream smoke tests are never faked.** Scenarios that need a real
  provider credential, a live Stele deployment, or a live Baymax endpoint
  stay classified as `live-required`, with the exact command and the required
  environment recorded. They are never reported as passing without real
  evidence, and they are not counted toward the claimed feature set.
- **No real-browser automation in this change.** The browser boundary is
  verified at the HTTP, SSE, and workbench contract layer across refresh,
  reconnect, retry, and restart; a live browser run stays an explicit
  separate step.
- **No production behavior changes.** The gate observes; it does not
  weaken, bypass, or re-implement any gate.
- **No scenario is silently dropped.** A scenario that cannot be executed
  is still registered with its classification and the reason, so the gap is
  visible rather than absent.
