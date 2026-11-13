# http-api-workbench Specification

## Purpose
Define Danny's loopback-only browser boundary: one assembled HTTP service that mounts the Milestone 9 AG-UI SSE projection and restricted A2UI v0.9.1 surface boundary unchanged, adds read-only Danny-owned `/api/` projections with explicit field allowlists, enforces a fail-closed same-origin guard on every state-changing request, and serves a React workbench that consumes those projections. The workbench is a projection, never a source of truth: no raw payloads, Baymax event types, Stele envelopes, or native identities cross the wire, and the only mutation it exposes is a revision-aware task update that passes through Danny's commit gates and returns typed conflicts. A2UI 1.0 stays unclaimed until a real React 1.0 renderer is verified.

## Requirements

### Requirement: One loopback-only service assembles the browser boundary

The system SHALL expose a single HTTP handler that mounts the Milestone 9 `/agent/` execution and surface endpoints unchanged, adds read-only `/api/` product projections, and serves the workbench static assets. The listener SHALL be validated by the existing loopback policy so a non-loopback address is rejected in the default (unauthenticated) profile. Missing dependencies SHALL fail closed with a typed service-unavailable error rather than fabricating a response.

#### Scenario: Default listener stays loopback-only

- **WHEN** the service starts with the default configuration in unauthenticated mode
- **THEN** the listen address SHALL pass the loopback validation policy, and a non-loopback address SHALL be rejected before any socket is opened

#### Scenario: Milestone 9 subtree is mounted, not reimplemented

- **WHEN** a client requests an `/agent/` execution-event, cancel, or surface-action endpoint through the assembled handler
- **THEN** the request SHALL be served by the existing `AgentUIService` projection and validation boundary with unchanged semantics

#### Scenario: Missing dependency fails closed

- **WHEN** the projection repository or the run source is not configured
- **THEN** the corresponding `/api/` or `/agent/` endpoint SHALL return a typed service-unavailable error and SHALL NOT emit an empty success response

### Requirement: State-changing requests require a same-origin signal

The system SHALL wrap the assembled handler in a fail-closed same-origin guard. Every state-changing request (POST, PUT, PATCH, DELETE) SHALL carry either an `Origin` whose scheme and host match the request host or a `Sec-Fetch-Site: same-origin` header. Requests that do not satisfy the rule SHALL be rejected with a typed `same_origin_required` error before reaching any handler, including the Milestone 9 cancel and surface-action endpoints. Read methods (GET, HEAD, OPTIONS) SHALL NOT require the signal.

#### Scenario: Cross-origin state change is rejected

- **WHEN** a POST request carries an `Origin` whose host does not match the request host
- **THEN** the guard SHALL return HTTP 403 with a typed `same_origin_required` error and no handler SHALL run

#### Scenario: Same-origin state change proceeds

- **WHEN** a state-changing request carries a matching `Origin` or `Sec-Fetch-Site: same-origin`
- **THEN** the guard SHALL pass the request to the target handler

#### Scenario: Reads are unaffected

- **WHEN** a GET request carries no origin signal
- **THEN** the guard SHALL allow the request through to the resource or streaming handler

### Requirement: Product projections expose Danny-owned fields only

The system SHALL provide a read-only projection repository with bounded list and detail views for tasks, schedules and their occurrences, webhook deliveries and notifications, knowledge generations, artifacts and source metadata, and execution inspector records. Each projection SHALL be an explicit allowlist of Danny-owned fields with declared JSON names. No Baymax internal event type, Stele envelope, raw payload blob, or native identity SHALL appear on the wire. Unknown IDs SHALL produce a typed not-found error, and list endpoints SHALL be bounded by a fixed cap.

#### Scenario: Projection contains only declared fields

- **WHEN** any `/api/` list or detail endpoint is consumed
- **THEN** every returned object SHALL contain only documented Danny-owned projection fields, with no raw payload bytes, internal event structures, or native source identifiers

#### Scenario: Unknown resource is typed

- **WHEN** a detail endpoint is requested for an ID that does not exist
- **THEN** the response SHALL be a typed not-found error rather than an empty object or a zero-value row

#### Scenario: Lists are bounded

- **WHEN** a list endpoint is called on a store containing more rows than the cap
- **THEN** the response SHALL contain at most the fixed cap of ordered rows and SHALL NOT stream an unbounded result set

### Requirement: Task updates carry an expected revision and surface conflicts

The system SHALL expose a single task mutation endpoint that requires an `expected_revision` in the request body and delegates to the existing conditional repository update. A stale revision SHALL return HTTP 409 with a typed `task_revision_conflict` error and SHALL NOT overwrite newer state. A missing or malformed revision SHALL be rejected as a malformed request. No other browser write path SHALL be introduced by this boundary.

#### Scenario: Stale update is rejected without mutation

- **WHEN** a task update carries an `expected_revision` older than the committed revision
- **THEN** the endpoint SHALL return HTTP 409 with `task_revision_conflict` and the stored task SHALL remain unchanged

#### Scenario: Current update succeeds

- **WHEN** a task update carries the current `expected_revision`
- **THEN** the repository SHALL commit the patch, advance the revision, and the response SHALL contain the updated projection

#### Scenario: Browser data cannot mint a new write path

- **WHEN** a client posts to any endpoint other than the revision-aware task update or the Milestone 9 control endpoints
- **THEN** the request SHALL be rejected as not found or not allowed and no mutation SHALL occur

### Requirement: The workbench consumes projections and server-registered surfaces

The system SHALL ship a React + TypeScript + Vite workbench that consumes the Milestone 9 AG-UI SSE projection with `Last-Event-ID` reconnect, renders the `/api/` projections, and renders dynamic surfaces only from the server-registered catalog using the A2UI v0.9.1 wire version. Action submission SHALL go through the existing server-validated surface endpoint with an idempotency key and SHALL display typed rejections. The client SHALL NOT map browser-provided component data to a mutation, register a catalog entry, or claim a different A2UI version.

#### Scenario: Reconnect uses the event cursor

- **WHEN** the workbench reconnects to an execution stream
- **THEN** it SHALL resume from the last observed event ID through the Milestone 9 projection rather than restarting the execution

#### Scenario: Surface action passes the server gate

- **WHEN** a user activates a surface action in the workbench
- **THEN** the client SHALL submit it to the existing server-validated endpoint with an idempotency key and SHALL display the typed acceptance or rejection returned by the server

#### Scenario: No unverified version claim

- **WHEN** the workbench surface contract or any documentation claim is reviewed
- **THEN** it SHALL pin A2UI v0.9.1 and SHALL NOT assert A2UI 1.0 support or React renderer 1.0 certification

### Requirement: Contract tests cover the boundary and its verification gate

The system SHALL include deterministic tests over `httptest` covering routing, same-origin rejection, loopback validation, typed errors, projection field allowlists, revision conflicts, SSE passthrough, and static asset handling. The full verification suite (`go build ./...`, `go test ./... -count=1`, front-end type-check and production build, `openspec validate --all --strict`, `git diff --check`) SHALL pass before the change is archived, and evidence locations SHALL be recorded in `docs/http-workbench.md`.

#### Scenario: Guards and routes are exercised

- **WHEN** the contract test suite runs
- **THEN** it SHALL assert same-origin rejection on every state-changing method, loopback address validation, typed not-found and conflict responses, and unchanged Milestone 9 SSE framing through the assembled handler

#### Scenario: Front-end builds without a browser

- **WHEN** the workbench toolchain is verified
- **THEN** the TypeScript type-check and production build SHALL succeed, and browser end-to-end scenarios SHALL remain recorded as Milestone 12 work rather than claimed
