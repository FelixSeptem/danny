## 1. Contract and persistence

- [x] 1.1 Define the versioned HTTP/JSON parser submission, status, result, cancellation, readiness, capability, and typed error schemas, including opaque staging object references; verify the schema examples validate and reject unknown major versions or arbitrary host paths.
- [x] 1.2 Add SQLite and PostgreSQL migrations for parser jobs, attempts, source fingerprints, result metadata, failure details, and cleanup state; verify both migration paths start from an empty database and restart without duplicate migrations.
- [x] 1.3 Implement the driver-neutral parser repository with idempotency, status transitions, cancellation races, and recovery leases; verify the shared repository contract passes for SQLite and PostgreSQL.
- [x] 1.4 Connect parser job and source metadata creation in one transaction; verify an injected crash leaves both records committed or neither record visible.

## 2. Python parser service

- [x] 2.1 Create the shared parser core, HTTP adapter, CLI adapter, and pinned dependency lock using the OpenKB-derived MarkItDown extras, PyMuPDF, Trafilatura, and optional PageIndex client; verify both adapters start without Pandoc, LibreOffice, Tesseract, or pdftotext installed.
- [x] 2.2 Implement Markdown/plain-text/CSV/HTML/Office/PDF adapters and canonical result envelope; verify representative fixtures produce canonical Markdown, detected format, content hash, and parser version.
- [x] 2.3 Implement short-versus-long PDF routing and explicit PageIndex Cloud mode/fallback reporting; verify local long-PDF parsing works without credentials and a simulated cloud failure records the fallback.
- [x] 2.4 Implement bounded URL acquisition for PDF and HTML with provenance, redirect, byte, and timeout limits; verify disallowed schemes and oversized responses return typed failures.
- [x] 2.5 Implement resource enforcement, per-job temporary directories, cleanup, startup orphan reconciliation, and safe error redaction; verify oversized, malformed, timeout, cancellation, and parser-crash fixtures leave no untracked temporary files.
- [x] 2.6 Implement versioned HTTP health/readiness, capabilities, status/result/cancel operations and deterministic CLI exit-code mapping; verify readiness distinguishes required local adapters from optional PageIndex Cloud availability.

## 3. Go ingestion and knowledge boundary

- [x] 3.1 Implement the Go HTTP parser client and local CLI adapter without importing Python implementation packages; verify both contract paths cover accepted, duplicate, unsupported, resource-limited, timeout, cancellation, and terminal failure responses.
- [x] 3.2 Store parser provenance, source fingerprint, canonical content hash, warnings, provider mode, and untrusted-source marker in source metadata and generation inputs; verify a compiled Office/PDF source retains these fields.
- [x] 3.3 Reject non-success parser results before compilation planning and preserve the failure for inspection; verify no staged or committed generation references a failed parser result.
- [x] 3.4 Propagate the untrusted marker into KnowledgeAgent context assembly and tool policy; verify instruction-like parsed content cannot authorize memory, task, schedule, webhook, or compilation writes.

## 4. Docker and operations

- [x] 4.1 Add a dedicated parser Dockerfile with pinned Python dependencies and a non-executable temporary directory; verify the image builds on the supported architecture.
- [x] 4.2 Add a Compose parser profile, internal app-to-parser HTTP network, loopback-only host bindings, health checks, environment-based provider configuration, and CPU/memory/concurrency limits; verify the default profile does not publish the parser beyond localhost and the CLI remains available for diagnostics.
- [x] 4.3 Add startup reconciliation and observability for pending parser jobs, retries, provider attempts, cleanup, and terminal failures; verify restart resumes an accepted job or records a structured terminal failure.
- [x] 4.4 Document local and cloud parser modes, supported formats, dependency footprint, resource settings, and troubleshooting; verify the docs match the contract and roadmap.

## 5. Integration verification

- [x] 5.1 Run the full format fixture matrix for Markdown, TXT, CSV, HTML, DOCX, PPTX, XLSX, XLS, short PDF, and long PDF through both CLI and HTTP adapters; verify equivalent canonical output or equivalent structured failure.
- [x] 5.2 Run duplicate submission, restart, cancellation, crash-boundary, and materialization recovery scenarios in SQLite and PostgreSQL; verify one logical parser result and no partial generation input.
- [x] 5.3 Run lint, unit, repository contract, Docker smoke, and documentation consistency checks; verify the parser change satisfies the roadmap Milestone 7 exit gate.
