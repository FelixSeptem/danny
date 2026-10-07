## Context

Milestone 2 already provides driver-neutral generation, artifact, manifest, effect, lease, and outbox repositories. Milestone 3 provides the policy-controlled KnowledgeAgent, bounded projections, and Baymax/Stele adapter boundaries. This change connects those foundations into a compiler while keeping the database authoritative for committed knowledge and Stele authoritative for context continuity.

## Goals / Non-Goals

**Goals:**

- Build an incremental Markdown/plain-text ingestion and normalization pipeline.
- Produce typed, lineage-preserving knowledge artifacts and a committed generation manifest.
- Use Baymax public runner, subagent, team, vote, and Context Assembler APIs for compilation stages.
- Validate every generated claim and link before the commit gate.
- Provide bounded navigation and citation resolution from committed generations.
- Persist Stele projection intent through the existing transactional outbox boundary.
- Exercise SQLite/PostgreSQL and pinned Baymax/Stele behavior with deterministic fixtures.

**Non-Goals:**

- HTTP or browser workbench endpoints.
- Natural-language scheduling, mailbox workers, or task execution modes.
- A vector database or a second memory store inside Danny.
- Copying generated artifacts or full source documents into Stele.
- Treating the mounted filesystem as authoritative knowledge storage.
- Adding a Danny workaround for a missing Baymax or Stele contract.

## Decisions

### 1. Separate ingestion, compilation, commit, and navigation boundaries

Ingestion produces normalized source decisions and section identities. Compilation produces a server-owned plan and staged artifact candidates. Validation is a pure gate over the plan and its references. Commit delegates to the existing generation transaction. Navigation reads only committed generations.

This keeps model output and filesystem scans outside the durable commit until their identities, lineage, and citations are validated. A single compiler service with untyped mutable state was rejected because it would make crash recovery and citation validation difficult to test.

### 2. Use content-addressed source identity with explicit path metadata

The normalized content hash identifies source content, while the original path and scan scope remain metadata for user navigation. Source decisions distinguish unchanged, changed, and removed inputs. This prevents path renames from creating duplicate content while preserving useful citation locations.

The compiler will not infer deletions from an incomplete scan; removal decisions require an explicit scan scope or source manifest. This avoids accidental cleanup when a directory is temporarily unavailable.

### 3. Keep generation commit in the existing storage transaction

The commit gate passes one immutable `CompilationPlan` to the storage generation repository. Source metadata, artifacts, lineage, manifest, and commit marker are written using the existing transaction and effect-key contract. The filesystem materializer consumes only the committed manifest. Stele receives a separate outbox intent containing generation and reference metadata, because Stele cannot participate in the application database transaction.

This preserves the Milestone 2 crash guarantees and avoids claiming cross-store atomicity that cannot be provided.

### 4. Use Baymax orchestration by phase

The coordinator uses the KnowledgeAgent compile policy and Baymax Context Assembler for bounded context. Independent sections use bounded child/subagent dispatch and team parallel aggregation. Concept and citation conflicts use team vote. A serial validation and commit gate follows aggregation. Danny stores correlation and plan evidence but does not implement another runner or aggregation loop.

If a required Baymax capability is absent or its public contract differs from the pinned version, the affected task is recorded in `docs/upstream/baymax-findings.md` and remains blocked. No local emulation is added.

### 5. Make citation validation structural and provenance-first

Every artifact claim is represented with source identity, section range, and generation-scoped references. Validation checks that the cited source exists, the range is valid, links resolve, and generated pages are non-empty. The model may propose claims, but it cannot create authority-bearing references or commit capability.

This allows deterministic fixtures to reject unsupported claims without evaluating model prose semantics.

### 6. Expose navigation as bounded typed reads

Navigation APIs return references and bounded content rather than a serialized knowledge store. Index traversal returns artifact identities and links; source section reads return a bounded range with explicit truncation metadata. This aligns with the existing role projections and prevents large artifacts from entering every Agent context.

## Risks / Trade-offs

- [Risk] Incremental concept/entity updates can leave stale cross-document links. → Mitigation: stage the complete next index and validate all links before committing; removed sources produce explicit cleanup decisions.
- [Risk] Fanout increases duplicate or conflicting model findings. → Mitigation: parent budget bounds children, team aggregation records source identities, and vote validation is required for conflicts.
- [Risk] Filesystem export may lag a committed generation after a crash. → Mitigation: readers use database generations only and startup reconciliation rebuilds exports from the immutable manifest.
- [Risk] Stele projection delivery may remain pending. → Mitigation: commit validity is independent of Stele availability; the durable outbox preserves the same idempotency key and reports projection health separately.
- [Risk] Model output may contain prompt injection or unsupported claims. → Mitigation: all text is untrusted, server-created compilation plans are required, and structural citation validation runs before commit.
- [Risk] Baymax or Stele public APIs may not support a required phase. → Mitigation: add a pinned integration fixture and block the dependent task through the Chinese upstream findings process.

## Migration Plan

1. Add compiler domain contracts and deterministic source/section fixtures without changing existing readers.
2. Implement ingestion, artifact planning, validation, and navigation against the current storage repositories.
3. Add Baymax compilation phases and correlate child/team/vote evidence with the plan.
4. Add generation commit, materialization reconciliation, and Stele projection outbox handling.
5. Run SQLite/PostgreSQL repository contracts, crash-boundary tests, and pinned dependency fixtures.
6. Roll back by disabling compiler admission; existing committed generations and Milestone 3 runtime paths remain readable.

## Open Questions

- The initial summary, concept, and entity prompt templates can be tuned during implementation without changing artifact identity, lineage, or citation contracts.
- Search ranking for candidate navigation can begin deterministic and bounded; a later milestone may add semantic ranking without changing the navigation response shape.
