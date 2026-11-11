# Stele Context Evidence

This document records the context boundary evidence for the pinned Stele dependency.

## Pin and Public Contract

- Dependency: `github.com/FelixSeptem/stele`
- Current dependency pin: `v0.0.0-20261025080141-f38016ec0cba` (`f38016e`)
- Latest live-service evidence: `testdata/runtimeprobe/stele-f38016-danny-smoke.evidence.json` (2026-10-26), plus the independent Stele assurance reports listed below.
- Historical blocked version: `v0.0.0-20261019065620-7ffd30f9e635`
- Contract source: `openapi.SpecYAML()` from the pinned module, especially `ProviderContextInput`, `ProviderContextResponse`, `ProviderContextReference`, `ProviderContextSourceCitation`, `ProviderContextBudgetAccounting`, and `ProviderContextContinuityResult`.
- Danny adapter: `internal/context/stele/adapter.go`

The request fixture uses only documented snake-case input fields. Scope and runtime/session binding are carried by the authenticated headers; `role` and `references` are not treated as Stele controls. The response fixture uses the five documented result sections (`profile`, `recent_session`, `recent_episodes`, `relevant_summaries`, and `related_entities`) and the top-level and per-reference citation arrays.

## Deterministic Fixture

`testdata/runtimeprobe/stele-context-reference.json` is a compact provider/context response fixture at the pinned response shape. `TestPinnedContextFixtureMatchesOpenAPIAndProjectionBoundary` serves that exact body through an HTTP test server and verifies:

- `provider-v1` metadata, `providerAssembleContext`, and the turn-bound `context-turn-digest-v1` response;
- all response sections, canonical-memory citation identity, source metadata, and reference provenance;
- item budget units, effective/requested byte and citation bounds, and exact response-byte accounting;
- exact `path_prefix` forwarding and rejection of ambiguous or unsafe selectors;
- authenticated scope, binding, and session headers;
- absence of undocumented `role` and `references` request controls;
- projection continuity and separate Stele source and Danny projection digests.

Related negative fixtures in `internal/context/stele/adapter_test.go` reject turn drift, foreign paths, missing provenance, incomplete budget accounting, oversized bodies, malformed continuity receipts, and non-item budgets before model execution.

## Live Service Status

The current `f38016ec0cba` source was built as an isolated image and verified
against a fresh PostgreSQL 18 + pgvector stack using Stele's
`stele-provider-context-verify.ps1`. Initial and restart phases passed the
public context contract, durable exact-path reference contract, and runtime
feedback projection contract. The bounded reports are:

- `testdata/runtimeprobe/stele-f38016-live.evidence.json` and its `.initial.json`
  companion;
- `testdata/runtimeprobe/stele-f38016-reference.evidence.json` and its
  `.initial.json` companion;
- `testdata/runtimeprobe/stele-f38016-projection.evidence.json` and its
  `.initial.json` companion.

The reports bind to source revision `f38016ec0cbafcd6877e2f66ee1da44ea8e4b057`
and image digest `sha256:8835b606d21c55cbf2d00cd6cc43f73e553b97c0ee7502be550e80a08c481e1e`.
They confirm governance completion, non-root exact-path retrieval, durable
labels, replay conflict behavior, continuity, and restart identity. The stack
was created under an isolated random project and removed by the verifier.

Danny task/run references and bounded compilation projections now explicitly
send `source_metadata` version `source-metadata-v1`, trust `untrusted`, and
classification `medium`. This is an application-owned disclosure decision for
these bounded runtime references; it does not attest their content or grant
tool authority. The outbound payload assertions live in
`internal/context/stele/adapter_test.go` and
`internal/context/stele/compile_projection_test.go`. Existing stored events
without labels remain `untrusted/restricted`; this change does not relabel old
records.

The historical `7ffd30f9e635` service was built and tested against an isolated
Docker PostgreSQL database. Its turns/retrieve route-gate failure is retained
in `testdata/runtimeprobe/stele-7ffd-live.evidence.json` and finding
`STELE-006`.

The previously tested `b63151c0464d` service was built from its exact module-cache source
and bootstrapped against a fresh `stele_pinned_b631` PostgreSQL database and a
new exact scope. The route-gate repair is present: memory append/retrieve,
restart context and event idempotency pass. However, the documented minimal
`POST /v1/provider/turns` request returns HTTP 400 `operation_failed` even
without optional context-budget or input idempotency fields. Exact-path
references and compilation lookup also remain incomplete in this run. The
sanitized result is `testdata/runtimeprobe/stele-b631-live.evidence.json` and
the remaining turn failure is recorded as `STELE-007`. Context turn/continuity
and dependent acceptance remain blocked. No local route fallback, disclosure
bypass, or native endpoint substitution is used.

The current `f38016e` pin documents that omitted event `source_metadata`
defaults to `untrusted/restricted`, which governance may promote but public
retrieve/context will exclude. Its fresh PostgreSQL/pgvector verification is
captured above. Danny's runtime reference writers now supply an explicit,
policy-approved `untrusted/medium` classification.

The Danny end-to-end smoke was rerun against a random isolated Docker Compose
project using the same `f38016e` image, a fresh PostgreSQL 18 + pgvector
database, and credentials created by Stele's official bootstrap script. All
six capabilities passed: context disclosure and turn continuity, memory
append/retrieve, task/run exact-path references, compilation replay and
lookup, restart context, and event idempotency. The report is
`testdata/runtimeprobe/stele-f38016-danny-smoke.evidence.json`; it contains
only bounded correlation, path, digest, and event identifiers. The temporary
Compose resources and bootstrap credential directory were removed after the
run. The references check intentionally uses a bounded retrieve retry because
Stele governance is asynchronous; it does not rewrite paths or repeat the
event write.
