# danny

Personal agent project built around Baymax as the agent runtime and Stele as the context store.

## Foundation configuration

The repository uses the fixed Go module path `github.com/FelixSeptem/danny`. Configuration is loaded from `DANNY_*` environment variables, with environment values taking precedence over documented local defaults.

The default local configuration uses SQLite at `data/danny.db`, stores documents and artifacts below `data/`, uses UTC scheduling, and binds the unauthenticated workbench to `127.0.0.1:8080`. A non-loopback listener is rejected unless `DANNY_AUTHENTICATED=true`; authentication and request-forgery protection are reserved for a later milestone.

Useful settings include:

- `DANNY_MODEL_ENDPOINT` and `DANNY_MODEL_NAME` for later Baymax runtime probes;
- `DANNY_STORAGE_DRIVER=sqlite|postgres`;
- `DANNY_DATABASE_URL` when PostgreSQL is selected;
- `DANNY_LISTEN_ADDRESS` and `DANNY_AUTHENTICATED` for the listener boundary;
- `DANNY_SCHEDULER_ENABLED` and `DANNY_SCHEDULER_TIMEZONE` for later scheduling work.

Baymax and Stele are pinned to the exact pseudo-versions in `go.mod`, `go.sum`, and `testdata/fixtures/manifest.json`. Any integration defect that requires an upstream change must be recorded in the dependency-specific document under `docs/upstream/` and keeps dependent work blocked until the recorded reproduction passes against the upstream repair. Production workarounds are not permitted.

Fixture hashes are computed after normalizing CRLF to LF, so deterministic evidence is independent of the host Git line-ending setting.

## Baseline checks

Run these commands from the repository root:

```powershell
gofmt -l (Get-ChildItem -Recurse -Filter *.go | ForEach-Object FullName)
go list -m
go list ./...
go test ./...
go vet ./...
golangci-lint run
pwsh -File .\scripts\check-provider-boundaries.ps1
pwsh -File .\scripts\check-docs-consistency.ps1
pwsh -File .\scripts\check-foundation.ps1
```

The foundation checks use deterministic fixtures and never contact a live model endpoint. The provider boundary check reserves model-provider imports for the future Baymax adapter package; application domain packages must not import provider SDKs directly.
