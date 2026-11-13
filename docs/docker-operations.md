# Docker operations (Milestone 11)

Danny runs as one loopback-published application container. This document is
the operator contract for `compose.yaml`: build, topologies, health, data,
backup, and recovery.

## Prerequisites

- Docker Engine with the Compose plugin. This change was developed against
  Docker 29.7.2 and Compose v5.5.1.
- Copy `.env.example` to `.env` and fill in real values. `.env` is ignored by
  Git; no credential is baked into an image layer.

## Commands

| Topology | Command |
| --- | --- |
| SQLite (default) | `docker compose up -d --build` |
| PostgreSQL | `docker compose -f compose.yaml -f compose.postgres.yaml up -d --build` |
| Add the parser service | `docker compose --profile parser up -d --build` |
| Static configuration check | `docker compose -f compose.yaml -f compose.postgres.yaml config` |
| Stop (default) | `docker compose down` |
| Stop (PostgreSQL) | `docker compose -f compose.yaml -f compose.postgres.yaml down` |

The PostgreSQL topology is an override file, not a Compose profile flag.
Always pass both `-f` files when starting, inspecting, or stopping it; loading
only `compose.yaml` leaves the application on SQLite. The parser service is
included from the Milestone 7 `compose.parser.yaml`, so there is one parser
definition on the internal network; that standalone file remains usable as
documented in `docs/parser-service.md`.

## Topology matrix

| Topology | Services started | Storage driver | Host ports |
| --- | --- | --- | --- |
| Default | `app` | `sqlite` | `127.0.0.1:8080` |
| PostgreSQL override | `app`, `postgres` | `postgres` | `127.0.0.1:8080` |
| Parser profile | `app`, `parser` | `sqlite` | `127.0.0.1:8080` |

Neither the database nor the parser publishes a host port. The parser is
reachable only on the internal Compose network at `http://parser:8090`, which
is the `DANNY_PARSER_URL` default.

## Ports and the trust boundary

`security.ValidateListener` rejects a non-loopback listener in the default
profile, and that policy is unchanged. Inside a container, however, binding
`127.0.0.1` makes a published host port unreachable because Docker forwards
the publication to the container's network interface, not its loopback.

The container profile therefore uses an explicit opt-in:

- `DANNY_CONTAINER_LISTEN=true` plus `DANNY_LISTEN_ADDRESS=0.0.0.0:8080` are
  set for the container. `security.ValidateContainerListener` accepts only
  wildcard addresses and rejects a concrete non-loopback IP.
- `compose.yaml` publishes the port as `127.0.0.1:<host>:8080`. The Go test
  `internal/security/compose_publication_test.go` fails if any publication is
  unbound or bound to a wildcard host.
- The Milestone 10 same-origin guard still covers every state-changing
  request, including the `/agent/` cancel and surface-action endpoints.
- `DANNY_AUTHENTICATED` is deliberately **not** used to widen the listener:
  no authentication is implemented behind that flag, so using it would be a
  false security claim.

Do not run the image with a bare `-p 8080:8080`. That publishes to all host
interfaces and defeats the boundary this change establishes.

## Startup sequence and health

`cmd/personal-agent` performs, in order:

1. `config.LoadFromEnv` and `Validate` (including the listener policy).
2. `storage.Open`, then `Migrate` (schema v10).
3. `Reconcile`: release expired outbox leases and rebuild derived artifacts
   from committed `knowledge_generations`, then create the artifact directory.
4. `ListenAndServe` with graceful shutdown on SIGINT/SIGTERM.

| Endpoint | Meaning |
| --- | --- |
| `GET /api/health` | Unchanged Milestone 10 static boundary identifier. |
| `GET /api/health/live` | Process liveness; no dependency checks. |
| `GET /api/health/ready` | Real `storage.Ping`, artifact directory usability, and the configured `storage_driver`; a typed 503 on any failure. |

