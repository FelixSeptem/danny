# docker-operations

## Why

Milestone 10 shipped a single-process loopback-only HTTP service, but it only
runs on a prepared host: the operator must install Go, Node.js, and Python,
build the workbench assets, and start processes by hand. There is no
application image, no Compose project for the SQLite or PostgreSQL modes,
no one-command startup, and no backup or recovery procedure. Milestone 11
requires the validated local system to be reproducible in Docker with both
storage modes, with host ports unreachable beyond the local machine.

## What Changes

- Add a multi-stage `Dockerfile` that builds the React workbench and the Go
  service into one static image. Both storage drivers are pure Go
  (`modernc.org/sqlite`, `pgx`), so the runtime image needs neither cgo nor
  Node.js.
- Add a root `compose.yaml` with a default SQLite profile (mounted data,
  documents, and artifact directories) plus `compose.postgres.yaml`, an
  explicit override whose database service gates application startup on a
  health check.
- Keep the Milestone 7 `compose.parser.yaml` parser-only project intact and
  reuse its parser service on the internal network. No parser host port is
  published.
- Add an explicit, opt-in container listener mode (`DANNY_CONTAINER_LISTEN`)
  plus `security.ValidateContainerListener`. An in-container loopback bind is
  unreachable through a published port, while the default policy correctly
  rejects non-loopback addresses. Host publication is fixed to `127.0.0.1`
  and covered by a test that reads the Compose file.
- Add `/api/health/live` and `/api/health/ready`. Readiness performs a real
  storage `Ping` and reports the configured driver, so the PostgreSQL override
  and container health checks gate on actual database reachability instead of
  a static response. The existing `/api/health` stays unchanged.
- Add `.env.example` documenting the OpenAI-compatible endpoint, model name,
  credentials, storage selection, timezone, parser URL, and profile flags.
  No secret is baked into an image or committed to the repository.
- Add `docs/docker-operations.md`: one-command startup, profile matrix,
  health and startup reconciliation, backup/export of the database,
  manifests, source files, and derived artifacts, and recovery procedures.

## Capabilities

### New Capabilities

- `docker-operations`: reproducible container build and Compose operation of
  the Danny service in SQLite and PostgreSQL modes, with loopback-only host
  publication, health-gated startup, startup reconciliation, and documented
  backup/recovery.

### Modified Capabilities

- None. The `http-api-workbench` specification is not changed; the readiness
  endpoints extend the existing assembled handler, and the default listener
  policy is preserved for the default profile.

## Impact

- New files: `Dockerfile`, `.dockerignore`, `compose.yaml`, `.env.example`,
  `docs/docker-operations.md`.
- Go changes: `internal/config` (opt-in container listener flag),
  `internal/security` (container listener validator), `internal/server`
  (live/ready endpoints), `cmd/personal-agent` (readiness wiring), and tests.
- No schema migration and no change to authorization, revision, idempotency,
  or commit-gate semantics.
- A2UI remains v0.9.1 only; `a2ui-1-0-upgrade` stays BLOCKED.

## Non-goals

- Separate scheduler, mailbox/outbox, materializer, and delivery worker
  processes. The runtime currently implements these as library-level
  `RunOnce` units (`WebhookWorker`, `MailboxWorker`, projection workers) with
  no standalone command entry point. Inventing long-running containers for
  them would fabricate a runtime that does not exist. This change documents
  the gap instead of claiming it.
- Authenticated mode, remote exposure, TLS termination, or a reverse proxy.
  `DANNY_AUTHENTICATED` is not used to widen the listener: that flag has no
  authentication implementation behind it.
- The full repository and end-to-end suites in PostgreSQL mode, crash
  injection at every boundary, and the release gate, which remain
  Milestone 12.
