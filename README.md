# danny

Danny is a personal agent project built around Baymax as the agent runtime and
Stele as the context store. It ingests documents, compiles them into cited
knowledge generations, runs tasks through a planner with subagent, team,
workflow, mailbox, and scheduled modes, and exposes the result through a
local browser workbench.

Everything is local-first: the service binds to the loopback interface, the
storage is a SQLite file by default, and no container port is reachable
beyond the host.

## Status

Milestones 0 through 12 of `docs/roadmap.md` are complete. Twelve
capabilities are specified under `openspec/specs/`, the HTTP service, the
workbench, the Docker profiles, and the release gate are implemented, and the
release-gate evidence matrix is recorded in `docs/release-gate.md`.

Two roadmap scenarios stay `live-required` because they need a live upstream
deployment and a provider credential: the Stele preference round trip and the
Baymax live pause/restore gate. They are never reported as passing without
real evidence.

## Capabilities

| Domain | Capability | Specification |
| --- | --- | --- |
| Agent runtime and context | Runtime dependency probe, bounded context ownership | `openspec/specs/runtime-dependency-probe`, `openspec/specs/runtime-context-ownership-boundary` |
| Knowledge | Context assembly, compilation workflow, document ingestion and parsing boundary | `openspec/specs/knowledge-agent-context-assembly`, `openspec/specs/knowledge-compilation-workflow`, `openspec/specs/document-ingestion-parser-boundary` |
| Execution | Task execution planner | `openspec/specs/knowledge-agent-context-assembly` (planner delta) |
| Scheduling and delivery | Scheduler, mailbox, reliable delivery | `openspec/specs/scheduler-mailbox-reliable-delivery` |
| Storage | Durable storage, schema, recovery | `openspec/specs/storage-durability` |
| Browser boundary | AG-UI execution projection, restricted A2UI surface, HTTP API and workbench | `openspec/specs/agent-ui-protocol-boundary`, `openspec/specs/http-api-workbench` |
| Operations | Docker profiles, release validation | `openspec/specs/docker-operations`, `openspec/specs/release-validation` |

## Architecture

```text
browser workbench (web/, React + TypeScript + Vite)
  |
  |-- /agent/threads/{id}/runs/{id}/events   AG-UI SSE projection, durable replay
  |-- /agent/surfaces/{id}/actions            server-validated surface submissions
  |-- /api/*                                  read-only Danny-owned projections
  |
  v
assembled HTTP service (internal/server)  -- same-origin guard, loopback only
  |
  |-- knowledge: compiler, coordinator, navigation, storage committer
  |-- tasks: planner, harness, scheduler, mailbox, webhook worker
  |-- agent: Baymax execution, Stele context projection
  |
  v
durable store (internal/storage)  -- SQLite or PostgreSQL, schema v10
```

The boundaries are enforced, not decorative: the listener is loopback-only
unless authenticated mode is enabled, every state-changing request must carry
a same-origin signal, the dynamic-surface catalog is registered on the server,
and no browser-provided component data can become a mutation.

## Run it locally

### 1. The local script

Prerequisites: Go 1.26 or newer, Node.js 20 or newer (npm ships with it).

```powershell
pwsh -File .\scripts\start-local.ps1
```

The script checks the toolchain, builds the workbench assets into `web/dist`,
applies the local defaults, and starts the service in the foreground.

```powershell
pwsh -File .\scripts\start-local.ps1 -CheckOnly    # verify the prerequisites only
pwsh -File .\scripts\start-local.ps1 -SkipFrontend # reuse the assets already built
pwsh -File .\scripts\start-local.ps1 -Port 9090
```

Then open <http://127.0.0.1:8080/>.

### 2. Docker with SQLite

Prerequisite: Docker Engine with the Compose plugin.

```powershell
docker compose up -d --build
```

One command, no host Go or Node.js needed. The host port is published to
`127.0.0.1` only.

### 3. Docker with PostgreSQL

```powershell
docker compose -f compose.yaml -f compose.postgres.yaml up -d --build
```

The override starts a pinned PostgreSQL with a `pg_isready` health check and
gates the application on it, so migrations never race an unready database.
No database port is published to the host.

### 4. Front-end development mode

```powershell
cd web
npm ci
npm run dev
```

The Vite dev server proxies to the Go service, which must already be running
from step 1, 2, or 3.

## Configuration

The service reads `DANNY_*` environment variables. Environment values take
precedence over the documented defaults.

