# docker-operations Specification (Delta)

## ADDED Requirements

### Requirement: A reproducible multi-stage image builds the service and the workbench

The system SHALL provide a root `Dockerfile` that builds the React workbench assets and the Go service into a single runtime image. The build SHALL use pinned base images and `CGO_ENABLED=0`, and the runtime image SHALL contain the static binary and the built workbench assets without a Node.js, Python, Go, or C toolchain, source tree, or local cache. A `.dockerignore` SHALL exclude dependency directories, build outputs, VCS data, session data, and local data from the build context. The runtime process SHALL NOT run as root.

#### Scenario: Clean context produces a static service image

- **WHEN** the image is built from a clean checkout
- **THEN** the workbench assets SHALL be present under the configured `DANNY_WORKBENCH_DIR`, the Go binary SHALL start without cgo, and neither `web/node_modules` nor the source tree SHALL be present in the runtime layer

#### Scenario: Runtime has no build toolchain

- **WHEN** the runtime image is inspected
- **THEN** it SHALL NOT contain Node.js, Python, the Go compiler, or a C compiler, and the process SHALL run as a non-root user

### Requirement: The default SQLite profile starts with one documented command

The system SHALL provide a root `compose.yaml` whose default profile starts the assembled service with the SQLite driver and mounted data, documents, and artifacts directories. Startup SHALL perform `Open`, `Migrate`, and `Reconcile` before listening, so a clean machine needs no manual build, migration, or artifact step. The parser and database services SHALL NOT be required by the default profile.

#### Scenario: Clean machine one-command startup

- **WHEN** an operator runs the single documented startup command on a machine with only Docker available
- **THEN** the service SHALL create and migrate its SQLite database, reconcile derived artifacts, and become ready without any additional command

#### Scenario: Data survives a restart

- **WHEN** the default profile container is stopped and restarted with its named volumes intact
- **THEN** the database, documents, and derived artifacts SHALL remain available and the service SHALL return to ready

### Requirement: A PostgreSQL override gates startup on database health

The system SHALL provide a `compose.postgres.yaml` override with a pinned database image, a named data volume, and a `pg_isready` health check. Loading it together with the base `compose.yaml` SHALL give the application service `depends_on` with `condition: service_healthy` and SHALL set `DANNY_STORAGE_DRIVER=postgres` and a `postgres`-scheme `DANNY_DATABASE_URL`. No database port SHALL be published to the host. Migrations SHALL run through the existing startup sequence only after the database is healthy.

#### Scenario: Migrations never race an unready database

- **WHEN** the PostgreSQL override starts while the database is still initializing
- **THEN** the application container SHALL wait for the healthy condition, run migrations, and only then become ready

#### Scenario: Database is not host-reachable

- **WHEN** the normalized Compose configuration for the PostgreSQL override is inspected
- **THEN** no service SHALL publish a database port to the host

### Requirement: Host publication stays loopback-only and the container listener is an explicit opt-in

Every host port in `compose.yaml` SHALL be published with an explicit `127.0.0.1` host binding, and no service port SHALL be published without one. Because an in-container loopback bind is unreachable through a published port, the system SHALL provide an explicit `DANNY_CONTAINER_LISTEN` flag, default false. With the flag off, the existing listener policy SHALL still reject a non-loopback address. With the flag on, `security.ValidateContainerListener` SHALL accept only wildcard addresses and SHALL reject a concrete non-loopback IP. The flag SHALL NOT be implemented by enabling `DANNY_AUTHENTICATED`, which has no authentication implementation. A test SHALL read `compose.yaml` and fail on any unbound or wildcard host publication.

#### Scenario: Default profile rejects a widened listener

- **WHEN** configuration is loaded without `DANNY_CONTAINER_LISTEN` and with a non-loopback `DANNY_LISTEN_ADDRESS`
- **THEN** validation SHALL fail with the existing non-loopback error

#### Scenario: Container profile accepts only a wildcard listener

- **WHEN** `DANNY_CONTAINER_LISTEN` is enabled
- **THEN** `0.0.0.0` or `[::]` SHALL validate, a concrete non-loopback IP SHALL be rejected, and the Compose file SHALL bind every host port to `127.0.0.1`

### Requirement: Liveness and readiness report real dependencies

The system SHALL expose `/api/health/live` for process liveness and `/api/health/ready` for dependency readiness without changing the existing `/api/health`. Readiness SHALL perform a real storage `Ping`, verify the artifact directory is usable, and report the configured storage driver. A failing or missing storage dependency SHALL produce a typed service-unavailable response rather than an empty success. The Compose health check SHALL use the readiness endpoint.

#### Scenario: Readiness reflects database reachability

- **WHEN** the storage backend is unreachable
- **THEN** `/api/health/ready` SHALL return a typed 503 while `/api/health/live` SHALL still report the process as live

#### Scenario: Startup reconciliation is observable

- **WHEN** the service starts after derived artifacts were deleted
- **THEN** startup SHALL reconcile from committed generations and readiness SHALL NOT succeed before reconciliation completes

### Requirement: Configuration, secrets, backup, and recovery are documented and verifiable

The system SHALL provide `.env.example` documenting the OpenAI-compatible endpoint, model name, credentials, storage selection, timezone, parser URL, and profile flags, and SHALL NOT bake any secret into an image or commit real credentials. `docs/docker-operations.md` SHALL document one-command startup, the profile matrix, health endpoints, the loopback threat model, backup/export of the database, manifests, source files, and Stele data, and a recovery smoke procedure that deletes derived artifacts and restarts. Every verification check that was not actually executed SHALL be recorded as unverified rather than claimed as passing.

#### Scenario: No secret is baked in

- **WHEN** the image and `.env.example` are inspected
- **THEN** no model credential or API key SHALL be present, and `.env` SHALL remain ignored by Git

#### Scenario: Derived artifacts rebuild after deletion

- **WHEN** the derived artifact volume is deleted and the service is restarted
- **THEN** the documented recovery procedure SHALL rebuild artifacts from committed generations and the service SHALL become ready

#### Scenario: Unverified checks are labelled

- **WHEN** an exit-gate check cannot be executed in the build environment
- **THEN** `docs/docker-operations.md` SHALL list it as unverified with the reason, and no feature claim SHALL depend on it
