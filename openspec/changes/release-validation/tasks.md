# Tasks: release-validation

## 1. Scenario registry

- [x] 1.1 Add `internal/scenarios` with a registry entry per roadmap scenario: stable ID, title, classification, proof reference, and evidence location.
- [x] 1.2 Add an invariant test: every scenario S01-S16 is registered, every entry has a reason, and every `executed` entry names an implemented proof that matches the implementation list.
- [x] 1.3 Map each `executed` scenario to the check that proves it, reusing the existing Milestone contract tests where they already prove the scenario and adding new scenario checks where they did not.
- [x] 1.4 Record `live-required` scenarios with the exact command and required environment; never report them as passing.

## 2. Deterministic doubles

- [x] 2.1 Add `internal/deterministic` implementing the execution and compile-execution boundaries with scripted responses and call recording.
- [x] 2.2 Assert the recorded calls: plan digest and capability digest propagation, and that a rejected plan never reaches the executor.
- [x] 2.3 Ensure the doubles never reach the network; a provider dial is an explicit typed failure.

## 3. Crash and restart recovery

- [x] 3.1 Reuse the storage failure injector that already covers the generation, lease, effect, and materialization boundaries; record it as the S09 proof instead of duplicating the injector.
- [x] 3.2 Reuse the committed-generation rebuild test that reopens the store and asserts the derived materialization is rebuilt.
- [x] 3.3 Run the same recovery checks against both SQLite and PostgreSQL through the release-gate command.

## 4. Dual storage modes and parser matrix

- [x] 4.1 Run the suite against SQLite and PostgreSQL and compare the results; the release-gate command runs both and reports each mode separately.
- [x] 4.2 Add deterministic parser samples for Markdown, TXT, CSV, HTML, DOCX, PPTX, XLSX, XLS, and PDF.
- [x] 4.3 Assert canonical output or a structured failure per format. The binary formats are exercised through their failure path when the optional converter is absent, which is itself the documented structured failure.

## 5. Browser boundary convergence

- [x] 5.1 Verify the AG-UI reconnect with `Last-Event-ID` converges with the durable execution record and ends on one terminal event.
- [x] 5.2 Verify the surface submission matrix: stale, replayed, unauthorized, malformed, and untrusted submissions create no effect and expand no authority.
- [x] 5.3 Record the real-browser run as an explicitly unverified step rather than claiming it.

## 6. Upstream linkage and release gate

- [x] 6.1 Add a finding-to-fixture linkage check over both upstream findings documents: version, reproduction, evidence, affected task, and unblock condition.
- [x] 6.2 Fail the gate when a `blocked-upstream` finding sits inside a claimed scenario.
- [x] 6.3 Add `cmd/release-gate` that runs every suite, collects the results, and writes the evidence matrix.
- [x] 6.4 Add `docs/release-gate.md` with the scenario map, the six release-gate clauses, the evidence matrix, and the unverified list.

## 7. Verification

- [x] 7.1 `gofmt -l`, `go build ./...`, `go vet ./...`, `go test ./... -count=1`, focused `golangci-lint`, `openspec validate --all --strict`, `git diff --check`.
- [x] 7.2 Execute `cmd/release-gate` end to end in both storage modes and record its output.
- [x] 7.3 Record every check that could not be executed as unverified with its reason.
