## 1. Domain contracts and source ingestion

- [x] 1.1 Define source identities, normalized source metadata, source scan decisions, section identities, source ranges, and typed ingestion outcomes; verify unchanged, changed, and explicitly removed sources are deterministic.
- [x] 1.2 Implement Markdown/plain-text normalization and heading-based section-tree extraction; verify line-ending normalization, nested headings, duplicate headings, empty sections, and stable section identities with fixtures.
- [x] 1.3 Define artifact types, artifact lineage, citation references, compilation plans, validation diagnostics, and bounded navigation responses; verify JSON round trips preserve generation and source provenance.
- [x] 1.4 Add compiler policy tools and server-created `CompilationPlan` admission to the KnowledgeAgent registry; verify source text and model output cannot authorize `commit_compilation`.

## 2. KnowledgeStore and generation planning

- [x] 2.1 Extend the storage repositories and shared SQLite/PostgreSQL contract for source metadata, staged artifacts, lineage, manifests, and committed generation reads; verify both drivers expose the same typed outcomes.
- [x] 2.2 Implement incremental source selection and artifact planning for summaries, concepts, entities, section trees, indexes, and saved explorations; verify unchanged inputs are skipped and changed or removed inputs create explicit decisions.
- [x] 2.3 Implement structural compilation-plan validation for empty artifacts, invalid paths, duplicate identities, unknown links, missing sources, invalid ranges, and unsupported citations; verify invalid plans cannot reach the commit repository.
- [x] 2.4 Implement idempotent generation commit using the existing effect-key and generation transaction boundaries; verify replay returns the original generation result and staged rows remain invisible before the commit marker.
- [x] 2.5 Implement manifest-based filesystem materialization and startup reconciliation; verify missing, extra, and partially written export files are rebuilt from a committed generation without changing generation identity.

## 3. Baymax compilation orchestration

- [x] 3.1 Add a compile coordinator that admits a compile-policy KnowledgeAgent run, creates a plan version and projection, and correlates Baymax parent and child evidence; verify the capability and projection digests remain stable.
- [x] 3.2 Implement bounded section subagent fanout through the Baymax public composer/child boundary; verify independent sections inherit the parent prefix and capability digest and remain within the parent child and token budget.
- [x] 3.3 Implement Baymax team-parallel aggregation for section findings; verify all required child terminal outcomes are acknowledged, failed children block commit, and aggregation preserves source references.
- [x] 3.4 Implement Baymax team-vote validation for conflicting concepts and citations; verify the decision includes participating evidence references and unresolved conflicts keep the plan uncommittable.
- [x] 3.5 Route each compile stage through the Baymax Context Assembler with role-specific bounded projections; verify pressure compaction preserves source ranges and handoff references.
- [x] 3.6 Add a compile commit gate that invokes only the validated server-created plan; verify prompt-injection text, unreferenced model claims, and capability drift cannot commit a generation.

## 4. Navigation and Stele continuity

- [x] 4.1 Implement bounded KnowledgeStore navigation for index, summary, concept, entity, candidate search, and source-section reads; verify every response includes generation identity, artifact lineage, and bounded citation references.
- [x] 4.2 Implement deterministic index traversal from query to linked artifacts and source sections; verify staged, missing, and superseded artifacts are not returned as current navigation results.
- [x] 4.3 Add the transactional Stele context projection outbox payload for compile decisions, generation references, and source identities; verify it is committed with the generation intent and uses a stable idempotency key.
- [x] 4.4 Implement the Stele projection worker adapter and retry/reconciliation path; verify temporary Stele failure leaves the generation valid and retries the same reference-first payload without copying artifact bodies.

## 5. Verification and upstream validation

- [x] 5.1 Add deterministic fake-model compiler fixtures for normalization, section fanout, aggregation, vote conflicts, citation rejection, prompt injection, and bounded navigation; verify no invalid plan produces a durable generation.
- [x] 5.2 Add SQLite/PostgreSQL generation contract and crash-boundary fixtures for pre-commit, post-commit/pre-materialization, replay, and reconciliation; verify readers observe either the previous generation or the complete new generation.
- [x] 5.3 Add pinned Baymax live fixtures for subagent, team parallel, team vote, Context Assembler, and terminal correlation; if an upstream contract is missing, record it in Chinese findings and mark the dependent task `blocked-upstream` without a workaround.
- [x] 5.4 Add pinned Stele live fixtures for compile-context projection, reference lookup, idempotent replay, and restart reconciliation; verified against `v0.0.0-20261007033528-a556ac0ddacc` after the upstream STELE-005 provenance UUID fix. Compilation and task/run references reconcile through the governance worker, and replay remains idempotent.
- [x] 5.5 Run the full unit, integration, storage-driver, provider-boundary, documentation, and OpenSpec validation suites; verify the Milestone 4 exit gate and that `.tmp/` and `debug.log` remain untouched. Completed with the pinned Stele `a556ac0ddacc` live fixture, full Go tests, `go vet`, strict OpenSpec validation, documentation and provider-boundary checks, diff validation, and isolated Compose cleanup.
