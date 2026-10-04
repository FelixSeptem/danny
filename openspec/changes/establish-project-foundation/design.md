## Context

See `proposal.md` for the motivation and scope. The repository currently has no Go module, application packages, OpenSpec capability specs, or runtime configuration. The approved roadmap requires a foundation that can be used by later Baymax/Stele probes without introducing a second orchestration or provider boundary.

The foundation must support a local unauthenticated workbench, reproducible Baymax and Stele dependency versions, future SQLite/PostgreSQL storage selection, deterministic offline fixtures, and an upstream blocking process. It must also be safe to run from Docker later, even though Docker service composition is a later milestone.

## Goals / Non-Goals

**Goals:**

- Establish a compilable Go module and focused package boundaries for later milestones.
- Provide typed configuration parsing and validation with explicit defaults and actionable errors.
- Pin Baymax and Stele to exact reproducible module versions or reviewable commits.
- Enforce loopback-only access for the unauthenticated local mode at the configuration boundary.
- Create separate, structured Baymax and Stele findings documents and deterministic fixture directories.
- Provide baseline validation commands and a static dependency-boundary check.

**Non-Goals:**

- Running a Baymax Agent, creating a Stele context, or calling a model provider. Those belong to the runtime probe milestone.
- Implementing HTTP routes, Web UI, storage repositories, scheduler workers, or Docker Compose services.
- Adding authentication. The foundation only rejects unsafe non-loopback configuration until a later authenticated mode is designed.
- Selecting an application-specific model or provider default beyond validating that the endpoint and model fields are configured when runtime execution is requested.

## Decisions

### 1. Use a Go module with domain package boundaries from the roadmap

Create the module and package directories needed by later milestones, but keep this change's implementation limited to configuration, boundary checks, fixtures, and documentation. The initial package map is:

```text
cmd/personal-agent/
internal/config/
internal/security/
internal/agent/
internal/context/
internal/knowledge/
internal/tasks/
internal/storage/
internal/server/
web/
testdata/fixtures/
scripts/
```

`internal/agent`, `internal/context`, and the other domain packages receive only package documentation or minimal interfaces in this milestone. Runtime behavior is added by later OpenSpec changes.

**Alternative considered:** wait to create package boundaries until the first runtime feature. Rejected because dependency ownership and direct provider usage would then be decided opportunistically inside feature code.

### 2. Pin dependencies in Go module metadata

Baymax and Stele are declared as direct Go module dependencies in `go.mod` and locked in `go.sum`. The selected versions are exact semantic versions or pseudo-versions resolving to reviewable commits. The dependency update procedure records the resolved version and commit in the upstream findings documents when a probe exposes a compatibility issue.

Application packages do not import OpenAI-compatible provider SDKs. Provider execution enters through the future Baymax adapter package; this milestone adds a static boundary check that rejects direct provider imports from `internal/knowledge`, `internal/tasks`, `internal/storage`, `internal/server`, or `web`.

**Alternative considered:** use a replace directive pointing at a mutable local clone. Rejected because it prevents clean-checkout reproducibility. A temporary replace is allowed only in a test fixture branch while recording the exact upstream commit, and it cannot be part of the foundation's normal build.

### 3. Use typed environment configuration with explicit precedence

`internal/config` parses environment variables into a typed `Config` value. The foundation defines these groups:

- model endpoint and model name;
- Baymax and Stele version metadata for diagnostics;
- storage driver and paths;
- documents, artifacts, and fixture directories;
- HTTP listen address and authenticated-mode flag;
- scheduler and Docker profile names.

Precedence is `environment > documented defaults`. Configuration parsing is pure and does not open files, bind sockets, start workers, or contact external services. A separate `Validate` operation applies cross-field rules and returns stable setting names in errors.

The default listener is `127.0.0.1:<port>`. A non-loopback address is invalid when authenticated mode is false. The parser also exposes the normalized host so Docker and the future server can apply the same rule rather than reimplementing it.

**Alternative considered:** accept arbitrary environment values and validate inside each service. Rejected because separate processes could apply inconsistent security or storage defaults.

### 4. Enforce the local security boundary before server construction

`internal/security` provides a pure listener policy check over the normalized address and authentication mode. The future HTTP server must call this check before creating a listener. This milestone tests the policy without opening a port.

The check treats loopback IPv4, loopback IPv6, and an explicit loopback hostname as local. Wildcard addresses, LAN addresses, and public addresses require authenticated mode and request-forgery protection, which are not implemented by this change.

**Alternative considered:** rely only on Docker port publishing to protect the service. Rejected because local development and non-Docker launches would remain unsafe and configuration drift would be easy.

