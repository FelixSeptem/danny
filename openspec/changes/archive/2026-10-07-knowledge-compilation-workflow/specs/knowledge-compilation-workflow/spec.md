## Purpose

This capability turns trusted source selections into durable, browsable, cited knowledge generations while using Baymax for bounded compilation orchestration and Stele for compile-context continuity.

## ADDED Requirements

### Requirement: Source ingestion is incremental and provenance preserving

The system SHALL accept Markdown and plain-text source files through an explicit ingestion request. It SHALL normalize line endings, compute a stable content hash, assign a source identity, detect unchanged and removed inputs, and preserve the original source bytes and path metadata for later citation.

#### Scenario: Unchanged source is skipped
- **WHEN** an ingestion request contains a source whose normalized content hash matches the latest committed metadata
- **THEN** the compiler SHALL report the source as unchanged and SHALL not create duplicate source or artifact rows

#### Scenario: Changed source is selected for recompilation
- **WHEN** an ingestion request contains a known path with a different normalized content hash
- **THEN** the compiler SHALL create a new source decision that references both the previous identity and the new content hash

#### Scenario: Removed source is represented explicitly
- **WHEN** a previously indexed source is absent from an explicit directory scan
- **THEN** the compiler SHALL represent the removal in the compilation plan and SHALL not silently leave broken source references in the next committed index

### Requirement: Compilation plans are validated before durable mutation

The system SHALL create a server-controlled `CompilationPlan` containing source decisions, section identities, proposed artifact identities, lineage, links, citations, generation metadata, and an idempotent effect key. Only a validated plan SHALL be accepted by the compilation commit operation.

#### Scenario: Invalid source reference blocks commit
- **WHEN** a proposed artifact cites a source identity or section range absent from the plan
- **THEN** validation SHALL reject the plan with a typed citation error and SHALL write no committed generation

#### Scenario: Duplicate artifact identity blocks commit
- **WHEN** a plan contains two artifacts with the same generation-scoped identity or an invalid artifact path
- **THEN** validation SHALL reject the plan before any durable artifact becomes reader-visible

#### Scenario: Valid plan reaches commit gate
- **WHEN** all source, artifact, link, citation, policy, and lineage checks pass
- **THEN** the plan SHALL be eligible for one idempotent generation commit with its stable effect key

### Requirement: Knowledge artifacts form a navigable lineage graph

The system SHALL represent source documents, summaries, concepts, entities, section trees, knowledge indexes, and saved explorations as typed artifacts with source references, generation identity, content hash, and lineage. Links SHALL resolve only within the committed generation or to explicitly retained source identities.

#### Scenario: Section tree preserves source ranges
- **WHEN** a Markdown document is normalized and parsed
- **THEN** each section SHALL have a stable identity, heading path, parent relationship, and source range sufficient for bounded citation reads

#### Scenario: Cross-document concept keeps supporting sources
- **WHEN** independent source analyses identify the same concept
- **THEN** the committed concept artifact SHALL retain links to each supporting source or section and SHALL not replace provenance with an unsupported synthesis

#### Scenario: Unknown link is rejected
- **WHEN** an artifact links to an identity that is neither staged in the plan nor present in the referenced committed generation
- **THEN** compilation validation SHALL reject the plan

### Requirement: Baymax owns compilation orchestration and bounded aggregation

The system SHALL execute compilation through the KnowledgeAgent and Baymax public execution boundaries. Independent sections SHALL be eligible for bounded subagent fanout and team parallel aggregation; conflicting concepts or citations SHALL be eligible for team vote validation. The application SHALL not reimplement Baymax runner, team, subagent, Context Assembler, or terminal semantics.

#### Scenario: Independent sections fan out within limits
- **WHEN** a compilation plan contains independent sections and sufficient parent budget
- **THEN** Baymax child runs SHALL receive bounded section projections, inherit the parent capability digest, and return correlated findings for aggregation

#### Scenario: Conflicting citation decisions are voted
- **WHEN** section analyses disagree about a citation or concept merge
- **THEN** a bounded Baymax vote operation SHALL produce a typed decision with the participating evidence references

#### Scenario: Child failure blocks unsafe commit
- **WHEN** a required section child reaches a terminal failure or loses its authorized projection
- **THEN** the parent compilation SHALL remain uncommitted and SHALL expose a correlated diagnostic or pause state

### Requirement: Committed generations are atomic and authoritative

The system SHALL commit source metadata, generated artifacts, lineage, manifest, generation status, and commit marker through the existing transactional storage contract. Readers SHALL resolve only the latest committed generation. Filesystem exports SHALL be derived from the committed manifest, and Stele projection SHALL be represented by a durable outbox intent outside the database generation transaction.

#### Scenario: Complete generation becomes visible
- **WHEN** a validated compilation transaction commits all required rows and its commit marker
- **THEN** navigation and citation reads SHALL resolve the new generation as one complete logical state

#### Scenario: Crash before generation commit
- **WHEN** the process fails before the new generation commit marker is durable
- **THEN** readers SHALL continue to observe the previous committed generation and SHALL not resolve staged artifacts or citations

#### Scenario: Crash after commit before export
- **WHEN** the process fails after the generation commit marker is durable but before filesystem materialization completes
- **THEN** readers SHALL continue using the committed database generation and startup reconciliation SHALL rebuild the derived export from its manifest

#### Scenario: Stele is temporarily unavailable
- **WHEN** the generation transaction commits but the Stele projection cannot be delivered
- **THEN** the generation SHALL remain valid, the durable projection outbox SHALL remain retryable with the same idempotency key, and navigation SHALL continue from the committed KnowledgeStore

### Requirement: Navigation follows the knowledge graph and returns citations

The system SHALL expose bounded navigation operations for the committed index, summaries, concepts, entities, candidate search, and source sections. Results SHALL include artifact identity, generation ID, lineage, and source references, and source section reads SHALL enforce a bounded content limit.

#### Scenario: Query traverses index to source section
- **WHEN** a query finds a relevant index entry and linked summary or concept
- **THEN** navigation SHALL return the linked artifact references and allow a bounded read of the cited source section

#### Scenario: Citation survives generation selection
- **WHEN** a caller requests a citation by artifact and source identity
- **THEN** the system SHALL resolve it against the requested committed generation and SHALL reject citations to staged, missing, or superseded identities according to the generation contract

#### Scenario: Bounded source read truncates explicitly
- **WHEN** a source section exceeds the requested read limit
- **THEN** the response SHALL return a deterministic truncation marker and retain the source range and citation identity

### Requirement: Imported and generated text cannot authorize compilation side effects

The system SHALL treat source text, generated artifacts, summaries, model findings, and citation descriptions as untrusted data. The `commit_compilation` operation SHALL require a server-created validated plan and the active compile policy capability, regardless of instructions found in any text field.

#### Scenario: Prompt injection in a source does not commit
- **WHEN** a source contains instructions to commit artifacts, alter policy, or call another tool
- **THEN** the compiler SHALL preserve the text as data and SHALL not commit unless an independently authorized validated plan reaches the commit gate

#### Scenario: Model output without provenance is rejected
- **WHEN** a generated finding contains a claim without a valid source reference
- **THEN** citation validation SHALL reject the finding or mark the plan uncommittable
