# Design: agent-ui-protocol-boundary

## Context

Milestone 8 left Danny with durable, protocol-neutral execution evidence: `task_harness_executions`/plans/checkpoints/evidence rows, replay cursors, checkpoint bindings, and a capability manifest that keeps Baymax behind an adapter. `internal/server` is still a stub and no browser-facing contract exists. AG-UI provides the execution-event vocabulary (RunStarted/RunFinished/RunError lifecycle, TextMessage*/ToolCall* streams, StateSnapshot/StateDelta sync, Custom for typed app extensions); A2UI v0.9.1 provides declarative forms and interaction cards rendered from a host-owned catalog. Neither protocol may become the source of truth for Danny product state.

## Goals / Non-Goals

**Goals:**

- One Danny-owned projection from durable execution records to an ordered, replayable event stream; AG-UI is a wire rendering, not the storage model.
- Reconnect and replay from any cursor without a live process; terminal convergence with no duplicate logical side effects.
- Dynamic UI that can only render server-registered components and submit server-registered actions, validated at Danny's authorization and idempotency boundary.

**Non-Goals:**

- The React workbench, its build pipeline, and visual design (Milestone 10).
- A2UI v1.0 support (Candidate, unverified renderer), MCP-UI/MCP Apps hosting, or any AG-UI client SDK dependency.
- Moving task, memory, schedule, or commit authority behind the UI boundary.

## Decisions

### D1. Storage model is Danny-owned; AG-UI is an adapter rendering

`internal/agentui` defines `ProjectionEvent` (execution identity, monotonic sequence, kind, sanitized payload, terminal marker) and an `AGUIRenderer` that maps kinds to AG-UI wire events. Kinds cover run lifecycle (`run_started`, `run_finished`, `run_error`), steps, text and tool-call streams, child/team/workflow/mailbox observations, planner decisions, checkpoints (`input_required`/`approval_required`/`budget_exhausted` as AG-UI run interrupts), citations, and cancellation status. Unknown kinds render as `Custom` with a documented name so old payloads never break the wire contract. Baymax native identity stays in the envelope; the wire event exposes only Danny correlation.

### D2. Durable event log, idempotent by native identity

Storage gains `agent_ui_events(execution_id, seq, native_source, native_event_id, kind, payload, terminal, created_at)` with `UNIQUE(execution_id, native_source, native_event_id)` matching the Milestone 8 dedup contract: same identity + equivalent content is idempotent, conflicting content is a typed conflict, seq advances only in the append transaction. A terminal row marks the run finished/error/interrupted; replay after a cursor is a bounded ordered page. Reconnect honors `Last-Event-ID`; on connect the server emits a refresh reconciliation snapshot (StateSnapshot + MessagesSnapshot derived from execution records) before replaying the tail, so a client that was offline converges without trusting its own cache.

### D3. SSE transport in `internal/server`, protocol-neutral core

The server package grows an SSE handler (`text/event-stream`) over a transport-neutral `EventStream` interface so a future WebSocket transport can share it. Endpoints: `GET /agent/threads/{threadId}/runs/{runId}/events` (stream + replay), `POST /agent/threads/{threadId}/runs/{runId}/cancel` (reuses `Harness.Cancel`), and surface endpoints under `/agent/surfaces`. Streams emit `retry`, event IDs equal to the observation sequence, a typed terminal event, and cancellation status — never a silent close. Heartbeats keep intermediaries from timing out.

### D4. Surfaces: version-independent models, restricted catalog, v0.9.1 adapter

`DynamicSurface` (id, execution ID, revision, catalog version, component tree, allowed actions) and `SurfaceAction` (id, input schema, authorization policy reference) are application models independent of the A2UI wire version. The Danny catalog is a server-registered allowlist; a surface referencing an unregistered component, function, or prop is rejected at creation. The A2UI v0.9.1 adapter renders surfaces to the wire format consumed by the React renderer and validates the shape of the shipped fixture; a future v1.0 adapter would be additive.

### D5. Submissions validate at the product boundary

`SurfaceSubmission` (execution ID, surface ID, surface revision, action ID, user activation, payload, task revision, idempotency key) is validated in one place: surface exists, revision current (stale → typed error), action in the surface's allowed set, payload conforms to the action schema, task revision current, authorization policy satisfied, idempotency key replays to the same result (via the existing `idempotency_effects` infrastructure). Accepted actions enter Danny's existing commit gates; the UI layer holds no authority of its own.

### D6. Untrusted data rules

Labels, options, source excerpts, retrieved memory, and imported documents flow into surfaces as data only. There is no path from model output to catalog registration, action creation, tool-capability expansion, or commit-gate bypass; tests prove a surface built around untrusted content cannot register a component, mint an action, or submit with expanded authority.

## Risks / Trade-offs

- **Fixture-only renderer evidence**: the A2UI React renderer is exercised through a wire-compatible fixture, not a live browser, until Milestone 10. The compatibility note states this explicitly.
- **SSE scope**: no auth server exists yet; endpoints are validated and typed but deployment hardening (TLS, origins) is deferred with the workbench.
- **Snapshot cost**: refresh reconciliation re-derives snapshots from execution records; acceptable at harness scale and bounded by replay pagination.

## Migration Plan

Forward-only storage migrations; no existing table changes. The server stub gains real handlers without changing any existing package's public behavior.

## Open Questions

- None blocking; renderer certification against the real React package is an explicit Milestone 10 task.
