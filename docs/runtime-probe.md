# Runtime probe evidence

Milestone 1 uses the pinned public APIs of Baymax and Stele as the integration
boundary. The deterministic command is:

```powershell
$env:GOCACHE = "$PWD/.gocache"
$env:GOSUMDB = "off"
$env:GOPROXY = "off"
go run ./cmd/runtime-probe offline > runtime-probe.json
```

The command uses Baymax's `integration/fakes.Model`, then runs the actual
`core/runner.Engine` for both Run and Stream. It does not contact a model
endpoint. Stele is represented as `skipped-unconfigured` until a real Stele
service and PostgreSQL system of record are configured; no in-memory substitute
is used.

The opt-in live path is separate:

```powershell
$env:OPENAI_BASE_URL = "https://provider.example/v1"
$env:OPENAI_API_KEY = "..."
$env:OPENAI_MODEL = "model-name"
$env:STELE_URL = "http://127.0.0.1:8080"
go run ./cmd/runtime-probe live-smoke > live-runtime-probe.json
```

For a real Stele-only probe against a PostgreSQL-backed service:

```powershell
$env:STELE_URL = "http://127.0.0.1:8080"
$env:STELE_CREDENTIAL_FILE = ".tmp/stele-smoke-credentials/runtime.credential"
$env:STELE_TENANT = "tenant-a"
$env:STELE_PROJECT = "project-a"
$env:STELE_NAMESPACE = "namespace-a"
go run ./cmd/runtime-probe stele-smoke > stele-runtime-probe.json
```

The Stele service must be started with `STELE_PROVIDER_ENABLED=true` and
`STELE_PROVIDER_SCHEMA_VERSIONS=provider-v1`. The bootstrap credential is
created by Stele's documented `stele-bootstrap-smoke.ps1`; it is read from a
file and is never emitted into evidence.

Evidence uses schema `runtime-probe.v1`. Sequence numbers are rewritten after
combining probe cases, and validation rejects missing IDs, broken ancestor
correlation, sequence gaps, and duplicate terminal events. Before writing an
artifact, `runtimeprobe.Redact` replaces secrets and hashes sensitive or
unbounded values. Credentials, authorization headers, prompts, and raw provider
payloads must not be committed.

## Capability status

- `passed`: the configured adapter completed the case and produced valid evidence.
- `failed-contract`: the adapter reached the dependency but the operation failed.
- `skipped-unconfigured`: the case requires an explicitly configured external service.
- `blocked-upstream`: a deterministic reproduction proves a dependency contract defect.

Stele provider operations use `/v1/provider/runtime`, `/v1/provider/context`,
`/v1/provider/intents`, and the other paths in the pinned OpenAPI document. The
adapter sends `X-Stele-Runtime-Binding`, `X-Stele-Runtime-Session`, and the
occurrence-scoped `Idempotency-Key` where available.

The offline evidence covers Baymax Run/Stream, typed local tools, Composer
child dispatch, teams, mailbox lifecycle, and scheduler/context adapter calls
through deterministic fixtures. Stele persistence cases use the real service
and PostgreSQL system of record; they are never replaced with an in-process
store.

The latest Compose-backed Stele run is recorded at
`testdata/runtimeprobe/stele-compose.evidence.json`. Context, basic memory
append/query, fresh-adapter restart projection, and occurrence replay passed
against `github.com/FelixSeptem/stele
v0.0.0-20261006031612-63d1f3dc4675`. The provider principal UUID issue from
`STELE-002` and the canonical path promotion issue from `STELE-003` are fixed
upstream. After the existing asynchronous governance worker completes, the
references case is `passed`: both task and run exact-path reads return true,
and the database preserves the non-root paths in candidate and canonical rows.
The adapter uses Stele's documented `conversation.message` event type and
continues sending `input.top_k`; it does not maintain a local reference store
or rewrite paths. Because Stele governance runs asynchronously in the worker,
the live smoke verification waits for that existing worker projection before
performing the exact-path read.
