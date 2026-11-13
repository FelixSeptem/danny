# Tasks: http-api-workbench

## 1. Read-only projection repository

- [ ] 1.1 Add `internal/storage/projections.go` with explicit Danny-owned projection structs (JSON allowlists) for tasks, schedules, occurrences, webhook deliveries, notifications, knowledge generations, artifacts, source metadata, and execution inspector records; verify no projection carries raw payload blobs, Baymax event types, Stele envelopes, or native identities.
- [ ] 1.2 Implement `Store.Projections()` with bounded, ordered list and detail methods using the driver-neutral binder; verify `ErrNotFound` propagates for unknown IDs and every list is capped and deterministic on SQLite.
- [ ] 1.3 Add `internal/storage/projections_test.go` covering field allowlists, bounded lists, not-found behavior, and ordering on an in-memory SQLite store.

## 2. Assembled HTTP service and same-origin guard

- [ ] 2.1 Add `internal/server/http.go` with a `Server` value whose `Handler()` dispatches `/agent/` to the existing `AgentUIService`, `/api/` to resource handlers, and the rest to static assets; verify zero-value dependencies fail closed with typed 503 responses.
- [ ] 2.2 Implement the fail-closed same-origin guard wrapping the whole handler; verify every state-changing method without a matching `Origin` or `Sec-Fetch-Site: same-origin` is rejected with `same_origin_required` before a handler runs, including the Milestone 9 cancel and surface-action endpoints, and reads pass through.
- [ ] 2.3 Add static asset serving with SPA fallback and a built-in placeholder when the asset directory is absent; verify `go build ./...` and `go test ./...` do not require the Node toolchain.

## 3. Resource endpoints and revision-aware task update

- [ ] 3.1 Implement read-only `/api/` handlers for tasks, schedules/occurrences, deliveries/notifications, knowledge (generations, artifacts, sources, index status), health, and the run inspector; verify typed 404 for unknown IDs and JSON field allowlists on every response.
- [ ] 3.2 Implement `PATCH /api/tasks/{id}` requiring `expected_revision` and delegating to `TaskRepository.Update`; verify stale revisions return 409 `task_revision_conflict` without mutation, malformed revisions return 400, and a current revision commits and returns the updated projection.
- [ ] 3.3 Verify no endpoint outside the task update and the Milestone 9 control endpoints performs a mutation.

## 4. Service entry point and security

- [ ] 4.1 Wire `cmd/personal-agent/main.go` to validate the listener through `security.ValidateListener`, create the `http.Server` on the configured loopback address, and shut down gracefully on signal; verify build and existing config tests still pass.
- [ ] 4.2 Add `internal/server/http_test.go` covering routing, same-origin rejection on all state-changing methods, loopback validation, typed errors, SSE passthrough with unchanged framing, SPA fallback, and fail-closed missing dependencies.

## 5. Workbench client

- [ ] 5.1 Add `web/` React + TypeScript + Vite project (package.json, tsconfig, vite config, index.html) with pinned dependencies and build/type-check scripts; verify `.gitignore` excludes `node_modules` and `dist`.
- [ ] 5.2 Implement the AG-UI SSE consumer with `Last-Event-ID` reconnect, the `/api/` projection views, and typed-rejection display; verify the client pins A2UI v0.9.1 and contains no component-data-to-mutation mapping or catalog registration path.
- [ ] 5.3 Implement restricted surface rendering from the server-registered catalog and action submission through the existing server-validated endpoint with an idempotency key; verify a typed rejection is displayed without a local mutation.
- [ ] 5.4 Run the front-end type-check and production build and record the evidence.

## 6. Documentation, verification, and exit gate

- [ ] 6.1 Write `docs/http-workbench.md` with the route contract, same-origin security model, projection field allowlists, A2UI v0.9.1 version policy, and verification evidence locations.
- [ ] 6.2 Update `docs/roadmap.md` Milestone 10 status with what is implemented, what is build-verified only (no live browser), and the exit-gate items that remain in Milestones 11 and 12.
- [ ] 6.3 Run `go build ./...`, `go test ./... -count=1`, `gofmt`/`go vet`, `golangci-lint run ./...`, `openspec validate --all --strict`, and `git diff --check`; record evidence locations and verify no claim depends on A2UI 1.0 or a live browser.
- [ ] 6.4 Review the complete task/evidence matrix for proposal/spec/design/task consistency and verify all checkboxes are backed by acceptance evidence before requesting archive.
