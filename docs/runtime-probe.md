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

The opt-in live path is separate. It runs every execution form the ad-hoc
`live-smoke` gate covers against one configured endpoint:

```powershell
$env:OPENAI_BASE_URL = "https://provider.example/v1"  # must implement the Responses API
$env:OPENAI_API_KEY = "..."
$env:OPENAI_MODEL = "model-name"
go run ./cmd/runtime-probe live-smoke > live-runtime-probe.json
```

Forms: `direct`, `child`, `team_parallel`, `team_vote`, `workflow`, `mailbox`.
Each form runs through Danny's authorized adapter, so the evidence records the
product path rather than a probe-only shortcut. `DANNY_LIVE_FORMS` restricts a
re-run to a subset (unknown names are rejected): `DANNY_LIVE_FORMS=direct,child`.
`DANNY_LIVE_OWNER_ROOT` overrides the per-run owner root, which defaults to
`.tmp/live-probe/<run-id>` and is never reused between runs.

The endpoint must implement the OpenAI Responses API (`/v1/responses`). The
pinned adapter profile declares `responses.v1`; a chat-completions-only
endpoint is rejected as `openai_endpoint_profile_unsupported_api_shape` before
any request is sent. `team_vote` requires the model to answer with a single
`approve`/`reject` token, because vote aggregation rejects prose.

A failing form marks its own events `failed-contract` and the run exits
non-zero, but the bundle for the forms that did pass is still written to
stdout. That keeps a partial gate diagnosable without letting it look like a
pass. Do not set `STELE_URL` for this command: the live-smoke Stele call does
not load a runtime credential, and a real service will reject it. Run
`stele-smoke` separately.

On 2026-11-05 this command was run with `OPENAI_BASE_URL`, `OPENAI_MODEL`,
and `OPENAI_API_KEY` unset. It returned `skipped-unconfigured` without
contacting a provider; the result is recorded as incomplete acceptance in
`testdata/runtimeprobe/baymax-live-smoke-unavailable.evidence.json`.

On 2026-11-12 it was run for real against an OpenAI-compatible gateway that
implements `POST /v1/responses` (`agnes-3.0-flash`). All six forms passed and
the bundle is committed as
`testdata/runtimeprobe/baymax-live-smoke.evidence.json`. The Stele case in that
bundle is `skipped-unconfigured`, so task 7.3 remains incomplete on the Stele
side.

### Provider compatibility shim

The 2026-11-12 run needed `DANNY_LIVE_PROVIDER_SHIM=stream-default`. That
gateway treats an **omitted** `stream` field as a streaming call and then tries
to JSON-decode the SSE body, answering `500 bad_response_body`
(`invalid character 'd' looking for beginning of value`). Omitting `stream`
means `false` in the Responses API and the pinned adapter omits it for
non-streaming requests, so this is a provider defect rather than a Danny or
Baymax one: the identical body with an explicit `"stream": false` succeeds.

The shim is a loopback reverse proxy inside the harness. It forwards to the real
endpoint untouched except for adding `"stream": false` when the field is absent;
a request that already carries a stream flag is unmodified. It is opt-in, and
when enabled the bundle records `"provider_compat": "stream-field-injected"`, so
a passing run never looks like a clean conformance run. The marker survives
redaction. Do not enable it against a conformant provider, and do not read a
passing bundle as proof that the gateway is spec-conformant.

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
correlation, sequence gaps, and duplicate terminal events. `provider_compat` is
an optional top-level field: absent means the provider was called as-is, a value
means the harness adapted something on the way to it. Before writing an
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
child dispatch, native parallel/vote teams, mailbox lifecycle, and
scheduler/context adapter calls through deterministic fixtures. Team evidence
includes execution-derived team/member identities, native member and team
cursors, inherited authority, native worker/timeout/cancellation controls,
vote provenance, and fail-closed aggregation. Stele persistence cases use the
real service and PostgreSQL system of record; they are never replaced with an
in-process store.

