## Purpose

Define the reproducible local project foundation required to validate Baymax and Stele safely before user-facing Agent capabilities are introduced.

## ADDED Requirements

### Requirement: Reproducible project configuration
The system SHALL provide a documented, validated configuration contract for the OpenAI-compatible model endpoint, selected model, pinned Baymax and Stele dependency versions, storage driver, data directories, scheduler settings, listener address, and Docker profile. A clean checkout with the documented configuration SHALL resolve the same application dependency versions and configuration defaults.

#### Scenario: Valid local configuration loads
- **WHEN** a user starts the project with a complete valid local configuration
- **THEN** the system SHALL load the configuration, report the selected dependency versions and storage mode, and start without contacting an unconfigured model endpoint

#### Scenario: Invalid configuration is rejected before startup
- **WHEN** a required configuration value is missing or has an invalid value
- **THEN** the system SHALL fail startup with an error that identifies the invalid setting and SHALL not start application workers

### Requirement: Local-only unauthenticated access
The unauthenticated local deployment SHALL bind the HTTP listener and published Docker port to a loopback address by default. The system SHALL reject a non-loopback listener or published port unless an authenticated mode with authorization and request-forgery protection is explicitly enabled.

#### Scenario: Default deployment remains local-only
- **WHEN** a user starts the default local configuration or Docker profile
- **THEN** the HTTP service SHALL be reachable through `127.0.0.1` and SHALL not be published to a non-loopback host interface

#### Scenario: Non-loopback configuration is rejected
- **WHEN** a user configures a non-loopback listener while authenticated mode is disabled
- **THEN** startup SHALL fail before the HTTP service accepts requests and SHALL explain that authenticated mode is required

### Requirement: Upstream dependency findings are actionable
The project SHALL maintain separate Baymax and Stele findings records for any observed upstream defect, missing capability, or required optimization. Each finding SHALL identify the exact dependency version or commit, deterministic reproduction, observed and expected behavior, evidence, affected roadmap task, upstream reference, and explicit unblock condition.

#### Scenario: Upstream defect blocks dependent work
- **WHEN** a deterministic probe finds a Baymax or Stele behavior that requires an upstream change
- **THEN** the project SHALL record the finding in the dependency-specific document, mark every dependent task `blocked-upstream`, and SHALL not add a production workaround for that behavior

#### Scenario: Verified upstream repair unblocks work
- **WHEN** an upstream release or pinned reviewable commit fixes a recorded finding
- **THEN** the project SHALL rerun the recorded reproduction and affected integration checks before changing the finding to `fixed-upstream` and unblocking dependent tasks

### Requirement: Baseline validation and deterministic fixtures
The project SHALL provide documented commands for formatting, static analysis, unit tests, and documentation consistency checks. It SHALL include deterministic fixtures for documents, tasks, schedules, and Agent events that later milestones can use without a live model service.

#### Scenario: Baseline checks run from a clean checkout
- **WHEN** a contributor runs the documented baseline validation commands in a clean checkout
- **THEN** each command SHALL complete with a pass/fail result that identifies the failing check when a baseline contract is violated

#### Scenario: Fixture corpus supports offline tests
- **WHEN** a runtime, recovery, or configuration test needs document, task, schedule, or Agent event input
- **THEN** the test SHALL be able to use the deterministic fixture corpus without requiring a live model endpoint

### Requirement: Application domains do not call provider SDKs directly
The application domain modules SHALL use the Baymax model adapter boundary for model execution. Knowledge, task, scheduler, Web, and storage modules SHALL not invoke a model provider SDK directly.

#### Scenario: Static boundary check rejects direct provider usage
- **WHEN** a domain module adds a direct model provider SDK dependency or call
- **THEN** the baseline static check SHALL fail and identify the prohibited module boundary
