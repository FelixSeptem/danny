# a2ui-1-0-upgrade

## Status: BLOCKED (pending `@a2ui/react` 1.0)

As of 2026-11-13 the React renderer `@a2ui/react` is still `0.15.0`/v0.9 with no `v1_0` entry point, so the acceptance gate in Task 3.1 (render the Danny surface fixture through the real `@a2ui/react` 1.0 package) cannot be satisfied. A 1.0 wire would also be unrenderable by the current v0.9 renderer that Milestone 10's workbench builds on. This change is parked until `@a2ui/react` ships 1.0 with a `v1_0` export; do not implement or archive until then.

## Why

Milestone 9 shipped the browser-facing boundary against A2UI **v0.9.1** because, at implementation time (checked 2026-10-09), v1.0 was a Candidate with no committed release and no verified React renderer. That gap closed on 2026-11-11: Google published **A2UI 1.0 as the first stable release**, and the web/React renderer now declares 1.0 support. Both upgrade conditions M9 recorded are therefore satisfied.

Keeping the v0.9.1 wire path alive buys nothing: it doubles fixture and adapter maintenance, and Milestone 10's React workbench should build on the stable 1.0 protocol rather than a superseded one. This patch sits between M9 and M10 and upgrades Danny's only claimed A2UI wire version from v0.9.1 to v1.0, removing the v0.9.1 path entirely.

## What Changes

- Raise `internal/agentui.A2UIWireVersion` from `0.9.1` to `1.0` and rewrite the `RenderA2UI` wire adapter to the 1.0 declarative envelope, including action IDs, `surfaceProperties`, and extension metadata scaffolding from the v1.0 evolution guide. No v0.9.1 code path is retained.
- Update the shipped A2UI wire fixture (`internal/agentui/testdata/surface-fixture.a2ui.json`) to the 1.0 envelope and verify it round-trips through the new adapter and the restricted catalog.
- Verify the 1.0 claim against the real renderer: install `@a2ui/react` 1.0 and confirm the Danny surface fixture renders through the actual package. This is the hard acceptance gate; the claim is not "supported" until this passes.
- Keep the security boundary identical: declarative data only, server-registered catalog, server-side submission validation. A2UI 1.0's new surface capabilities (bidirectional function calls, accessibility, extensions) are evaluated under the same restricted-catalog rule and do not expand Agent authority by themselves.
- Update `docs/agent-ui-protocols.md` to claim A2UI 1.0 (with the verified React renderer version and satisfied upgrade conditions) and remove the v0.9.1-only statement. Update `docs/roadmap.md` Milestone 9 status and version policy.

## Capabilities

### Modified Capabilities

- `agent-ui-protocol-boundary`: upgrade the claimed A2UI wire version from v0.9.1 to v1.0 and remove the deprecated v0.9.1 path; the version-independent `DynamicSurface`/`SurfaceAction`/`SurfaceSubmission` models and the server-side submission validator are unchanged in contract.

## Impact

- Code: `internal/agentui/a2ui.go` (wire version + 1.0 envelope), `internal/agentui/agentui_test.go` and fixture (`testdata/surface-fixture.a2ui.json`).
- Docs: `docs/agent-ui-protocols.md` (compatibility note), `docs/roadmap.md` (M9 status + version policy).
- No storage migration: the surface catalog version (`danny.catalog.v1`) is decoupled from the A2UI wire version, so no table or migration changes are required.
- No Baymax/Stele pin changes. The React workbench UI itself remains a Milestone 10 deliverable; this patch only moves the protocol boundary to 1.0 and proves it against the real renderer.
