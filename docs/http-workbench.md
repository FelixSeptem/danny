# HTTP API and Web Workbench Boundary (Milestone 10)

This note records the route contract, security model, projection field
allowlists, A2UI version policy, and verification evidence for the
`http-api-workbench` change. It complements the Milestone 9 compatibility note
in `docs/agent-ui-protocols.md`.

## Assembled service

`cmd/personal-agent/main.go` validates configuration (including the loopback
listener policy), opens/migrates/reconciles storage, then serves one assembled
`server.Server.Handler()`. The handler dispatches by path segment:

| Prefix | Owner | Behavior |
|---|---|---|
| `/agent/` | `AgentUIService` (Milestone 9) | Mounted unchanged: SSE execution events, cancellation, surface-action submissions. |
| `/api/` | `Server` resource handlers | Read-only Danny-owned projections plus one revision-aware task update. |
| everything else | static workbench | Built assets from `DANNY_WORKBENCH_DIR` (default `web/dist`) with SPA fallback; a built-in placeholder when the directory is absent. |

Zero-value dependencies fail closed. In the current wiring, the run controller
(cancel) and the surface action executor are intentionally unconfigured because
their product authorization and commit gates are not yet wired; the
corresponding endpoints return a typed `unavailable` (503) rather than
executing without Danny authority. The read-only run source and the restricted
catalog are wired through `StorageRunSource`, `SurfaceService`, and
`agentui.DefaultCatalog()`.

## Security model

- **Loopback-only**: `internal/security.ValidateListener` is applied through
  `config.Config.Validate`. A non-loopback address is rejected in the default
  unauthenticated profile before any socket is opened. Authentication, TLS, and
  non-loopback deployment remain out of scope.
- **Same-origin guard**: a single wrapper rejects every state-changing request
  (POST, PUT, PATCH, DELETE) unless it carries an `Origin` whose scheme and host
  match the request host, or `Sec-Fetch-Site: same-origin`. Rejections are HTTP
  403 with `same_origin_required` and run before any handler, including the
  Milestone 9 cancel and surface-action endpoints. GET/HEAD/OPTIONS need no
  signal. This closes hostile-origin and DNS-rebinding form posts against a
  loopback service.
- **No new write authority**: the only new mutation is the revision-aware task
  update. All other mutations continue through Danny's existing
  authorization, `idempotency_effects`, and commit gates.

## Route contract

All responses are JSON with an explicit Danny-owned field allowlist. Errors use
`{"error": <code>, "message": <text>}`; codes include `same_origin_required`,
`unavailable`, `not_found`, `malformed`, `task_revision_conflict`,
`method_not_allowed`, and `projection_failed`.

| Method | Path | Response |
|---|---|---|
| GET | `/api/health` | dependency-free liveness |
| GET | `/api/tasks` | bounded task list |
| GET | `/api/tasks/{id}` | one task projection |
| PATCH | `/api/tasks/{id}` | requires `expected_revision`; 409 on stale revision |
| GET | `/api/schedules` | bounded schedule list |
| GET | `/api/schedules/{id}` | one schedule projection |
| GET | `/api/schedules/{id}/occurrences` | bounded execution history |
| GET | `/api/deliveries` | bounded webhook delivery list |
| GET | `/api/deliveries/{id}` | one delivery projection |
| GET | `/api/notifications` | bounded notification list |
| GET | `/api/knowledge/generations` | bounded generation list |
| GET | `/api/knowledge/artifacts?generation_id=N` | bounded artifact list |
| GET | `/api/knowledge/sources` | bounded source metadata list |
| GET | `/api/knowledge/status` | committed generation, artifact and source counts |
| GET | `/api/runs` | bounded execution list |
| GET | `/api/runs/{id}` | run inspector projection |
| GET | `/api/surfaces/{id}` | A2UI v0.9.1 wire rendering of a stored, catalog-approved surface |
| GET | `/agent/threads/{tid}/runs/{rid}/events` | Milestone 9 AG-UI SSE projection |
| POST | `/agent/threads/{tid}/runs/{rid}/cancel` | Milestone 9 control boundary |
| POST | `/agent/surfaces/{sid}/actions` | Milestone 9 server-validated submission |

