# document-ingestion-parser-boundary Specification

## Purpose

This capability provides a versioned, isolated document parsing boundary that converts supported user sources into canonical, provenance-preserving content for knowledge compilation.

## Requirements

### Requirement: Go and Python use a versioned HTTP JSON contract

The production parser integration SHALL use versioned HTTP/1.1 endpoints with JSON request and response bodies. The contract SHALL expose job submission, status retrieval, result retrieval, cancellation, liveness, readiness, and capability discovery. Requests SHALL identify source bytes through an opaque staging object identifier and SHALL include a source fingerprint, content hash, parser policy, and idempotency key.

#### Scenario: Job is submitted without exposing a host path
- **WHEN** Go submits a parser job
- **THEN** the request SHALL contain an opaque staging object identifier and SHALL not require an arbitrary filesystem path or inline unbounded document body

#### Scenario: Parser API version is incompatible
- **WHEN** a client requests an unsupported major contract version
- **THEN** the parser SHALL return a typed compatibility error before reading the source object

### Requirement: CLI and HTTP adapters have equivalent parser semantics

The parser distribution SHALL provide a CLI adapter for local development, deterministic fixtures, and diagnostics. The CLI and HTTP adapters SHALL use the same parser core, request model, result model, error categories, staging object rules, content hash rules, and format support matrix. CLI exit statuses SHALL map deterministically to parser terminal statuses.

#### Scenario: Same fixture produces equivalent results
- **WHEN** the same source fingerprint and parser version are processed through the CLI and HTTP adapters
- **THEN** both adapters SHALL produce equivalent detected format, canonical Markdown, content hash, warnings, provider mode, and terminal status

#### Scenario: CLI failure maps to a typed status
- **WHEN** the CLI process exits because of an unsupported format, resource limit, timeout, cancellation, or parser failure
- **THEN** the Go adapter SHALL map the exit status and result metadata to the corresponding parser terminal status without exposing arbitrary stderr as user content

### Requirement: Parser accepts the OpenKB-derived format set

The parser service SHALL accept Markdown, plain text, CSV, HTML, DOCX, PPTX, XLSX, XLS, and PDF inputs and SHALL report the detected format in every terminal result.

#### Scenario: Supported local document is accepted
- **WHEN** a caller submits a file with a supported format and the file is readable
- **THEN** the parser SHALL accept the job and expose a terminal result containing the detected format and canonical Markdown content

#### Scenario: Unknown extension is rejected
- **WHEN** a caller submits an unknown or disallowed format
- **THEN** the parser SHALL return a structured `unsupported` result and SHALL not mark the source as parsed

### Requirement: Parser results are canonical and provenance preserving

The parser SHALL return canonical Markdown plus a source fingerprint, normalized content hash, parser/provider version, source format, page or section metadata when available, image references, warnings, and the original source identity.

#### Scenario: Result can be used for citation
- **WHEN** a supported PDF, Office, HTML, text, CSV, or Markdown source is parsed successfully
- **THEN** the result SHALL contain stable source identity and content metadata sufficient for a later generation to cite the source and bounded section or page

#### Scenario: Parser warning does not erase provenance
- **WHEN** conversion succeeds with a warning such as incomplete HTML extraction or image loss
- **THEN** the terminal result SHALL be successful with the warning preserved and the source identity unchanged

### Requirement: Parser jobs are idempotent and restart safe

The parser SHALL accept a caller-provided idempotency key and source fingerprint. Repeating an equivalent submission SHALL return the existing logical job or result, and accepted jobs SHALL remain observable after parser process restart.

#### Scenario: Duplicate submission converges
- **WHEN** two submissions use the same idempotency key and equivalent source fingerprint
- **THEN** the service SHALL create one logical parser job and both callers SHALL receive the same job and result identity

#### Scenario: Restart preserves accepted work
- **WHEN** the parser process exits after accepting a job but before returning its terminal result
- **THEN** a replacement process SHALL recover the job state or expose a terminal structured failure, without silently losing the request

### Requirement: Parser resource limits are explicit

The parser SHALL enforce configured limits for input bytes, PDF pages, CPU time, memory, concurrency, wall-clock timeout, temporary storage, and cleanup. A limit violation SHALL be terminal and typed.

#### Scenario: Oversized source is rejected
- **WHEN** an input exceeds the configured byte or page limit
- **THEN** the parser SHALL return a `resource_limited` result that identifies the violated limit and SHALL not invoke a model or external provider

#### Scenario: Timed out conversion is recoverable
- **WHEN** conversion exceeds its wall-clock deadline
- **THEN** the parser SHALL record a `timeout` failure with cleanup status and SHALL make the job eligible for a bounded retry according to policy

### Requirement: Provider mode and fallback are observable

The parser SHALL support local conversion by default and SHALL treat PageIndex Cloud as an explicit optional provider for long PDF OCR or cloud document import. Every terminal result SHALL record the selected mode, provider attempts, fallback decisions, and external error category when applicable.

#### Scenario: Local long PDF works without cloud credentials
- **WHEN** a long PDF is submitted without PageIndex Cloud credentials
- **THEN** the parser SHALL use the local path and SHALL return a successful local result or a structured local failure

#### Scenario: Cloud failure falls back explicitly
- **WHEN** PageIndex Cloud is enabled and its OCR or content request fails
- **THEN** the parser SHALL either return a successful local fallback result with the cloud failure recorded or return a terminal provider failure according to configured policy

### Requirement: Parsed source is marked as untrusted data

The parser SHALL mark original source text, extracted Markdown, images, warnings, and metadata as untrusted data. The marker SHALL survive handoff to knowledge compilation and SHALL not grant or expand Agent tool capabilities.

#### Scenario: Instruction-like source cannot authorize a write
- **WHEN** parsed content contains instructions to save memory, create a task, or commit a generation
- **THEN** the downstream Agent policy SHALL treat those instructions as data and SHALL require an independently authorized operation

### Requirement: Parser service exposes health and bounded lifecycle operations

The parser SHALL expose versioned operations to submit a source, query job status, retrieve a result, cancel a pending job, and report readiness. Requests SHALL have bounded payloads and SHALL return typed errors for malformed input, missing jobs, cancellation races, and unavailable providers.

#### Scenario: Readiness reflects required local dependencies
- **WHEN** the parser service reports ready
- **THEN** all required local format adapters and temporary storage checks SHALL pass, while optional PageIndex Cloud availability SHALL be reported separately

#### Scenario: Missing job is typed
- **WHEN** a caller requests a nonexistent job or result
- **THEN** the service SHALL return a stable not-found error without exposing filesystem paths or internal stack traces
