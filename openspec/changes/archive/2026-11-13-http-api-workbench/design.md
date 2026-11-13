# Design: http-api-workbench

## Context

Milestone 9 delivered `internal/agentui` (AG-UI projection, A2UI v0.9.1 wire adapter, restricted catalog, submission validator) and `internal/server` (`AgentUIService.Handler()` exposing `/agent/threads/{tid}/runs/{rid}/events`, `/cancel`, and `/agent/surfaces/{sid}/actions`). Storage exposes per-ID repositories only (`Tasks().Get`, `Schedules().GetSchedule`, `Webhooks().GetDelivery`, `Executions().GetExecution`, `Generations().Committed`, etc.); there is no bounded list query and no product projection model. `internal/security.ValidateListener` already rejects non-loopback addresses unless authenticated mode is on, and `internal/config` supplies `ListenAddress`. `cmd/personal-agent/main.go` currently stops after `store.Reconcile`. `web/` is empty. The query binder `(*Store).bind` is package-private, so any new SQL must live in `internal/storage`.

## Goals / Non-Goals

**Goals:**

- One assembled `http.Handler` that mounts the Milestone 9 subtree without reimplementing it, adds read-only `/api/` projections, and serves the built workbench.
- A fail-closed same-origin guard on every state-changing request, including the Milestone 9 cancel and surface-action endpoints.
- Danny-owned projection types only; Baymax and Stele internals never cross the wire.
- A workbench that consumes the AG-UI SSE stream and server-registered A2UI v0.9.1 surfaces, and can display a revision conflict without ever bypassing a Danny gate.
- Deterministic contract tests over `httptest` for every route and guard.

**Non-Goals:**

- A2UI 1.0 support or React renderer certification (the `a2ui-1-0-upgrade` change is BLOCKED).
- Authentication, TLS, or non-loopback deployment.
- New mutation authority for the browser: only the revision-aware task update is added, and it goes through the existing repository.
- Full OpenKB visual design, browser end-to-end automation, Docker profiles, and the Milestone 12 release gate.
- Schema migrations, new tables, or changes to existing write paths.

## Decisions

### D1. One assembled handler, manual segment routing

`internal/server` gains a `Server` value with a `Handler()` that dispatches by path segment, matching the Milestone 9 decision to avoid `ServeMux` pattern semantics. The prefix `/agent/` delegates to `AgentUIService.Handler()` unchanged; `/api/` serves the new resource projections; everything else resolves to the static workbench (with an SPA fallback to `index.html`). Zero-value dependencies fail closed: a missing projection repository yields 503 on `/api/`, a missing source yields 503 on `/agent/.../events`.

### D2. Same-origin guard is a wrapper, not a per-handler check

A single middleware wraps the whole handler. For any method other than GET/HEAD/OPTIONS it requires either an `Origin` whose scheme/host matches the request `Host`, or `Sec-Fetch-Site: same-origin`. Requests that fail return a typed `same_origin_required` JSON error with HTTP 403 before reaching a handler. This closes cross-site state changes on a loopback service (DNS rebinding and hostile-origin form posts), including the Milestone 9 cancel and action endpoints, without adding per-endpoint duplication.

### D3. Read-only projection repository inside `internal/storage`

`store.Projections()` returns a `ProjectionRepository` with bounded list and detail methods. All SQL uses `s.bind()` for SQLite/PostgreSQL parity and `LIMIT` with a fixed cap. Every method returns projection structs declared in `internal/storage` with explicit JSON tags: an allowlist of Danny-owned fields. No method returns a raw domain row with `[]byte` payloads, native Baymax IDs, or Stele envelopes. Detail lookups map `ErrNotFound` to a typed 404 at the HTTP layer. The repository holds no write methods; the only mutation endpoint uses the existing `TaskRepository.Update` with `expected_revision`.

### D4. Revision-aware task update, typed conflict

`PATCH /api/tasks/{id}` requires a JSON body with `expected_revision` plus optional `title`, `description`, `status`. It calls `Tasks().Update(ctx, id, expectedRevision, TaskPatch)`. `storage.ErrRevisionConflict` maps to HTTP 409 with `task_revision_conflict`; a malformed or missing revision maps to 400. The handler never re-reads and overwrites, and no idempotency key is needed because the repository's conditional update is the authority.

### D5. Workbench is a consumer, not an authority

`web/` is a React + TypeScript + Vite SPA. It opens an `EventSource` on the Milestone 9 events endpoint (carrying `Last-Event-ID` on reconnect), renders the AG-UI event timeline, lists `/api/` projections, and renders dynamic surfaces only from the server-registered catalog. Action submission posts to `/agent/surfaces/{sid}/actions` with an idempotency key and displays typed rejections verbatim. The client contains no component-name-to-mutation mapping: browser-provided component data is data only, consistent with the Milestone 9 untrusted-content rule. A2UI wire version is pinned to v0.9.1 in the client contract, matching `agentui.A2UIWireVersion`.

### D6. Static assets are optional at runtime

The Go service serves built assets from a configurable directory. When the directory is absent, the service still starts and `/api/` + `/agent/` work; the root returns a small built-in placeholder instead of a 500. This keeps `go test ./...` and `go build ./...` independent of the Node toolchain while allowing `npm run build` to produce the real workbench.

## Risks / Trade-offs

- **Projection freshness**: list views are point-in-time reads of committed rows, not live streams. The workbench uses the AG-UI SSE projection for live execution state, so no false liveness is claimed from `/api/`.
- **Unauthenticated MVP**: loopback-only plus same-origin is not authentication. The roadmap keeps non-loopback deployment blocked until an authenticated mode exists; this change does not alter that policy.
- **Front-end build evidence**: the workbench is verified by a TypeScript type-check and a production Vite build, not by a live browser. Browser end-to-end scenarios remain in Milestone 12.
- **No schema change**: bounded lists use existing indexes; if a view later needs pagination at scale, that is a separate change.

## Migration Plan

No storage migration. `cmd/personal-agent/main.go` gains a listener and graceful shutdown after the existing startup sequence; existing configuration keys are unchanged. The new handler is additive to `internal/server`. Rollback is removing the listener and the new files.

## Open Questions

- None blocking. Pagination cursors for large installations and authenticated deployment are explicitly deferred.
