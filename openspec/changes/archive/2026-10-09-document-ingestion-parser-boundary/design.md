## Context

See proposal.md for the motivation. The current roadmap has a Go application with SQLite/PostgreSQL durability, Baymax execution, Stele context, and a knowledge compiler that currently assumes Markdown and plain text inputs. OpenKB's inspected conversion path uses pinned Python libraries: MarkItDown with Office extras, PyMuPDF for PDF extraction, Trafilatura for HTML URL extraction, and optional PageIndex Cloud. No system document CLI is required by the observed OpenKB conversion path, but some Python wheels contain native binaries and MarkItDown imports Magika/ONNX Runtime.

## Goals / Non-Goals

**Goals:**

- Isolate format-specific parsing in a separately deployable parser service.
- Define a stable result model that the Go compiler can consume without importing Python packages.
- Provide local support for the OpenKB-derived format set and explicit PageIndex Cloud behavior for long PDFs.
- Make parser submissions idempotent, restart-safe, resource bounded, and observable.
- Preserve provenance, untrusted-source status, and failure details through the KnowledgeStore generation boundary.
- Make SQLite and PostgreSQL persistence behavior identical for parser jobs and source metadata.
- Provide a Docker Compose profile with health/readiness checks and loopback-only defaults.

**Non-Goals:**

- Implement the HTTP/Web workbench in this change.
- Reimplement Baymax orchestration, Stele internals, or the KnowledgeStore generation commit.
- Install or depend on Pandoc, LibreOffice, Tesseract, pdftotext, or other system document CLIs.
- Promise OCR for scanned PDFs in local mode when PyMuPDF cannot extract usable text.
- Add broad web crawling or JavaScript browser rendering.

## Decisions

### 1. Use HTTP/JSON for production and CLI for local execution

Production Go-to-Python communication SHALL use HTTP/1.1 with JSON over the Docker internal network or loopback. The versioned API exposes:

- `POST /v1/parser/jobs` to submit a job;
- `GET /v1/parser/jobs/{job_id}` to read status;
- `GET /v1/parser/jobs/{job_id}/result` to retrieve a canonical result;
- `POST /v1/parser/jobs/{job_id}/cancel` to request cancellation;
- `GET /v1/health/live` and `GET /v1/health/ready` for process and dependency health;
- `GET /v1/capabilities` for supported formats, parser version, and optional provider status.

The request contains an opaque `staging_object_id`, source fingerprint, content hash, format hint, parser policy, and idempotency key. Go writes the immutable source bytes into its controlled staging root before submitting the request. Python resolves only object IDs below its configured staging root and never receives an arbitrary host path. The response and job state use explicit versions and typed terminal states: succeeded, unsupported, resource_limited, timeout, provider_failed, cancelled, and failed.

The Python distribution SHALL also provide a CLI adapter using the same request, result, error, and capability models. The CLI is for local development, deterministic fixtures, and diagnostics; it is not the cross-container production transport. Its request and result files use the same staging object and hash rules, and its exit codes map to the same terminal statuses.

A separate parser process is preferred over in-process Python embedding because it keeps Python/native dependencies out of the Go binary, permits independent resource limits, and makes crashes and restarts observable. HTTP is preferred over JSON-RPC because resource-oriented jobs, health checks, OpenAPI tooling, and Docker networking are direct to express. gRPC is deferred because the parser is an asynchronous control plane rather than a high-throughput streaming data plane. Unix domain sockets are deferred because they do not naturally cover Docker Compose networking or independent deployment. A direct Go-only implementation was rejected because it would not reach the required Office/PDF fidelity. System CLI adapters such as Pandoc or LibreOffice were rejected because OpenKB's observed conversion path does not require them and they add platform-specific process complexity.

### 2. Use a canonical parsed document envelope

The result envelope includes source identity, original filename, media type, detected format, source fingerprint, normalized content hash, parser version, provider mode, canonical Markdown, page/section metadata, extracted image references, warnings, untrusted-data marker, and failure details. The compiler stores the envelope metadata and reads only canonical content for section extraction and citation.

