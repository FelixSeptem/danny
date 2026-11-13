# Tasks: release-validation

## 1. Scenario registry

- [ ] 1.1 Add `internal/scenarios` with a registry entry per roadmap scenario: stable ID, title, classification, proof reference, and evidence location.
- [ ] 1.2 Add an invariant test: every scenario S01-S16 is registered, every entry has a reason, and every `executed` entry names a real check.
- [ ] 1.3 Add the executable checks for the scenarios classified `executed` (S01, S03-S15, S16 deterministic half).
- [ ] 1.4 Record `live-required` scenarios with the exact command and required environment; never report them as passing.

## 2. Deterministic doubles

- [ ] 2.1 Add `internal/deterministic` implementing the planner, `agent/knowledge.Executor`, and `knowledge.CompileExecutor` boundaries with scripted responses and call recording.
- [ ] 2.2 Assert the recorded calls: plan digest, capability digest, citation payloads, aggregate ordering, and single-commit behavior.
- [ ] 2.3 Ensure the doubles never reach the network; a provider dial is a test failure.

## 3. Crash and restart recovery

- [ ] 3.1 Add crash injection at each documented outbox, lease, materialization, and webhook boundary.
- [ ] 3.2 Reopen a second store instance over the same data directory and assert reconciliation restored every invariant.
- [ ] 3.3 Run the same recovery checks against both SQLite and PostgreSQL.

## 4. Dual storage modes and parser matrix

- [ ] 4.1 Run the scenario suite against SQLite (file database) and PostgreSQL (Compose override) and compare the results.
- [ ] 4.2 Add deterministic parser samples for PDF, DOCX, PPTX, XLSX, XLS, HTML, CSV, TXT, and Markdown.
- [ ] 4.3 Assert canonical output or a structured failure per format, and add the formats to the checked-in fixture manifest.

## 5. Browser boundary convergence

- [ ] 5.1 Verify AG-UI reconnect with `Last-Event-ID` converges with the durable execution record across direct, child, team, workflow, mailbox, and scheduled run modes.
- [ ] 5.2 Verify the surface submission matrix: stale, replayed, unauthorized, malformed, and untrusted submissions create no effect and expand no authority.
- [ ] 5.3 Record the real-browser run as an explicitly unverified step rather than claiming it.

## 6. Upstream linkage and release gate

- [ ] 6.1 Add a finding-to-fixture linkage check over both upstream findings documents: version, reproduction, evidence, affected scenario, and unblock condition.
- [ ] 6.2 Fail the gate when a `blocked-upstream` finding sits inside a claimed scenario.
- [ ] 6.3 Add `cmd/release-gate` that runs every suite, collects results, and writes the evidence matrix.
- [ ] 6.4 Add `docs/release-gate.md` with the scenario map, the six release-gate clauses, the evidence matrix, and the unverified list.

## 7. Verification

- [ ] 7.1 `gofmt -l`, `go build ./...`, `go vet ./...`, `go test ./... -count=1`, focused `golangci-lint`, `openspec validate --all --strict`, `git diff --check`.
- [ ] 7.2 Execute `cmd/release-gate` end to end and record its output.
- [ ] 7.3 Record every check that could not be executed as unverified with its reason.