The Compose health check polls `/api/health/ready`. The PostgreSQL override
gates the application on `postgres: service_healthy` (`pg_isready`), so
migrations never race an unready database. Liveness staying up while
readiness fails is intentional: it keeps a dependency failure diagnosable
instead of making the container look dead.

## Volumes and data

| Volume | Container path | Contents | Backup required |
| --- | --- | --- | --- |
| `danny_data` | `/var/lib/danny/data` | SQLite database (`danny.db`) | Yes |
| `danny_documents` | `/var/lib/danny/documents` | Imported source documents | Yes |
| `danny_artifacts` | `/var/lib/danny/artifacts` | Derived, rebuildable artifacts | No |
| `danny_parser_staging`, `danny_parser_jobs` | parser service | Staging objects and parser jobs | Optional (in-flight jobs) |
| `danny_pgdata` | `/var/lib/postgresql/data` | PostgreSQL data (override only) | Yes |

Generation manifests, citations, and task/execution records live in the
database, so a database backup covers them. Derived artifacts are explicitly
rebuildable from committed generations via startup reconciliation and are not
part of a required backup.

## Backup and export

- **SQLite**: stop the service, then copy the database out of the volume, for
  example `docker run --rm -v danny_danny_data:/data -v "$PWD/backup:/backup" alpine cp /data/danny.db /backup/danny.db`.
- **PostgreSQL**: `docker compose -f compose.yaml -f compose.postgres.yaml exec postgres pg_dump -U danny danny > backup/danny.sql`.
- **Documents and staging**: copy the `danny_documents` and
  `danny_parser_staging` volumes with the same `docker run --rm -v ...` pattern.
- **Stele data**: Stele-owned context lives in the upstream Stele service.
  Danny keeps only bounded projections and references, covered by the Danny
  database backup. Export the upstream store through Stele's own contract
  (see `docs/stele-context-evidence.md` and `docs/runtime-context-ownership.md`);
  this Compose project does not own or back up that store.

## Recovery smoke procedure

1. Start the default topology and wait until `GET /api/health/ready` reports
   `status: ready` with `storage_driver: sqlite`.
2. Remove the contents of the derived artifact volume (or delete and recreate
   the volume), then restart the application container.
3. Startup reconciliation rebuilds artifacts from committed generations.
   Readiness must not succeed before reconciliation completes; a fresh
   `ready` response confirms recovery.
4. Repeat a stop/start with the volumes intact to confirm the database and
   documents survive a restart.

Expired outbox leases and pending work recovery are covered by the existing
`internal/storage` reconciliation contract tests; see the Known gaps section
for what is not covered.

## Known gaps (not claimed)

- **Separate worker processes.** The roadmap lists separate app, scheduler,
  mailbox/outbox, materializer, and delivery worker processes "where required
  by the runtime contract". The current runtime implements those units as
  library-level `RunOnce` functions (`WebhookWorker`, `MailboxWorker`,
  projection workers) with no standalone command entry point. This change
  ships one `app` container rather than adding inert containers, and the
  "restart each worker" exit gate cannot be executed literally. Recovery is
  covered by startup reconciliation and the existing contract tests.
- **PostgreSQL suites.** The full repository and end-to-end suites in
  PostgreSQL mode, crash injection at every outbox/lease/materialization/
  webhook boundary, and the release gate remain Milestone 12.
- **Authenticated mode, remote exposure, TLS, and reverse proxy** are out of
  scope. The default posture stays loopback-only and unauthenticated.
- **Browser end-to-end validation** of the workbench remains Milestone 12,
  consistent with the Milestone 10 status line.

## Verification record

Environment: Docker 29.7.2, Docker Compose v5.5.1, daemon 29.7.2 reachable;
Go 1.26.8; Windows host. All commands below were executed on 2026-11-13.

