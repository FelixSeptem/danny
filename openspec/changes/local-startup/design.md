# Design: local-startup

## README structure

The README answers, in order: what the project is, what it currently
supports, how the pieces fit together, how to run it, how to configure it,
what the repository looks like, how to verify it, and where the deeper
documents live.

- **Position and status.** One paragraph on the project, then the current
  milestone state and the number of landed capabilities. The status line is
  derived from `docs/roadmap.md`; it is not a second source of truth.
- **Capability map.** Grouped by domain (agent runtime and context, knowledge
  and compilation, tasks, scheduling, storage, browser boundary, operations),
  each line naming the capability and pointing at the specification that owns
  it.
- **Architecture.** A short text diagram of the request paths: the browser
  workbench, the `/agent/` AG-UI and surface boundary, the `/api/` read-only
  projections, the domain services, and the durable store. It names the
  boundaries that exist (loopback-only, same-origin guard, restricted
  catalog) instead of implying a deployment story that does not exist yet.
- **Startup paths.** Three reproducible paths: the local script, the Docker
  SQLite topology, and the Docker PostgreSQL topology, plus the front-end
  development mode.
- **Configuration.** A table of the `DANNY_*` variables that the process
  actually reads, with their defaults.
- **Layout, verification, and documents.** The repository tree at a useful
  depth, the test and release-gate commands, and an index of `docs/`.

## The local startup script

`scripts/start-local.ps1` is the single local entry point.

```powershell
pwsh -File .\scripts\start-local.ps1            # build the workbench and start the service
pwsh -File .\scripts\start-local.ps1 -CheckOnly # verify the toolchain only
pwsh -File .\scripts\start-local.ps1 -SkipFrontend -Port 9090
```

Sequence:

1. Resolve the repository root and verify `go`, `node`, and `npm` are on
   `PATH`. A missing toolchain is a named, actionable error.
2. Unless `-SkipFrontend` is given, install the pinned front-end dependencies
   when `web/node_modules` is absent and run `npm run build`, which produces
   `web/dist`.
3. Apply the documented local defaults as process environment variables:
   `DANNY_LISTEN_ADDRESS=127.0.0.1:<port>`, `DANNY_DATA_DIR`,
   `DANNY_DOCUMENTS_DIR`, `DANNY_ARTIFACTS_DIR`, `DANNY_WORKBENCH_DIR`, and
   the SQLite database path.
4. Start `go run ./cmd/personal-agent` in the foreground and print the
   workbench URL, the health endpoints, and the stop instruction.

The script does not install a toolchain, does not change machine
configuration, and does not write outside the repository except for the
`data/` directory the service already uses. It follows the existing script
conventions: `CmdletBinding`, strict mode, a `-Root` parameter, and ASCII-only
source so Windows PowerShell 5.1 decodes it correctly.

In `-CheckOnly` mode the script performs the toolchain and workbench checks
and exits, which makes it usable as a preflight check in an automated setup.

## Documentation consistency

`scripts/check-docs-consistency.ps1` currently asserts that the required
documents and the fixture manifest exist and that the pinned dependency
versions are pseudo-versions. It gains one more assertion: every
`scripts/*.ps1` path referenced by `README.md` must exist in the repository.
A stale script reference therefore fails the documentation check instead of
misleading a reader at the moment they try to run it.
