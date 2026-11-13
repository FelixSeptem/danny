# local-startup Specification (Delta)

## ADDED Requirements

### Requirement: The repository front door describes the delivered system

`README.md` SHALL describe the project as it currently stands: what it is, which milestones are complete, which capabilities are landed and where each one is specified, how the request paths fit together, which boundaries are enforced, and how the repository is laid out. The description SHALL match the state recorded in `docs/roadmap.md` and SHALL NOT claim a capability that is not specified. The README SHALL remain in English, matching the other repository documents, and SHALL index the documents under `docs/` so a reader can navigate from the front door to the detail.

#### Scenario: A reader can tell what the project supports

- **WHEN** the README is read without opening any other file
- **THEN** the reader SHALL learn the delivered capabilities, the enforced boundaries, and where each capability is specified

#### Scenario: The README claims nothing beyond the specifications

- **WHEN** a capability name appears in the README
- **THEN** a corresponding specification SHALL exist under `openspec/specs/`

### Requirement: Three reproducible startup paths are documented

`README.md` SHALL document three reproducible ways to start the system locally: the local startup script, the Docker SQLite topology, and the Docker PostgreSQL topology, plus the front-end development mode. Each path SHALL state the exact command, its prerequisites, and the address the workbench is reachable at. The Docker paths SHALL name the files they use and SHALL keep the loopback-only publication rule visible.

#### Scenario: Each documented path is runnable as written

- **WHEN** a documented startup command is copied and executed in a prepared environment
- **THEN** the workbench SHALL start on the documented address without an undocumented step

#### Scenario: The published address stays loopback-only

- **WHEN** the README describes a host port
- **THEN** it SHALL state that the publication is bound to `127.0.0.1`

### Requirement: One script starts the local service with its prerequisites checked

The system SHALL provide `scripts/start-local.ps1` as the single local startup entry point. It SHALL verify that `go`, `node`, and `npm` are available and SHALL fail with a named error naming the missing tool. It SHALL build the workbench assets under `web/dist` unless the caller skips that step, SHALL apply the documented local defaults as process environment variables, and SHALL start the service in the foreground while printing the workbench URL and the health endpoints. It SHALL support a check-only mode that performs the prerequisite checks and exits without starting the service, and it SHALL write nothing outside the repository and the data directory the service already uses.

#### Scenario: A missing toolchain is reported clearly

- **WHEN** the script runs and a required tool is not on `PATH`
- **THEN** it SHALL stop with an error naming that tool and the command to install it

#### Scenario: The check-only mode verifies without starting

- **WHEN** the script runs in check-only mode
- **THEN** it SHALL report the toolchain and workbench state and exit without starting the service

#### Scenario: The service starts with the documented defaults

- **WHEN** the script starts the service
- **THEN** the listener SHALL be bound to the documented loopback address and the workbench assets SHALL be served from the built `web/dist`

### Requirement: Documentation references stay executable

`scripts/check-docs-consistency.ps1` SHALL assert that every `scripts/*.ps1` path referenced by `README.md` exists in the repository, in addition to its existing assertions about the required documents and the pinned dependency versions. A README that references a renamed or deleted script SHALL fail the documentation check rather than mislead a reader.

#### Scenario: A stale script reference fails the check

- **WHEN** the README references a script that does not exist
- **THEN** the documentation consistency check SHALL fail and name the missing script

#### Scenario: The current README passes

- **WHEN** the documentation consistency check runs against the repository
- **THEN** it SHALL pass