### 5. Keep upstream findings as separate, versioned documents

Create:

- `docs/upstream/baymax-findings.md`
- `docs/upstream/stele-findings.md`

Each starts with the shared status vocabulary `observed`, `upstream-issue-filed`, `blocked-upstream`, `fixed-upstream`, and `closed`, followed by the required finding fields. The documents remain empty of fabricated issues; the first runtime probe appends records only when it has deterministic evidence.

**Alternative considered:** store findings only in GitHub issues. Rejected because the dependency version, reproduction, affected local task, and unblock condition must remain reviewable with the code and available offline.

### 6. Use checked-in deterministic fixtures without live model calls

Create small JSON and Markdown fixtures under `testdata/fixtures/` for source documents, task records, schedule records, and Baymax-style event envelopes. Fixtures use stable IDs and timestamps so later tests can assert correlation and recovery behavior.

The foundation does not include a fake Baymax runtime. It only provides data fixtures and validation helpers; the runtime probe owns the fake model and Baymax API integration.

**Alternative considered:** generate fixtures dynamically in tests. Rejected because checked-in fixtures make regression evidence inspectable and stable across platforms.

### 7. Make baseline checks explicit and platform-compatible

Provide documented commands in `CONTRIBUTING.md` or the repository README for:

- `gofmt` verification;
- `go vet ./...`;
- `go test ./...`;
- documentation consistency checks;
- the direct provider import boundary check.

The boundary check uses repository text scanning with an allowlist for `internal/agent` and future adapter packages. It fails with the file and prohibited import when a domain package imports a provider SDK.

**Alternative considered:** defer checks to a future CI setup. Rejected because later runtime evidence must not be built on an unverified dependency boundary.

## File plan

The implementation of this change is limited to these files and directories:

- `go.mod`, `go.sum`: module identity and exact Baymax/Stele dependency pins.
- `internal/config/config.go`, `internal/config/config_test.go`: typed parsing, defaults, and validation.
- `internal/security/listener_policy.go`, `internal/security/listener_policy_test.go`: loopback and authenticated-mode policy.
- `internal/agent/doc.go`, `internal/context/doc.go`, `internal/knowledge/doc.go`, `internal/tasks/doc.go`, `internal/storage/doc.go`, `internal/server/doc.go`: package ownership markers for the roadmap boundaries.
- `cmd/personal-agent/main.go`: minimal configuration validation entry point that exits before starting runtime workers.
- `docs/upstream/baymax-findings.md`, `docs/upstream/stele-findings.md`: structured upstream finding records.
- `testdata/fixtures/documents/*`, `testdata/fixtures/tasks/*`, `testdata/fixtures/schedules/*`, `testdata/fixtures/events/*`: deterministic inputs for later probes.
- `scripts/check-provider-boundaries.ps1`: static provider import boundary check.
- `README.md` or `CONTRIBUTING.md`: baseline commands and local-only access contract.

## Risks / Trade-offs

- **[Risk] The exact Baymax or Stele module API may not compile at the selected version.** → Resolve the versions during the dependency probe, pin the actual commit, and record the issue in the corresponding upstream findings document. Do not add a production workaround.
- **[Risk] Loopback detection differs across Windows, Linux, and Docker.** → Keep the policy pure, test IPv4, IPv6, hostnames, wildcard addresses, and normalized forms, and use the same normalized value for server and Compose configuration in later milestones.
- **[Risk] Empty package markers create no runtime behavior.** → Keep them intentionally minimal and use the runtime probe as the first change that imports and exercises Baymax/Stele APIs.
- **[Risk] Static import scanning can produce false positives.** → Restrict the scan to Go import blocks and maintain a small explicit allowlist; a false positive blocks the baseline check and is corrected in the check, not bypassed in application code.
- **[Risk] Configuration fields may grow as later milestones add storage or scheduling behavior.** → Keep parsing grouped and additive, with stable defaults and validation tests so new fields do not change existing local behavior silently.

## Migration Plan

1. Add the module and package markers.
2. Add typed configuration and listener policy tests.
3. Add exact dependency pins and run the baseline checks.
4. Add upstream findings documents and deterministic fixtures.
5. Add the minimal configuration validation command.
6. Verify a clean checkout using the documented commands.

Rollback is a Git revert of this change. It removes the foundation files and leaves the original empty repository intact; no user data or external system state is modified.

## Open Questions

None. The runtime probe will choose concrete Baymax and Stele API adapters after this foundation is complete, but that choice does not change the requirements or boundaries defined here.
