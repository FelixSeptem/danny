# Design: docker-operations

## Container build

Three stages, with base images supplied as build arguments so an operator can
pin a mirror or a digest:

1. `node` stage: install the pinned `web/package-lock.json` dependencies and
   run `npm run build` to produce `web/dist`.
2. `go` stage: `CGO_ENABLED=0 GOOS=linux go build ./cmd/personal-agent`.
   Both storage drivers are pure Go, so no C toolchain is required.
3. runtime stage: a minimal image containing only the static binary,
   `web/dist`, CA certificates, and the data directories. It runs as a
   non-root user and contains neither a toolchain nor the source tree.

`.dockerignore` trims the build context so `web/node_modules`, `web/dist`,
`.git`, `.workbuddy`, `data`, and local caches never enter an image layer.
The workbench assets are produced inside the build, not copied from the
host, so no host-side Node.js is needed.

## Listener boundary (the central trade-off)

`security.ValidateListener` rejects a non-loopback address in the default
(unauthenticated) profile. Inside a container, binding `127.0.0.1:8080`
makes a published host port useless: Docker forwards the published port to
the container's network interface, not its loopback. Relaxing the check with
`DANNY_AUTHENTICATED=true` is not acceptable because no authentication is
implemented behind that flag.

Resolution: an explicit, fail-closed opt-in.

- `DANNY_CONTAINER_LISTEN` defaults to false. With it off, the existing
  policy is unchanged and a non-loopback address is still rejected.
- When explicitly true, `security.ValidateContainerListener` accepts only
  the wildcard addresses `0.0.0.0` / `[::]` and rejects a concrete
  non-loopback IP. It never accepts an arbitrary host string.
- The trust boundary moves from "the process binds loopback" to "Compose
  publishes only to the host loopback". `compose.yaml` binds every host port
  as `127.0.0.1:<host>:<container>`, and a Go test reads the Compose file
  and fails on any unbound, wildcard, or non-loopback host publication.

This keeps the Milestone 10 requirement true for the default profile while
making a documented container profile possible without a false
authentication claim.

## Compose topology

`compose.yaml` defines:

- `app`: the assembled service, active in the default (SQLite) profile. A
  health check polls `/api/health/ready`. Volumes: `danny_data`,
  `danny_documents`, `danny_artifacts`.
- `postgres`: `profiles: [postgres]`, pinned image, named data volume, and a
  `pg_isready` health check. In that profile the app declares
  `depends_on: {postgres: {condition: service_healthy}}` so migrations never
  race an unready database. No database port is published.
- `parser`: the Milestone 7 parser service, reachable only on the internal
  network at `http://parser:8090` (the existing `DANNY_PARSER_URL` default).
  No host port is published and its existing health check is preserved.

Profile selection is additive: `--profile postgres` selects the PostgreSQL
topology, `--profile parser` adds the parser. The default project starts only
`app` with SQLite. `compose.parser.yaml` remains the standalone parser-only
project referenced by `docs/parser-service.md`.

## Startup, health, and reconciliation

`cmd/personal-agent` already performs `Open -> Migrate -> Reconcile` before
listening. `Reconcile` releases expired outbox leases and rebuilds derived
artifacts from committed generations. That is the recovery mechanism the
Milestone 11 gate asks about: deleting the derived artifact volume and
restarting rebuilds it. This change makes that behavior observable instead
of reimplementing it.

- `/api/health`: unchanged static boundary identifier.
- `/api/health/live`: process liveness, no dependency checks.
- `/api/health/ready`: calls `storage.Ping`, verifies the artifact directory
  is usable, and reports the storage driver. A failing or missing storage
  dependency returns a typed 503 rather than an empty success.

The Compose health check uses readiness, so `depends_on: service_healthy`
and restart policies reflect real database reachability.

## Configuration and secrets

`.env.example` is the single documented contract and contains no real
credentials. Model endpoint, model name, API key, storage driver, database
URL, timezone, parser URL, and profile flags are injected as environment
variables. `.env` is already ignored by `.gitignore`. The image contains no
secret and no model credential.

## Backup, export, and recovery

`docs/docker-operations.md` documents the SQLite database file copy while
the service is stopped, PostgreSQL `pg_dump`, the documents and staging
volumes, committed generation manifests, and the derived artifact volume.
It states that derived artifacts are rebuildable from committed generations
and are not part of a required backup. It includes a recovery smoke
procedure: delete the derived artifacts, restart, and confirm readiness
plus rebuild.

## Honest scope on workers

The roadmap lists separate app, scheduler, mailbox/outbox, materializer,
and delivery worker processes "where required by the runtime contract". The
current runtime exposes those units as `RunOnce` library calls with no
standalone command. This change therefore ships one `app` container and
documents the gap rather than adding inert containers. Recovery of pending
work is demonstrated through startup reconciliation and the existing
contract tests, not through a fabricated worker process.

## Verification strategy

- Static: `docker compose config` for the default, `postgres`, and `parser`
  profiles; inspect normalized output for host publication bindings;
  `git diff --check`.
- Build: build the application image when base images are reachable.
- Runtime: start the default profile, poll readiness, stop and restart the
  container, and confirm readiness recovers. Repeat with the `postgres`
  profile when its image is reachable.
- Go: `go build ./...`, `go vet ./...`, `go test ./... -count=1`, focused
  `golangci-lint`, and the Compose publication test.
- Any check that cannot run in this environment is recorded as unverified
  in `docs/docker-operations.md`. No claim is made for an unexecuted check.
