# Agent UI protocols: compatibility note

Status: 2026-11-12 (Milestone 9, change `agent-ui-protocol-boundary`).

## AG-UI

- Danny projects execution evidence through its own versioned envelope
  (`danny.agent-ui.projection.v1`, `internal/agentui`) and renders it to
  AG-UI-compatible wire events through a local adapter. Danny embeds no
  AG-UI SDK and imports no upstream package.
- Compatibility claim: AG-UI-compatible as implemented and fixture-verified.
  `internal/agentui/testdata/agui-run-fixture.json` pins the wire rendering
  for a full run (lifecycle, step, text and tool-call streams, documented
  `danny.*` custom events). The claim covers the event vocabulary and
  ordering semantics the fixture demonstrates; it is not a protocol
  conformance certification.
- Run pause (`input_required`, `approval_required`, `budget_exhausted`)
  projects as `RunFinished` with an `interrupt` outcome; product terminals
  are `RunFinished` (success) and `RunError`.

## A2UI

- Claimed version: **A2UI v0.9.1 only** (`internal/agentui.RenderA2UI`).
  The React renderer for v0.9.1 is listed as Stable upstream; the wire
  fixture is `internal/agentui/testdata/surface-fixture.a2ui.json`.
- **A2UI v1.0 is NOT supported and NOT claimed.** As checked on 2026-10-09
  and re-stated at implementation time, v1.0 is a Candidate/Release
  candidate with a roadmap target of Q4 2026 and no committed release date,
  and React renderer support for v1.0 is Planned, not shipped. Danny must
  not claim v1.0 support until (a) the specification reaches a stable
  release, (b) the React renderer declares v1.0 support, and (c) a rendered
  compatibility fixture passes against the real renderer package. The
  version-independent `DynamicSurface`/`SurfaceAction`/`SurfaceSubmission`
  models keep a future v1.0 adapter additive.
- MCP-UI/MCP Apps remains deferred; it is not a replacement for the local
  workbench.

## Security boundary

- The dynamic-surface catalog is server-registered only. Model output and
  untrusted content (labels, options, source excerpts, retrieved memory,
  imported documents) are data; there is no path from content to catalog
  registration, action creation, tool-capability expansion, or commit-gate
  bypass.
- Every surface submission validates execution ID, surface ID, surface
  revision, action ID, user activation, input schema, authorization policy,
  task revision, and idempotency key at Danny's existing authorization and
  idempotency boundary (`SurfaceService.Submit` + `idempotency_effects`).
  Stale, replayed, unauthorized, malformed, and untrusted submissions fail
  with typed errors.

## Evidence

- AG-UI wire fixture: `internal/agentui/testdata/agui-run-fixture.json`
- A2UI v0.9.1 surface fixture: `internal/agentui/testdata/surface-fixture.a2ui.json`
- Projection/replay contracts: `internal/agentui`, `internal/storage/agentui_repo.go`
- SSE reconnect/replay and submission convergence: `internal/server/agentui_test.go`
- React renderer integration against the real `@a2ui/react` package is a
  Milestone 10 workbench task and is explicitly deferred, not claimed.
