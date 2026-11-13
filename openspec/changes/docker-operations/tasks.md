# Tasks: docker-operations

## 1. Container image

- [ ] 1.1 Add `.dockerignore` excluding `web/node_modules`, `web/dist`, `.git`, `.workbuddy`, `data`, and local caches.
- [ ] 1.2 Add a multi-stage `Dockerfile`: pinned Node stage builds `web/dist`, pinned Go stage builds `cmd/personal-agent` with `CGO_ENABLED=0`, minimal runtime stage with a non-root user and no toolchain.
- [ ] 1.3 Verify the runtime image contains the workbench assets and no source tree, Node.js, or Python.

## 2. Compose profiles and networking

- [ ] 2.1 Add root `compose.yaml` with a default SQLite `app` service and mounted `danny_data`, `danny_documents`, and `danny_artifacts` volumes.
- [ ] 2.2 Bind every published host port to `127.0.0.1` and publish no parser or database port.
- [ ] 2.3 Add the `postgres` profile: pinned image, named volume, `pg_isready` health check, and `service_healthy` startup gating; wire `DANNY_STORAGE_DRIVER` and `DANNY_DATABASE_URL`.
- [ ] 2.4 Reuse the Milestone 7 parser service under the `parser` profile without publishing a host port; keep `compose.parser.yaml` and its documentation intact.
- [ ] 2.5 Add `.env.example` for the OpenAI-compatible endpoint, model name, credentials, storage, timezone, parser URL, and profile flags; commit no secret.

## 3. Explicit container listener boundary (Go)

- [ ] 3.1 Add `DANNY_CONTAINER_LISTEN` (default false) to `internal/config` and `security.ValidateContainerListener`, accepting only wildcard addresses when explicitly enabled.
- [ ] 3.2 Preserve the default listener policy: with the flag off, a non-loopback address is still rejected; the flag never relies on `DANNY_AUTHENTICATED`.
- [ ] 3.3 Add config/security tests for default rejection, opt-in wildcard acceptance, and concrete-IP rejection.
- [ ] 3.4 Add a test that reads `compose.yaml` and fails on any unbound or wildcard host port publication.

## 4. Health and startup reconciliation

- [ ] 4.1 Add `/api/health/live` (process liveness) and `/api/health/ready` (real `storage.Ping`, artifact directory check, typed 503 on failure) without changing `/api/health`.
- [ ] 4.2 Wire readiness into `cmd/personal-agent` and use it for the Compose health check.
- [ ] 4.3 Add server tests for ready/live semantics, fail-closed missing dependencies, and a failing storage ping.
- [ ] 4.4 Document that startup already performs `Open -> Migrate -> Reconcile` and that derived artifacts rebuild from committed generations.

## 5. Operations documentation

- [ ] 5.1 Add `docs/docker-operations.md`: one-command startup, profile matrix, health endpoints, startup sequence, loopback threat model, backup/export of database/manifests/sources/Stele data, and a recovery smoke procedure.
- [ ] 5.2 State the worker-process gap explicitly instead of claiming separate scheduler/mailbox/materializer/delivery containers.
- [ ] 5.3 Update `docs/roadmap.md` Milestone 11 status with an honest verification record.

## 6. Verification

- [ ] 6.1 `docker compose config` for default, `postgres`, and `parser` profiles; inspect normalized output for host bindings.
- [ ] 6.2 Build the application image and, when the daemon and images allow, run the default profile, poll readiness, restart, and confirm recovery.
- [ ] 6.3 `gofmt -l`, `go build ./...`, `go vet ./...`, `go test ./... -count=1`, focused `golangci-lint`, `openspec validate --all --strict`, `git diff --check`.
- [ ] 6.4 Record every check that could not be executed as unverified in `docs/docker-operations.md`.