Raw bytes are retained through the existing committed source/generation contract. The parser service may use a controlled staging directory, but the database remains authoritative for accepted source metadata and committed generation inputs.

### 3. Use persistent job state and idempotency keys

Parser jobs are stored in the application database with an idempotency key, source fingerprint, attempt count, lease/retry state, result reference, and cleanup status. The parser worker can be restarted without losing an accepted request. Duplicate submissions return the original logical job. A stable result identity prevents duplicate source rows and duplicate generation inputs.

The parser service itself uses an internal work queue backed by durable job state; it does not treat an HTTP request as the job lifetime. Cancellation is cooperative and records a terminal cancellation or a typed cancellation race.

The HTTP request only submits or reads a durable job. Go may poll the status endpoint, but parser progress is not coupled to the caller connection. The Go HTTP workbench may later expose SSE based on the application execution stream; the Python parser contract does not require SSE.

### 4. Keep local parsing and cloud enhancement explicit

The parser image always contains local adapters. PDF page count determines short versus long handling using a configured threshold. Long PDFs use local PyMuPDF by default. PageIndex Cloud is enabled only when credentials and policy permit it; provider attempts, fallback reason, and final mode are persisted. Cloud failure may fall back to local extraction only when the configured policy allows it.

HTML URL ingestion is a separate acquisition operation: the service fetches bounded HTTP content, uses Trafilatura for main-content extraction, and then passes canonical Markdown to the normal result path. Direct local HTML files use the local converter.

### 5. Enforce resource limits at the service boundary

The parser accepts a maximum byte size before storing an upload, caps PDF pages, limits concurrent conversions, applies wall-clock deadlines, and uses container CPU/memory/tmpfs limits. Temporary files are created below a per-job directory and removed on success, failure, cancellation, and startup reconciliation. Error responses expose categories and safe metadata, never stack traces or arbitrary host paths.

### 6. Propagate untrusted data metadata

Every parsed result carries an untrusted-data marker. The Go ingestion adapter copies this marker into source metadata and generation inputs. KnowledgeAgent context assembly receives source text as data and keeps tool capabilities server controlled. This preserves the prompt-injection boundary even when imported documents contain instruction-like text.

### 7. Compose deployment

The default Compose profile starts the Go app and parser service with host ports bound to 127.0.0.1. The parser profile exposes only an internal network endpoint to the app, a readiness endpoint for required local adapters, and a separate provider status for optional PageIndex Cloud. Parser configuration is supplied through environment variables and a mounted, non-executable temporary directory.

The local development profile may invoke the CLI directly from the parser image or Python environment. CLI and HTTP executions SHALL run the same parser core and SHALL produce equivalent canonical results for the same source fingerprint and parser version.

## Risks / Trade-offs

- [Risk] Python/native wheels increase image size and architecture-specific build risk → pin versions, build a dedicated parser image, run multi-architecture smoke checks, and keep parser optional for deployments that only need Markdown.
- [Risk] Office and PDF conversion can consume unbounded CPU or memory → enforce per-job limits, worker concurrency, container limits, and terminal resource states.
- [Risk] Cloud OCR can produce different content from local extraction → record provider mode and parser version in provenance; never silently overwrite a local result without a new fingerprint.
- [Risk] A parser crash can leave temporary files → persist cleanup state and reconcile orphaned job directories on startup.
- [Risk] URL ingestion can fetch unexpected content → allow only configured schemes, cap redirects and response bytes, apply network timeouts, and preserve URL provenance.
- [Risk] Parser contract evolution can break the compiler → version the envelope and reject unsupported major versions before generation planning.

## Migration Plan

1. Add parser job/result migrations and repository contract tests for SQLite and PostgreSQL.
2. Implement the parser contract and local Python adapter with deterministic format fixtures.
3. Add the parser Docker image, Compose profile, health checks, resource limits, and startup cleanup.
4. Add the Go ingestion adapter and generation input validation, leaving existing Markdown/plain-text paths compatible.
5. Run dual fixtures for legacy text ingestion and parser-backed ingestion, then make parser-backed ingestion the only path for non-Markdown formats.
6. Roll back by disabling parser-backed formats and retaining accepted source metadata; no committed generation is removed during rollback.
