# local-startup

## Why

`README.md` still describes the Milestone 1 foundation: it lists the
`DANNY_*` settings and the baseline checks, and nothing else. The repository
has since delivered twelve capabilities — an AG-UI execution projection, a
restricted A2UI surface boundary, durable storage with a crash-recovery
contract, a scheduler and mailbox, an assembled HTTP API with a React
workbench, Docker profiles, and a release gate. The front door of the
repository does not mention any of it, so a reader cannot answer "what does
this project do now" or "how do I run it locally".

Local startup is also not a single command. Today the workbench needs a
front-end build (`npm ci && npm run build` under `web/`) before the Go service
can serve it, and the two steps, their prerequisites, and the environment
defaults live only in a reader's head.

## What Changes

- Rewrite `README.md` around the delivered system: what the project is, what
  it currently supports, how the pieces fit together, the repository layout,
  the configuration surface, three reproducible startup paths, the
  verification commands, and an index of the documents under `docs/`.
- Add `scripts/start-local.ps1`, a single local startup entry point: it checks
  the toolchain, builds the workbench assets, applies the documented local
  defaults, and starts the service. It supports a check-only mode and can skip
  the front-end build.
- Extend `scripts/check-docs-consistency.ps1` so a script referenced by the
  README must exist. A README that points at a renamed or deleted script fails
  the documentation check instead of quietly misleading a reader.
- Keep the README in English, matching the repository's other documents.

## Capabilities

### New Capabilities

- `local-startup`: a repository front door that describes the delivered
  capabilities and the verification surface, documents three reproducible
  startup paths, and provides a single script that starts the local service
  with its prerequisites checked.

### Modified Capabilities

- None. `project-foundation` is not changed; this change updates the
  repository documentation and adds a startup script without touching runtime
  behavior.

## Impact

- New files: `scripts/start-local.ps1`.
- Changed files: `README.md`, `scripts/check-docs-consistency.ps1`.
- No runtime, schema, or contract change: the script only orchestrates
  existing commands and environment defaults.
- No new dependency: the script uses the Go, Node, and npm toolchains the
  repository already requires.

## Non-goals

- No new feature, no change to the HTTP boundary, the AG-UI projection, or
  the surface gate.
- No cross-platform shell rewrite: the existing scripts are PowerShell, and
  the new one follows that convention.
- The script starts the service in the foreground; it is not a supervisor, a
  daemon, or a process manager.
- A2UI remains v0.9.1 only; `a2ui-1-0-upgrade` stays BLOCKED.
