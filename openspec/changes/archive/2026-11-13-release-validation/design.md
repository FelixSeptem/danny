# Design: release-validation

## Scenario registry

`internal/scenarios` owns a registry. Each entry has a stable identifier
(`S01` … `S16`), the roadmap scenario it proves, a classification, the
executable check that proves it, and where the evidence lives.

| Class | Meaning |
| --- | --- |
| `executed` | A deterministic check runs in this repository with no credential and no upstream service. |
| `live-required` | The scenario needs a real provider credential, a live Stele deployment, or a live Baymax endpoint. The command and required environment are recorded; it is never reported as passing without real evidence. |
| `blocked-upstream` | The scenario depends on an upstream contract that is not yet available. |
| `not-executable` | The scenario cannot be automated as written; the reason is recorded. |

A registry invariant test fails when a roadmap scenario has no entry, when an
entry has an empty reason, or when an `executed` entry has no check. This is
what keeps the gate from drifting away from the roadmap.

## Scenario map

| # | Roadmap scenario | Class | Proof |
| --- | --- | --- | --- |
| S01 | Import two Markdown documents and answer a cited question | `executed` | Deterministic compile over the checked-in fixtures, then citation validation against the committed generation. |
| S02 | Save a personal preference to Stele and retrieve it in a new conversation | `live-required` | `cmd/runtime-probe stele-smoke` with a live deployment and credential. |
| S03 | Create, update, and complete a task across conversations with revision checks | `executed` | Task repository sequence with a stale `expected_revision` rejected and the newer state preserved. |
| S04 | Run a daily natural-language summary schedule and save the result to memory | `executed` | Occurrence calculation, deterministic dispatch, effect idempotency, and the stored result. |
| S05 | Execute a fixed reminder with a failing Webhook and verify independent retry status | `executed` | Failing delivery keeps the task status unchanged and produces an independent retry state. |
| S06 | Compile a multi-section document using subagents, parallel team aggregation, citation validation, and atomic generation commit | `executed` | Deterministic compile executor: fanout, aggregate, vote, citation check, and a single committed generation. |
| S07 | Trigger dynamic planner choices for direct, subagent, team, workflow, mailbox, and scheduled modes | `executed` | Planner decisions per mode, including the modes that must be refused. |
| S08 | Pause and resume a run from a harness checkpoint | `executed` | Harness pause, checkpoint commit, and resume from a reopened store. |
| S09 | Crash at every documented outbox, lease, materialization, and webhook boundary and verify recovery | `executed` | Injected failure per boundary, then store reopen and reconciliation assertions. |
| S10 | Run the full suite in SQLite and PostgreSQL modes | `executed` | `cmd/release-gate` runs the suite against both drivers; PostgreSQL runs through the Milestone 11 Compose override. |
| S11 | Deterministic fake-model contract tests and an OpenAI-compatible live smoke test | `executed` + `live-required` | `internal/deterministic` doubles drive the contract suite; the live half is `cmd/runtime-probe live-smoke`. |
| S12 | Import representative PDF, DOCX, PPTX, XLSX, XLS, HTML, CSV, TXT, and Markdown files through the parser service | `executed` | Parser format matrix over deterministic samples, asserting canonical output or a structured failure. |
| S13 | Verify active upstream findings have regression fixtures and blocked tasks stay blocked | `executed` | Finding-to-fixture linkage check over the upstream findings documents. |
| S14 | Reconnect during direct, child, team, workflow, mailbox, and scheduled runs; verify the AG-UI projection converges with the durable execution record | `executed` | SSE reconnect with `Last-Event-ID` per run mode, compared against the durable execution record and terminal result. |
| S15 | Render a restricted A2UI v0.9.1 form and interaction card; verify stale, replayed, unauthorized, malformed, and untrusted submissions cannot create an effect or expand authority | `executed` | Surface submission matrix over the server gate. |
| S16 | Verify Baymax execution recovery and Stele bounded context/restart behavior through the Milestone 8 contracts | `live-required` + `executed` | `cmd/runtime-probe live-pause-restore` for the live half; the recorded evidence bundles and the restart contract tests for the deterministic half. |

## Deterministic doubles

The fakes live in `internal/deterministic` as production-shaped code, not
test-only helpers, so the scenario suite and any future consumer share one
implementation. They implement the same interfaces the production code
consumes:

- `agent/knowledge.Executor` (`Run`, `RunChild`) and the planner interface,
- `knowledge.CompileExecutor` (`Fanout`, `Aggregate`, `Vote`, `Commit`).

Each double is scripted: a caller supplies the response for the next call and
the double records what it received. The suite asserts on the recorded calls
(plan digest propagation, capability digest, citation payloads, commit
ordering) rather than on model output, which keeps the assertions stable.

The doubles never touch a network. Any attempt to reach a provider is a test
failure, not a silent fallback.

## Crash recovery

Crash-recovery checks follow one shape: build the pre-crash state, inject the
failure at the named boundary, close the store without a graceful shutdown
path, reopen a fresh store over the same data directory, and assert that
reconciliation restored the invariant (no expired lease stays owned, pending
work is available again, derived artifacts exist for committed generations).
Reopening a second store instance is what makes this a durability claim
rather than an in-process assertion.

The storage driver runs the suite in both modes; SQLite uses a file database
so a reopen is meaningful, and PostgreSQL uses the Compose override.

## Browser boundary

The browser is a consumer of the AG-UI projection, the `/api/` projections,
and the surface submission endpoint. The convergence checks therefore drive
those interfaces directly with `Last-Event-ID` reconnects, retries, and
restarts, and compare the observed projection against the durable execution
record and the terminal result. This proves agreement across the four
boundaries the release gate names. A real browser run is recorded as a
separate, explicitly unverified step in the evidence matrix.

## Upstream findings

`docs/upstream/baymax-findings.md` and `docs/upstream/stele-findings.md`
are the source of truth for finding state. The linkage check parses the
finding entries, and for each one asserts that a regression fixture or
evidence bundle exists and that the entry records a version, a reproduction,
an evidence location, the affected scenario, and an unblock condition.
Findings whose state is `blocked-upstream` are reported as blocking and are
excluded from the claimed feature set; the gate fails if a blocked finding is
inside a claimed scenario.

## Release gate command

`cmd/release-gate` runs, in order: the Go suite, the parser matrix, and the
storage-mode matrix. It writes a machine-readable result file and renders
the evidence matrix into `docs/release-gate.md`. Checks that cannot run in
the current environment (no credential, no daemon, no upstream) are recorded
as unverified with the reason, never as passing.

## Verification strategy

- `go build ./...`, `go vet ./...`, `go test ./... -count=1`, focused
  `golangci-lint`.
- `cmd/release-gate` executed end to end, including the PostgreSQL mode when
  the daemon is reachable.
- Parser matrix executed with the Python toolchain.
- `openspec validate --all --strict`, `git diff --check`.
- Anything not executed is listed as unverified in `docs/release-gate.md`.
