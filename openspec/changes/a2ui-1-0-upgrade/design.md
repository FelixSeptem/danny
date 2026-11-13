# Design: a2ui-1-0-upgrade

## Context

M9 left Danny with a working A2UI v0.9.1 wire adapter (`RenderA2UI`), a version-independent surface model, and a server-side submission validator. The compatibility note explicitly refused any v1.0 claim because v1.0 was a Candidate with an unverified React renderer. On 2026-11-11 A2UI 1.0 reached a stable release and the web/React renderer now supports 1.0, satisfying the two release/renderer conditions M9 recorded. This patch upgrades the only claimed A2UI wire version to 1.0 and removes the v0.9.1 path.

## Goals / Non-Goals

**Goals:**

- Single A2UI wire version `1.0`; no retained v0.9.1 branch in code, fixtures, or docs.
- Wire adapter and fixture conform to the 1.0 envelope; the real `@a2ui/react` 1.0 renderer renders the Danny fixture.
- Security boundary unchanged: declarative data, server-registered catalog, server-validated submissions.

**Non-Goals:**

- Building the React workbench UI (Milestone 10).
- Adopting every v1.0 feature (bidirectional function calls, accessibility tree, extensions) as product features; these are scoped to catalog-safe wiring only and may be deepened later.
- Any change to AG-UI projection, storage schema, or Baymax/Stele integration.

## Decisions

### D1. One wire version, 1.0; delete the 0.9.1 path

Set `A2UIWireVersion = "1.0"` and rewrite `RenderA2UI` to emit the 1.0 envelope. The v0.9.1 constant, adapter branch, and fixture are removed. Because `DynamicSurface.CatalogVersion` (`danny.catalog.v1`) is decoupled from the A2UI wire version, no stored surface data changes.

### D2. Fixture-driven, renderer-verified

The shipped fixture moves to the 1.0 envelope and is checked by (a) the Go adapter round-trip and (b) the real `@a2ui/react` 1.0 package rendering it. The renderer check is the acceptance gate; without it the claim stays "fixture-only" and the compatibility note must say so.

### D3. v1.0 surface capabilities stay behind the catalog

A2UI 1.0 adds bidirectional function calls, `surfaceProperties`, extension metadata, and caller authorization. Danny exposes none of these as authorizing paths: every component/action still resolves to the server-registered catalog, and every submission still validates at Danny's authorization and idempotency boundary. New 1.0 wire fields are carried as data only; they cannot register entries, mint actions, or expand tool capability.

### D4. Docs and roadmap reflect the upgrade

`docs/agent-ui-protocols.md` claims A2UI 1.0 with the verified React renderer version and the satisfied conditions, and drops the v0.9.1-only line. `docs/roadmap.md` M9 status and version-policy paragraph are updated to record the patch.

## Risks / Trade-offs

- **Renderer availability**: the real `@a2ui/react` 1.0 package must be installable in the build environment; if the registry is unreachable the renderer check degrades to fixture-only and the note must state that explicitly.
- **Wire drift**: 1.0 envelope details (field names, required keys) must match the published spec; the renderer check is what guarantees correctness, so implementation details are validated there rather than assumed.

## Migration Plan

No storage migration. The server stub and surface endpoints keep their behavior; only the emitted wire version and envelope change. Existing stored surfaces are unaffected because the catalog version is independent of the A2UI wire version.

## Open Questions

- None blocking. Whether to surface v1.0 bidirectional function calls as a Danny product feature is a later, separate decision.
