# Tasks: agent-ui-protocol-boundary

## 1. Projection Models and AG-UI Adapter

- [x] 1.1 Add `internal/agentui` with the versioned `ProjectionEvent` envelope (execution/thread/run identity, monotonic sequence, typed kind, sanitized payload, terminal marker) and a kind taxonomy covering run lifecycle, steps, text/tool streams, child/team/workflow/mailbox observations, planner decisions, checkpoints, citations, cancellation status, and failures; verify round-trip fixtures reject unknown contract versions and unsanitized payloads.
- [x] 1.2 Implement the AG-UI wire renderer over the envelope: run-start/finish/error boundaries, step pairing, text and tool-call start/content/end streams, state snapshot/delta, documented `Custom` rendering for app-specific kinds (child/team/workflow/mailbox, checkpoints-as-interrupt, citations, cancellation status), and verify no Baymax package types appear in wire output.
- [x] 1.3 Derive the projection from durable execution records (admission, plans, steps, checkpoints, evidence, terminal outcomes) without fabricating evidence: verify every rendered lifecycle boundary corresponds to a stored record, pauses project as interrupts preserving reason and checkpoint reference, and unknown or missing records fail typed instead of emitting synthetic events.

## 2. Durable Event Log and Replay

- [x] 2.1 Add forward migrations for `agent_ui_events` with execution-scoped monotonic sequence, native-source/native-event deduplication unique key, terminal markers, and SQLite/PostgreSQL parity; verify repeated identical delivery is idempotent and conflicting content for the same identity returns a typed conflict without advancing the sequence.
- [x] 2.2 Implement bounded ordered replay after a cursor with typed expired/foreign/malformed/gap errors, replay that never starts or resumes an execution, and fresh-process replay parity on both drivers; verify the terminal marker closes the log and late events after the terminal are rejected.
- [x] 2.3 Implement refresh reconciliation snapshots (state and message snapshot derived from execution records) for reconnect, and verify a client with a stale cursor converges on the same terminal state through snapshot plus bounded replay without duplicate logical side effects.

## 3. SSE Transport and Run Control

- [x] 3.1 Grow `internal/server` with a transport-neutral event stream core and an SSE rendering (`text/event-stream`, event IDs equal to observation sequence, heartbeat, retry hint, typed terminal event, no silent close); verify contract tests cover headers, ordering, cursor replay via `Last-Event-ID`, and terminal framing.
- [x] 3.2 Expose run event streaming and cancellation endpoints that reuse `Harness.Cancel` with idempotency keys; verify cancellation status is projected into the stream, control failure is distinct from source terminal outcome, and unauthorized or foreign run access fails typed.

## 4. Dynamic Surfaces and A2UI v0.9.1 Boundary

- [x] 4.1 Add the version-independent `DynamicSurface`, `SurfaceAction`, and `SurfaceSubmission` models, the restricted server-registered catalog, and storage persistence with surface revision advancement; verify surfaces referencing unregistered components, functions, or props are rejected at creation and revisions advance monotonically.
- [x] 4.2 Implement the A2UI v0.9.1 wire adapter rendering approved surfaces to renderer-consumable JSON, and ship a wire-compatible fixture whose components and actions all resolve to registered catalog entries; verify the fixture validates and the application models survive an adapter-boundary round trip without wire-version leakage.
- [x] 4.3 Implement the single-place submission validator (execution ID, surface ID, surface revision, action ID, user activation, action schema, authorization policy, task revision, idempotency key) routing accepted actions through Danny's existing authorization and commit gates with `idempotency_effects` convergence; verify stale surface/task revisions, unknown actions, schema violations, missing activation, and unauthorized policies fail typed and replayed keys return the recorded result without re-execution.

## 5. Security, Evidence, and Documentation

- [x] 5.1 Add security tests proving imported or retrieved instruction content cannot register a catalog entry, create an action, expand Baymax tool capabilities, alter an authorization policy, or bypass a commit gate, with typed rejections and diagnostics; verify submission payloads claiming different scope or policy than the registered action are rejected.
- [x] 5.2 Run the full verification suite (`go build ./...`, `go test ./... -count=1`, `golangci-lint run ./...`, `openspec validate --all --strict`, `git diff --check`) and record evidence locations for the projection, replay, and validator contracts in `docs/agent-ui-protocols.md`.
- [x] 5.3 Write the compatibility note documenting AG-UI compatibility as implemented and fixture-verified, A2UI v0.9.1 as the only claimed dynamic-surface version, the v1.0 Candidate status and Q4 2026 roadmap target, the renderer status verified on the implementation date, and the upgrade conditions; verify no feature claim asserts A2UI v1.0 or React v1.0 renderer support.
- [x] 5.4 Update `docs/roadmap.md` Milestone 9 status with what is implemented, what is fixture-only until the Milestone 10 workbench, and the exit-gate items that remain open; verify the capability manifest and dependency pins are unchanged.

## 6. Exit Gate

- [x] 6.1 Verify the browser-client exit gate as far as the protocol boundary allows: a client reconnecting to a running execution converges on the same terminal state without duplicate logical side effects, projections cover Baymax execution modes without exposing Baymax internals, an approved surface renders from the fixture, and a valid action reaches Danny's authorization and idempotency boundary before any mutation; record any item that requires the Milestone 10 workbench as explicitly deferred rather than claimed.
- [x] 6.2 Run `openspec validate agent-ui-protocol-boundary --strict` and `git diff --check`, review the complete task/evidence matrix for proposal/spec/design/task consistency, and verify all implementation checkboxes are backed by acceptance evidence before requesting archive.
