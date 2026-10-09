## Why

The roadmap's next HTTP workbench milestone would otherwise need to embed document parsing decisions inside the Go API. OpenKB demonstrates that reliable support for PDF, Office, HTML, CSV, and text formats depends on a pinned Python parser stack (MarkItDown extras, PyMuPDF, Trafilatura, and optional PageIndex Cloud), while its current source does not require system document CLIs. Danny needs a versioned parser boundary before exposing uploads so format support, resource limits, failures, provenance, and Docker deployment remain explicit and testable.

## What Changes

- Add an isolated parser service contract for local file ingestion and canonical Markdown results.
- Use HTTP/1.1 with JSON as the production Go-to-Python transport, while providing a CLI adapter over the same parser core for local development and diagnostics.
- Support the OpenKB-derived format set: Markdown, plain text, CSV, HTML, DOCX, PPTX, XLSX, XLS, and PDF.
- Add explicit parser modes for local conversion, optional PageIndex Cloud enhancement, unsupported input, and persisted failure.
- Persist source fingerprints, content hashes, parser/provider versions, page and section metadata, image references, warnings, and failure details.
- Add idempotent parser job submission and restart-safe status/result retrieval.
- Define file size, page count, CPU, memory, concurrency, timeout, temporary storage, and cleanup limits.
- Carry an untrusted-source marker into KnowledgeAgent context and preserve server-controlled tool capabilities.
- Add a parser Docker image and Compose profile with health/readiness checks and loopback-only default exposure.
- Keep the HTTP/Web workbench as a later change that consumes this contract instead of implementing format-specific parsing.

## Capabilities

### New Capabilities

- `document-ingestion-parser-boundary`: Versioned parser service API, canonical parsed document model, supported formats, resource limits, provider modes, failure semantics, and restart/idempotency behavior.

### Modified Capabilities

- `knowledge-compilation-workflow`: Compilation ingestion SHALL consume canonical parser results and preserve parser provenance and untrusted-source metadata for citations and tool policy.
- `storage-durability`: Durable source metadata SHALL include parser jobs/results and their idempotency and recovery state.

## Impact

- New Python parser service and Docker image using pinned OpenKB-derived dependencies; no default Pandoc, LibreOffice, Tesseract, or pdftotext installation.
- Go ingestion adapter, source metadata repositories, generation input validation, and KnowledgeAgent context policy.
- SQLite/PostgreSQL migrations and repository contract tests.
- Docker Compose profiles, health checks, resource limits, and temporary-file cleanup.
- Later HTTP API/Web workbench scope and roadmap dependencies.
