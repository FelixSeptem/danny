## 1. Repository and module bootstrap

- [ ] 1.1 Create `go.mod` with the exact module path `github.com/FelixSeptem/danny`, the roadmap package directories, and package documentation markers; verify `go list -m` reports that path and `go list ./...` resolves every package without application runtime behavior.
- [ ] 1.2 Add the exact Baymax and Stele module dependencies to `go.mod`, generate `go.sum`, and record the resolved versions or commits; verify a clean dependency download/build reproduces the same module graph.
- [ ] 1.3 Add the initial `cmd/personal-agent` configuration-validation entry point; verify invalid configuration exits before creating listeners or workers.

## 2. Configuration contract

- [ ] 2.1 Implement typed configuration parsing in `internal/config/config.go` for model, dependency metadata, storage, directories, listener, scheduler, and Docker profile settings; verify valid environment fixtures produce the expected normalized `Config`.
- [ ] 2.2 Implement configuration defaults and stable validation errors for missing, malformed, or conflicting values; verify unit tests cover valid local configuration and each invalid setting group.
- [ ] 2.3 Document configuration precedence, required variables, local defaults, and baseline commands in `README.md` or `CONTRIBUTING.md`; verify the documented commands match the repository scripts and Go commands.

## 3. Loopback security boundary

- [ ] 3.1 Implement the pure listener policy in `internal/security/listener_policy.go` for loopback IPv4, loopback IPv6, loopback hostname, wildcard, LAN, and public addresses; verify table-driven tests classify each address consistently.
- [ ] 3.2 Connect configuration validation to the listener policy so non-loopback unauthenticated mode fails before service startup; verify the command exits with an actionable authenticated-mode error and does not bind a port.
- [ ] 3.3 Add a configuration fixture documenting the future authenticated-mode gate without implementing authentication; verify the default configuration remains loopback-only.

## 4. Dependency governance and upstream findings

- [ ] 4.1 Create `docs/upstream/baymax-findings.md` and `docs/upstream/stele-findings.md` with the approved statuses, required evidence fields, `blocked-upstream` rule, and unblock condition; verify both documents are present and contain no fabricated findings.
- [ ] 4.2 Add dependency update guidance linking exact module versions or commits to the appropriate findings document; verify the guidance requires a deterministic reproduction before a task can be unblocked.
- [ ] 4.3 Add a deterministic fixture manifest format that records dependency version, fixture identity, and expected result; verify a malformed manifest is rejected by the fixture validation test.

## 5. Deterministic fixture corpus

- [ ] 5.1 Add stable Markdown and plain-text document fixtures under `testdata/fixtures/documents`; verify fixture hashes and source metadata are deterministic across repeated test runs.
- [ ] 5.2 Add task and schedule fixtures under `testdata/fixtures/tasks` and `testdata/fixtures/schedules`, including revisions, timezones, and occurrence metadata; verify JSON parsing and stable IDs.
- [ ] 5.3 Add Baymax-style event envelope fixtures under `testdata/fixtures/events` for parent, child, team, mailbox, scheduler, and terminal events; verify correlation IDs and required fields are present.

## 6. Provider boundary and baseline checks

- [ ] 6.1 Add `scripts/check-provider-boundaries.ps1` to reject direct provider SDK imports from application domain packages while allowing the future Baymax adapter package; verify the script reports the file and prohibited import for a synthetic violation fixture.
- [ ] 6.2 Add baseline formatting, static analysis, unit-test, and documentation consistency commands to the repository guidance; verify each command runs from a clean checkout and reports a nonzero result for an injected failure.
- [ ] 6.3 Add the minimal CI or local verification entry point that runs the boundary check and Go baseline checks; verify the entry point does not contact a live model endpoint.

## 7. Foundation verification

- [ ] 7.1 Run the complete foundation suite from a clean checkout with no external model service and verify module resolution, configuration validation, loopback enforcement, fixture validation, boundary checks, and documentation checks pass.
- [ ] 7.2 Run `openspec validate establish-project-foundation --strict` and resolve every proposal, spec, design, or task validation error before marking this change ready for apply.
