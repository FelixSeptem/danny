# http-api-workbench

## Why

Milestone 9 shipped the browser-facing protocol boundary: an AG-UI-compatible SSE execution projection with durable replay and reconnect, a restricted dynamic-surface catalog, and a server-validated A2UI v0.9.1 action boundary. That boundary is still unmounted. `cmd/personal-agent/main.go` validates configuration, opens and migrates storage, then prints one line and exits; no HTTP listener is ever created. `internal/server` exposes only the `/agent/` subtree behind an `AgentUIService`, and `web/` contains only `.gitkeep`. Milestone 10 of `docs/roadmap.md` requires a loopback-only workbench that exposes the verified domain capabilities through a local browser: streaming chat over the Milestone 9 projection, knowledge navigation, revision-aware task updates, schedule history, delivery status, and a run inspector. Without an assembled service boundary, every client would have to invent its own routes, own authorization assumptions, and its own version claims, which would duplicate Danny's commit gates and expose Baymax/Stele internals on the wire.

## What Changes

- Add a single assembled HTTP service in `internal/server`: one `Handler()` that mounts the existing Milestone 9 `/agent/` subtree unchanged, new read-only `/api/` resource projections, and the static workbench, with loopback-only binding enforced through `internal/security.ValidateListener`.
- Add a same-origin guard for every state-changing request (POST/PUT/PATCH/DELETE): requests without a matching `Origin` (or an accepted `Sec-Fetch-Site: same-origin` signal) are rejected with a typed error before any handler runs. The check is fail-closed and applies to the Milestone 9 cancel and surface-action endpoints too.
- Add a read-only projection repository inside `internal/storage` (the only package that can use the driver-neutral query binder) exposing bounded lists and detail views for tasks, schedules and occurrences, webhook deliveries and notifications, knowledge generations/artifacts/sources, and execution inspector records. Projections SHALL carry Danny-owned fields only; no Baymax or Stele internal type appears on the wire.
- Expose task mutation as a revision-aware endpoint that requires `expected_revision`, maps `storage.ErrRevisionConflict` to a typed HTTP 409 conflict, and never overwrites newer state. All other mutations continue to go through Danny's existing `idempotency_effects` and authorization boundaries; the workbench gains no write authority of its own.
- Add a React + TypeScript + Vite workbench under `web/` that consumes the Milestone 9 AG-UI SSE projection (with `Last-Event-ID` reconnect), renders only server-registered A2UI v0.9.1 surfaces, submits actions through the existing server-validated endpoint, displays typed rejections and revision conflicts, and never constructs or executes a mutation from browser-provided component data.
- Add contract tests for routing, same-origin rejection, loopback binding, typed errors, projection field allowlists, revision conflicts, SSE passthrough, and static asset handling.
- Keep A2UI v0.9.1 as the only claimed dynamic-surface version. The `a2ui-1-0-upgrade` change stays BLOCKED; no workbench claim depends on A2UI 1.0 or an unverified React renderer.

## Capabilities

### New Capabilities

- `http-api-workbench`: a loopback-only HTTP service boundary that assembles the Milestone 9 agent protocol, read-only Danny-owned product projections, revision-aware task updates with typed conflicts, same-origin state-changing protection, and a browser workbench that consumes projections without bypassing Danny gates.

### Modified Capabilities

- None. The `agent-ui-protocol-boundary` specification is not changed; this change mounts and consumes it.

## Impact

- New code: `internal/server` (assembled handler, same-origin guard, `/api/` resource handlers, static files), `internal/storage` (read-only projection repository), `cmd/personal-agent/main.go` (listen and serve), `web/` (React + TypeScript + Vite client).
- New docs: `docs/http-workbench.md` (route contract, security model, projection field allowlists, verification evidence), and a Milestone 10 status line in `docs/roadmap.md`.
- Storage: read-only queries only; no schema migration, no table changes, no new write paths.
- Dependencies: Go side unchanged. The workbench adds a pinned front-end toolchain under `web/`; the Go service serves built assets and does not require Node.js at runtime.
- Security: the default loopback binding and unauthenticated MVP posture are preserved. Non-loopback binding remains rejected until an authenticated mode exists. No existing authorization, revision, idempotency, or commit-gate semantics are weakened.
- Out of scope: full OpenKB visual design, live renderer certification, browser end-to-end automation, Docker profiles, and the release gate, which remain in Milestones 11 and 12.
