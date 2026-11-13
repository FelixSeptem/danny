# Tasks: a2ui-1-0-upgrade

## Status: BLOCKED (pending `@a2ui/react` 1.0)

Task 3.1 (renderer verification) is the acceptance gate and cannot pass until `@a2ui/react` publishes 1.0 with a `v1_0` entry point (currently `0.15.0`/v0.9). Do not implement or archive this change until the React renderer ships 1.0.

## 1. Wire adapter upgrade to 1.0

- [ ] 1.1 Set `internal/agentui.A2UIWireVersion` to `1.0` and rewrite `RenderA2UI` (and supporting wire structs in `a2ui.go`) to emit the A2UI 1.0 envelope, including action IDs, `surfaceProperties`, and extension metadata scaffolding from the v1.0 evolution guide; remove all v0.9.1-only code and constants.
- [ ] 1.2 Verify the adapter still enforces the restricted catalog: every emitted component/action resolves to a registered catalog entry, and model-generated or untrusted names are rejected before they reach the wire.

## 2. Fixture and round-trip

- [ ] 2.1 Update `internal/agentui/testdata/surface-fixture.a2ui.json` to the 1.0 envelope (`a2ui_version: "1.0"`) and verify it round-trips through `RenderA2UI` and the catalog validator.
- [ ] 2.2 Add/adjust `internal/agentui/agentui_test.go` so the 1.0 fixture is the single source of truth and the v0.9.1 fixture is gone.

## 3. Real renderer verification (acceptance gate)

- [ ] 3.1 Install `@a2ui/react` 1.0 in a throwaway check and confirm the Danny surface fixture renders through the real package; record the installed package version and the evidence location.
- [ ] 3.2 If the package is unreachable, fall back to fixture-only verification and make `docs/agent-ui-protocols.md` state that explicitly; do NOT claim 1.0 support in that case.

## 4. Security and version-policy consistency

- [ ] 4.1 Confirm A2UI 1.0 surface capabilities (bidirectional function calls, extensions, caller authorization) cannot register catalog entries, mint actions, or expand Baymax tool capability through the submission validator; add a test if the 1.0 envelope introduces a new field that could carry untrusted authority.
- [ ] 4.2 Update `docs/agent-ui-protocols.md` to claim A2UI 1.0 (with verified React renderer version and satisfied conditions) and remove the v0.9.1-only statement; confirm no remaining "v1.0 NOT supported" claim.
- [ ] 4.3 Update `docs/roadmap.md` Milestone 9 status and version-policy paragraph to record the `a2ui-1-0-upgrade` patch and the 1.0 upgrade.

## 5. Exit Gate

- [ ] 5.1 Run `openspec validate a2ui-1-0-upgrade --strict`, `go build ./...`, `go test ./... -count=1`, `golangci-lint run ./...`, and `git diff --check`; verify all implementation checkboxes are backed by acceptance evidence.
- [ ] 5.2 Confirm the only claimed A2UI wire version is 1.0 with no v0.9.1 path retained in code, fixtures, or docs before requesting archive.
