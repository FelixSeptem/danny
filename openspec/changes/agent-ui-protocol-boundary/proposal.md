# agent-ui-protocol-boundary

## Why

Milestone 8 closed the runtime/context ownership gate: execution evidence, replay cursors, checkpoint bindings and authority are durable and protocol neutral. The browser-facing boundary, however, is still empty — `internal/server` is a stub and `web/` has no client. Milestone 9 of `docs/roadmap.md` requires a stable browser-facing projection for Agent execution (AG-UI) and controlled dynamic UI (A2UI v0.9.1) before the full HTTP workbench. Without it, any client work would couple to Baymax internals or fabricate a second source of truth for tasks, memory, schedules, and business side effects.

## What Changes

- Introduce a Danny-owned, versioned execution-event projection (`internal/agentui`) that maps durable execution records (admission, plans, steps, checkpoints, evidence, terminal outcomes, cancellation status) and verified Baymax-native evidence into an ordered AG-UI-compatible event stream. Baymax event types stay behind the adapter; clients never import Baymax packages or internal event structures.
- Add a durable `agent_ui_events` projection log keyed by execution identity with a monotonic observation sequence, idempotent native-event deduplication, and terminal markers, so replay and reconnect are served from storage instead of a live process.
- Add an SSE transport in `internal/server` (protocol-neutral core, AG-UI wire adapter) with reconnect cursors (`Last-Event-ID`), bounded replay after a cursor, refresh reconciliation via state/message snapshots on connect, typed terminal events, and cancellation-status projection.
- Introduce version-independent application models `DynamicSurface`, `SurfaceAction`, and `SurfaceSubmission` plus a restricted Danny catalog: only server-registered components and actions exist; model-generated component names, functions, and props are never executable by default.
- Add a server-side A2UI v0.9.1 adapter and action validator that validates every submission against execution ID, surface ID, surface revision, action ID, user activation, action schema, authorization policy, task revision, and an idempotency key, and routes accepted actions through Danny's existing commit gates — never around them.
- Treat labels, options, source excerpts, retrieved memory, and imported documents as untrusted data: an A2UI action cannot expand Baymax tool capabilities, create new catalog entries, or bypass Danny commit gates. Stale, replayed, unauthorized, malformed, and untrusted submissions fail with typed errors.
- Ship an A2UI v0.9.1 wire-compatible surface fixture (renderer-consumable JSON) and a compatibility note documenting v0.9.1 support, the v1.0 Candidate status, the Q4 2026 roadmap target, and upgrade conditions. No feature claims depend on A2UI v1.0 or an unverified React v1.0 renderer.

## Capabilities

### New Capabilities

- `agent-ui-protocol-boundary`: AG-UI-compatible execution event projection with durable replay/reconnect, restricted dynamic-surface catalog, and server-validated A2UI action submission.

## Impact

- New code: `internal/agentui` (projection models, AG-UI adapter, surface models, action validator), `internal/server` (SSE transport and surface endpoints), storage migrations and repositories for the event log and surfaces.
- Storage: forward-only migrations for `agent_ui_events` and dynamic-surface tables with SQLite/PostgreSQL parity, reusing the existing revision/idempotency infrastructure.
- Docs: `docs/agent-ui-protocols.md` (compatibility note), roadmap Milestone 9 status update.
- No changes to Baymax or Stele upstream pins; no execution, authorization, or commit-gate semantics move behind the new boundary. The React workbench itself stays in Milestone 10; this change ships the protocol boundary and fixtures only.
