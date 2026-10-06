# Storage durability

Milestone 2 uses the configured database as the authority for committed application state.

## Drivers

The local profile uses SQLite:

```powershell
$env:DANNY_STORAGE_DRIVER = "sqlite"
$env:DANNY_DATABASE_URL = "data/danny.db"
go run ./cmd/personal-agent
```

PostgreSQL uses the same repository contract and migration set:

```powershell
$env:DANNY_STORAGE_DRIVER = "postgres"
$env:DANNY_DATABASE_URL = "postgres://user:password@127.0.0.1:5432/danny?sslmode=disable"
go run ./cmd/personal-agent
```

Startup applies versioned migrations and then reconciles committed generations before any future durable workers are allowed to run. A migration or reconciliation error fails closed.

## Transaction boundaries

State changes that require external work write a durable outbox row in the same database transaction. The three outbox kinds are:

- `schedule_dispatch`: dispatch to a Baymax mailbox;
- `stele_projection`: projection into Stele context;
- `webhook_delivery`: notification delivery.

Outbox rows have a stable idempotency key, payload version, retry state, and lease ownership. A publisher may repeat delivery after a crash; downstream adapters receive the same key for every attempt.

Mutable entities use an expected revision. A stale write returns a revision conflict and cannot overwrite a newer update. Worker leases use a fencing value so an expired worker cannot commit after a replacement owner claims the work.

## Knowledge generations

Source bytes, generated artifacts, lineage, and manifests are committed in the database. A generation is visible only after its commit marker is durable. Files under the artifact directory are derived materialization. Removing the directory is recoverable:

```powershell
go run ./cmd/personal-agent
```

Startup reconciliation rebuilds materialized artifacts from every committed generation manifest. It does not publish staged generations.

## Verification

Run the SQLite contract suite with:

```powershell
$env:GOCACHE = (Join-Path (Get-Location) '.go-cache')
$env:GOMODCACHE = (Join-Path (Get-Location) '.go-mod-cache')
go test ./internal/storage -count=1
```

The same repository contract suite is intended to run against a disposable PostgreSQL database when the PostgreSQL profile is available. Baymax mailbox and Stele projection behavior remains owned by their adapters; a missing upstream acknowledgement or idempotency contract must be recorded in the corresponding `docs/upstream/*-findings.md` file and blocks the dependent worker.