| Check | Command / evidence | Result |
| --- | --- | --- |
| Compose static configuration | `docker compose -f compose.yaml config --quiet`, `--profile parser`, and `-f compose.yaml -f compose.postgres.yaml config --quiet` | PASS for all three topologies |
| Normalized host bindings | `docker compose ... config --format json`, inspected per service | PASS: only `app` publishes, with `host_ip=127.0.0.1`; `postgres` and `parser` publish none |
| PostgreSQL override merge | normalized override output | PASS: `depends_on/postgres/condition=service_healthy`, `DANNY_STORAGE_DRIVER=postgres`, `postgres`-scheme URL |
| Application image build | `docker compose -f compose.yaml build` | PASS: three stages (Node workbench, `CGO_ENABLED=0` Go build, minimal runtime), image `danny/personal-agent:local`, ~2m21s |
| Runtime image contents | `docker run --rm --entrypoint sh danny/personal-agent:local -c ...` | PASS: user `danny` (uid 100), `/opt/danny/web/dist` assets present, `/src` absent, `node`/`npm`/`python`/`python3`/`go`/`gcc` all absent |
| Workbench build inside image | `npm run build` (Dockerfile stage 1) | PASS: `tsc --noEmit && vite build` emitted CSS and JS assets |
| One-command SQLite startup | `docker compose up -d --build` | PASS: `danny-app-1` reached `healthy` |
| Readiness / liveness | `GET /api/health/ready`, `/api/health/live`, `/api/health` | PASS: ready returned `{"status":"ready","storage_driver":"sqlite","artifacts_ready":true}`; live and the Milestone 10 static response are unchanged |
| Restart persistence | `docker compose restart app` | PASS: `danny.db` (344064 bytes) intact in `danny_data`, ready returned |
| Derived artifact recovery | delete `/var/lib/danny/artifacts/*`, restart | PASS: ready within ~2s and startup reconciliation ran. Caveat: the committed-generation set was empty, so this exercises the reconciliation path, not a rebuild with real generation content |
| PostgreSQL startup | `docker compose -f compose.yaml -f compose.postgres.yaml up -d` | PASS: `danny-postgres-1 Healthy` before `danny-app-1 Starting`; ready returned `storage_driver: postgres`; both containers healthy; database port not published |
| Go | `gofmt -l`, `go vet ./...`, `go build ./...`, `go test ./... -count=1` | PASS for every package, including the Compose publication test |
| Focused lint | `golangci-lint run ./internal/config/... ./internal/security/... ./internal/server/... ./cmd/personal-agent/...` | PASS: 0 issues |
| OpenSpec | `openspec validate --all --strict` | 12/12 PASS |
| Whitespace | `git diff --check` | PASS |
| Container cleanup | `docker compose ... down` | PASS: no `danny-*` containers left running |

A first build attempt failed because a redundant `# syntax=docker/dockerfile:1`
directive made BuildKit fetch a frontend image from an unreachable registry.
The directive was removed (the Dockerfile uses no BuildKit-specific syntax)
and the build passed. The failed attempt is recorded here rather than hidden.

### Unverified / deferred

| Item | Reason |
| --- | --- |
| Full repository and end-to-end suites in PostgreSQL mode | Milestone 12; only startup, migration, and readiness were verified here |
| Crash injection at every outbox/lease/materialization/webhook boundary | Milestone 12 |
| Artifact rebuild from real committed generations | The reconciliation path ran, but the test database held no committed generation; full rebuild is a Milestone 12 scenario |
| "Restart each worker" exit gate | Not executable: the runtime has no standalone worker process (see Known gaps) |
| Live-browser workbench E2E and renderer certification | Milestone 12 |
| A2UI 1.0 | `a2ui-1-0-upgrade` is BLOCKED pending a real `@a2ui/react` 1.0 release |
| Full-repository `golangci-lint run ./...` | Not run: this sandbox times out on a full lint (known local limitation recorded in Milestone 10). Focused lint on every changed package, `go vet`, and the full test suite cover the changed code |
