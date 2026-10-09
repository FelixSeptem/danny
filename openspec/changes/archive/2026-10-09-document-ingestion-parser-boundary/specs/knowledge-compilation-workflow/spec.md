## ADDED Requirements

### Requirement: Compilation consumes canonical parser results

The knowledge compiler SHALL accept parser result references as ingestion inputs and SHALL preserve the parser source fingerprint, normalized content hash, parser/provider version, format, warnings, and untrusted-source marker in the source metadata and generation lineage.

#### Scenario: Parsed Office document becomes citable source
- **WHEN** a successful parser result for DOCX, PPTX, XLSX, or XLS is included in a compilation plan
- **THEN** the committed source SHALL retain the parser metadata and expose bounded citations against the canonical Markdown

#### Scenario: Parser failure cannot enter compilation
- **WHEN** a parser result is `unsupported`, `resource_limited`, `timeout`, or terminally failed
- **THEN** the compiler SHALL reject it as a source input and SHALL preserve the failure record for inspection