Lists are bounded by `storage.ProjectionListLimit` (200) and ordered by
committed timestamps. They are point-in-time reads; live execution state comes
from the AG-UI SSE stream, not from polling these views.

## Projection field allowlists

Every projection type is declared in `internal/storage/projections.go` with
explicit JSON names. No projection selects raw payload blobs, native event IDs,
native checkpoint identities, effect keys, Stele envelopes, or Baymax event
structures. Representative allowlists:

- task: `id, title, description, status, revision, created_at, updated_at`
- schedule: `id, cron, timezone, action_type, instruction, enabled, retry_limit, misfire_policy, backfill_limit, revision, created_at, updated_at`
- delivery: `id, execution_id, occurrence_id, status, attempt_count, next_attempt_at, last_error, created_at, updated_at` (no URL or payload)
- artifact: `id, generation_id, kind, content_hash, created_at` (no content or lineage)
- source: `id, path, content_hash, format, parser_version, provider_mode, untrusted_source, created_at, updated_at` (no content or warnings)
- execution: `id, task_id, status, revision, mode, plan_version, parent_run_id, correlation_id, created_at, updated_at` (no digests, idempotency key, result, or failure blob)
- run inspector: the execution projection plus plan versions, a checkpoint summary, context-projection digests, and evidence/event counts. Native identities and raw results are intentionally absent.

The run inspector's `context_projections` carry only digests and a scope string.
They are Danny-owned context-projection references, not Stele records.

## Workbench client and version policy

`web/` is a React + TypeScript + Vite SPA. It consumes the Milestone 9 AG-UI
SSE projection through a native `EventSource` (the browser replays
`Last-Event-ID` on reconnect) and renders the `/api/` projections. Dynamic
surfaces are fetched from the read-only `/api/surfaces/{id}` endpoint, which
renders a stored, server-approved `DynamicSurface` through the Milestone 9
`RenderA2UI` adapter over `agentui.DefaultCatalog()`. The client renderer is a
read-only guard: it accepts only server-registered components (`Card`,
`ChoiceChips`, `Text`), rejects any other component or catalog with a typed
error, and treats props as untrusted text. Action submission posts to the
existing server-validated endpoint with an idempotency key and displays the
typed acceptance or rejection verbatim. No browser-provided component data is
mapped to a mutation, and the client cannot register a catalog entry.

**A2UI version claim: v0.9.1 only.** `agentui.A2UIWireVersion` is `0.9.1` and
the client pins the same value. A2UI 1.0 is NOT claimed. As verified on
2026-11-13, `@a2ui/react` is still `0.15.0`/v0.9 with no `v1_0` entry point, so
the `a2ui-1-0-upgrade` change is BLOCKED; this workbench does not depend on it.

## Verification evidence

| Check | Location | Result |
|---|---|---|
| `go build ./...` | repository root | passes |
| `go test ./... -count=1` | repository root | all packages pass |
| Same-origin guard (all state-changing methods, M9 control endpoints) | `internal/server/http_test.go` | passes |
| Projection field allowlists, bounded lists, typed not-found, inspector | `internal/storage/projections_test.go` | passes |
| Revision conflict (409) and fail-closed dependencies | `internal/server/http_test.go` | passes |
| Front-end TypeScript type-check and production build | `web/` | `tsc --noEmit` clean; `vite build` produced `web/dist` (34 modules) |
| `golangci-lint run` on the changed packages | `internal/server`, `internal/storage`, `internal/agentui`, `internal/config`, `cmd/personal-agent` | 0 issues. A full-repository `golangci-lint run ./...` exceeded the sandbox timeout (EXIT=124, a known local limitation); `go vet ./...` and the full `go test ./...` pass instead. |
| `openspec validate --all --strict` | repository root | 11/11 passes |

Browser end-to-end scenarios, a live renderer certification run, Docker
profiles, and the release gate are NOT claimed here. They remain in Milestones
11 and 12. Milestone 10 delivers the HTTP API boundary and a build-verified
workbench client; no claim asserts a live browser scenario or A2UI 1.0 support.
