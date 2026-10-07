## Why

Milestone 3 provides a policy-controlled KnowledgeAgent and bounded context projections, but the project still lacks the OpenKB-style workflow that turns source documents into durable, navigable, cited knowledge. This change adds the first real compilation path so Baymax and Stele are validated through a user-facing knowledge use case while the existing SQLite/PostgreSQL generation and outbox guarantees remain authoritative.

## What Changes

- Add a document ingestion boundary for Markdown and plain-text sources with content hashing and incremental change detection.
- Add normalized source sections, section trees, generated knowledge artifacts, artifact lineage, and a browsable knowledge index.
- Add a server-created `CompilationPlan` and validation gate for source identity, artifact identity, links, and citations before commit.
- Add a Baymax-backed compilation workflow using bounded section subagents, team aggregation, citation validation, and a commit gate.
- Add KnowledgeStore navigation operations for indexes, summaries, concepts, entities, candidates, and bounded source sections.
- Commit complete knowledge generations through the existing durable generation and manifest repositories, with readers restricted to committed generations.
- Publish a durable Stele context projection outbox event after the KnowledgeStore transaction; Stele remains a reference and continuity store rather than a second artifact store.
- Add deterministic compiler, citation, crash-recovery, and pinned Baymax/Stele integration fixtures.

## Capabilities

### New Capabilities

- `knowledge-compilation-workflow`: Ingest documents, compile durable OpenKB-style knowledge artifacts with Baymax, validate citations, navigate committed generations, and project compile context to Stele.

### Modified Capabilities

- None.

## Impact

- Affected code: `internal/knowledge`, `internal/storage`, `internal/agent/baymax`, `internal/context/stele`, and compile-focused fixtures.
- Affected persistence: source metadata, generation, artifact, lineage, manifest, and Stele projection outbox records use the existing SQLite/PostgreSQL contract.
- Affected Baymax integration: compilation uses the public runner, bounded subagent, team parallel, team vote, Context Assembler, and terminal-event boundaries.
- Affected Stele integration: compile decisions and generation references are projected through the existing adapter and durable outbox; generated pages are not copied into Stele.
- Security boundary: imported and generated text remains untrusted data, while `commit_compilation` accepts only a server-created and validated compilation plan.