The repository contract suite can exercise the application durability boundary
against Docker PostgreSQL as well as SQLite:

```powershell
$env:DANNY_TEST_POSTGRES_URL = "postgres://stele:stele-local-only@127.0.0.1:5432/stele?sslmode=disable"
go test ./internal/storage -count=1
```

This checks Danny migrations, native evidence/cursor projection, checkpoint
authority, revision conflicts, scheduled effects, and outbox delivery. It is
not evidence that the PostgreSQL database is a pinned Stele service.

The latest committed Compose-backed Stele run is recorded at
`testdata/runtimeprobe/stele-compose.evidence.json`. That historical artifact
was captured against `github.com/FelixSeptem/stele
v0.0.0-20261007033528-a556ac0ddacc` and is not evidence for the current pin.
The current adapter is pinned to
`github.com/FelixSeptem/stele v0.0.0-20261111135338-19fc90134492`. Its public
turn-bound context contract is exercised by local adapter fixtures against
the refreshed `openapi.SpecYAML()`. The PostgreSQL/pgvector evidence in
`testdata/runtimeprobe/stele-f38016-*.evidence.json` belongs to the historical
`f38016e` pin and is not evidence for the current pin; task 7.3 still requires
a live service rerun on `19fc901`. The historical context,
basic memory append/query, fresh-adapter restart projection, and occurrence
replay results below remain historical context only. The provider principal UUID issue from
`STELE-002`, canonical path promotion issue from `STELE-003`, summary
canonical persistence issue from `STELE-004`, and provenance UUID issue from
`STELE-005` are fixed upstream. After the existing asynchronous governance
worker completes, the references case is `passed`: both task and run
exact-path reads return true, and the database preserves the non-root paths in
candidate and canonical rows.
The adapter uses Stele's documented `conversation.message` event type and
continues sending `input.top_k`; it does not maintain a local reference store
or rewrite paths. Because Stele governance runs asynchronously in the worker,
the live smoke verification waits for that existing worker projection before
performing the exact-path read.

The historical `7ffd30f9e635` context smoke was blocked by the disclosure route
gate; its evidence remains in `stele-7ffd-live.evidence.json` and its repair is
present in Stele commit `6d8983099147d79b432575347a1dfdf2abaf7fe7`. The prior
`b63151c0464d` context smoke first creates a server-issued Provider turn through
`POST /v1/provider/turns`, then performs an exact turn-bound
`POST /v1/provider/context` read with `continuity.mode=issue`, and finally
repeats it with `continuity.mode=compare`. Evidence records the turn ID,
source/application digests and continuity outcomes only; it never stores the
context body, query, credential or authorization headers. That pin's
`400 operation_failed` result is retained in
`testdata/runtimeprobe/stele-b631-live.evidence.json` and `STELE-007`.
The historical `f38016e` pin passed the original Stele assurance and Danny's
fresh PostgreSQL/pgvector end-to-end smoke; sanitized reports are in
`testdata/runtimeprobe/stele-f38016-*.evidence.json`, including
`stele-f38016-danny-smoke.evidence.json`. They are retained as historical
evidence and do not cover the current `19fc901` pin.
The `f38016e` pin additionally documents that events omitting
`source_metadata` default to `untrusted/restricted` and remain excluded from
public retrieve/context. Danny reference classifications must follow product
data policy rather than retrieval needs.

The compilation probe uses the same public event contract with a
reference-first `conversation.message` payload at
`knowledge/generations/<generation-id>`. It verifies the original event ID is
returned on replay and performs an exact-path lookup without copying artifact
bodies into Stele. On the clean pinned `a556ac0ddacc` Compose validation, the
API, replay, exact-path lookup, and asynchronous governance all passed. The
database showed processed raw events with no governance error, promoted
candidate memories, and active canonical memories. The probe and adapter do
not add a provenance rewrite, skip governance, or maintain a local reference
store; the upstream provenance fix is recorded as `STELE-005` in
`docs/upstream/stele-findings.md`.
