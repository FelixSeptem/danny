# Tasks: local-startup

## 1. Local startup script

- [x] 1.1 Add `scripts/start-local.ps1` with `-Port`, `-SkipFrontend`, `-CheckOnly`, and `-Root` parameters, following the existing script conventions.
- [x] 1.2 Verify the toolchain (`go`, `node`, `npm`) and report a named error for a missing tool.
- [x] 1.3 Install the pinned front-end dependencies when `web/node_modules` is absent and build `web/dist`, unless `-SkipFrontend` is given.
- [x] 1.4 Apply the documented local defaults as process environment variables and start `go run ./cmd/personal-agent` in the foreground, printing the workbench URL and the health endpoints.
- [x] 1.5 Run the script in `-CheckOnly` mode and confirm it exits cleanly.

## 2. README rewrite

- [x] 2.1 Rewrite the opening: what the project is and what it currently supports, with the milestone state.
- [x] 2.2 Add the capability map, grouped by domain, each entry pointing at the owning specification.
- [x] 2.3 Add the architecture section with the request paths and the boundaries that exist.
- [x] 2.4 Add the three startup paths and the front-end development mode, including the script commands.
- [x] 2.5 Add the configuration table for the `DANNY_*` variables the process reads, with defaults.
- [x] 2.6 Add the repository layout, the verification commands, and the document index.

## 3. Documentation consistency

- [x] 3.1 Extend `scripts/check-docs-consistency.ps1` so a `scripts/*.ps1` path referenced by the README must exist.
- [x] 3.2 Run the documentation consistency check and confirm it passes.

## 4. Verification

- [x] 4.1 `gofmt -l`, `go build ./...`, `go vet ./...`, `go test ./... -count=1`, `openspec validate --all --strict`, `git diff --check`.
- [x] 4.2 Run `scripts/check-docs-consistency.ps1` and `scripts/start-local.ps1 -CheckOnly`.
- [x] 4.3 Confirm every command shown in the README exists in the repository.