| Variable | Default | Meaning |
| --- | --- | --- |
| `DANNY_LISTEN_ADDRESS` | `127.0.0.1:8080` | Workbench listener. A non-loopback address is rejected unless `DANNY_AUTHENTICATED=true`. |
| `DANNY_DATA_DIR` | `data` | Root of the local data directory. |
| `DANNY_DOCUMENTS_DIR` | `data/documents` | Imported source documents. |
| `DANNY_ARTIFACTS_DIR` | `data/artifacts` | Derived artifacts, rebuildable from committed generations. |
| `DANNY_DATABASE_URL` | `data/danny.db` | SQLite path, or a `postgres://` URL when the PostgreSQL driver is selected. |
| `DANNY_STORAGE_DRIVER` | `sqlite` | `sqlite` or `postgres`. |
| `DANNY_WORKBENCH_DIR` | `web/dist` | Directory holding the built workbench assets. |
| `DANNY_MODEL_ENDPOINT` | empty | OpenAI-compatible model endpoint. |
| `DANNY_MODEL_NAME` | empty | Model name for that endpoint. |
| `DANNY_MODEL_API_KEY` | empty | Model credential, injected through the environment only. |
| `DANNY_CONTAINER_LISTEN` | `false` | Opt-in wildcard listener for the container profile. |
| `DANNY_AUTHENTICATED` | `false` | Authentication mode. Not implemented yet; it only widens the listener policy. |
| `DANNY_SCHEDULER_ENABLED` | `false` | Scheduler switch. |
| `DANNY_SCHEDULER_TIMEZONE` | `UTC` | Timezone for schedule evaluation. |
| `DANNY_PARSER_URL` | `http://parser:8090` | Parser service address on the Compose network. |

`DANNY_CONTAINER_LISTEN` exists because a published port reaches the
container's network interface, not its loopback. It accepts only a wildcard
address, and the Compose host publication stays bound to `127.0.0.1`.

## Repository layout

```text
cmd/personal-agent    the service entry point
cmd/runtime-probe     offline and live upstream probes
cmd/release-gate      the release-gate evidence command
internal/agent        Baymax execution and Stele context projection
internal/agentui      AG-UI projection and the restricted A2UI boundary
internal/knowledge    compilation, navigation, and source handling
internal/tasks        planner harness, scheduler, mailbox, webhook worker
internal/storage      durable store, schema, projections, repositories
internal/server       assembled HTTP service and the same-origin guard
parser_service        shared Python parser core and its HTTP adapter
web/                  React + TypeScript + Vite workbench
openspec/             proposals, specifications, and the roadmap contract
docs/                 design, contract, and operations documents
scripts/              repository scripts, including the local startup
testdata/             deterministic fixtures and recorded live evidence
```

## Verify it

```powershell
go build ./...
go vet ./...
go test ./... -count=1
golangci-lint run ./...
pwsh -File .\scripts\check-foundation.ps1
pwsh -File .\scripts\check-docs-consistency.ps1
go run ./cmd/release-gate
python parser_service\test_formats.py
```

`cmd/release-gate` runs the suite in both storage modes, runs the parser
format matrix, and writes the evidence matrix. A check that could not run is
reported as unverified with its reason and is never counted as passing.

## Documents

| Document | Contents |
| --- | --- |
| `docs/roadmap.md` | Milestones, deliverables, and exit gates. |
| `docs/release-gate.md` | The release-gate evidence matrix. |
| `docs/http-workbench.md` | HTTP routes, security model, and projection allowlists. |
| `docs/docker-operations.md` | Container build, topologies, health, backup, and recovery. |
| `docs/agent-ui-protocols.md` | AG-UI and A2UI compatibility, including the A2UI 1.0 status. |
| `docs/storage-durability.md` | Durability model, recovery, and the crash-recovery contract. |
| `docs/storage-schema.md` | The durable schema. |
| `docs/scheduler-reliable-delivery.md` | Scheduler, mailbox, and delivery guarantees. |
| `docs/task-execution-harness.md` | Planner modes, admission, and checkpoint authority. |
| `docs/runtime-context-ownership.md` | Baymax and Stele context ownership boundaries. |
| `docs/runtime-probe.md` | The upstream dependency probe. |
| `docs/parser-service.md` | The parser contract and its Docker profile. |
| `docs/upstream/` | Upstream findings with version, reproduction, evidence, affected task, and unblock condition. |

## Upstream policy

Baymax and Stele are pinned to exact pseudo-versions in `go.mod`, `go.sum`,
and `testdata/fixtures/manifest.json`. Any integration defect that needs an
upstream change is recorded under `docs/upstream/`, keeps the dependent work
blocked until the recorded reproduction passes against the upstream repair,
and is never worked around in production code.

## License

See `LICENSE`.
