# Parser service

Danny uses a shared Python parser core with two adapters:

- `danny-parser-http` is the production adapter. It serves versioned HTTP/JSON on the internal Compose network.
- `danny-parser` is the local, CI, and diagnostics adapter. It reads a JSON request file and writes a JSON result file.

Both adapters use the same staging object, content hash, canonical Markdown, provenance, warning, failure, and terminal status model.

## HTTP contract

The current contract is `v1`:

```text
POST /v1/parser/jobs
POST /v1/parser/urls
GET  /v1/parser/jobs/{job_id}
GET  /v1/parser/jobs/{job_id}/result
POST /v1/parser/jobs/{job_id}/cancel
GET  /v1/health/live
GET  /v1/health/ready
GET  /v1/capabilities
```

The job request contains an opaque `staging_object_id`, source fingerprint, content hash, filename, format hint, parser policy, and idempotency key. It never contains an arbitrary host filesystem path.

## Local CLI

```powershell
python -m parser_service.cli `
  --request request.json `
  --result result.json `
  --staging-root data/staging
```

The CLI exit codes are mapped to parser terminal states:

| Exit code | State |
| ---: | --- |
| 0 | `succeeded` |
| 1 | `failed` |
| 10 | `unsupported` |
| 11 | `resource_limited` |
| 12 | `timeout` |
| 13 | `cancelled` |
| 20 | `provider_failed` |

## Docker profile

The parser profile is defined in `compose.parser.yaml`:

```powershell
docker compose --profile parser -f compose.parser.yaml up --build
```

The parser has no default host port publication. It is reachable from the application through the internal Compose network. The default application listener remains loopback-only.

The parser image does not install Pandoc, LibreOffice, Tesseract, or `pdftotext`. It installs the pinned OpenKB-derived Python dependencies from `parser_service/pyproject.toml`.

## Provider modes

- Local Markdown, text, CSV, HTML, Office, and PDF conversion is always available when the corresponding Python wheels are installed.
- Long PDF PageIndex Cloud enhancement is opt-in through `PAGEINDEX_API_KEY` and parser policy.
- Cloud failure is recorded and can fall back to local PDF extraction.
- Source text and extracted Markdown are always marked as untrusted data before KnowledgeAgent context assembly.
