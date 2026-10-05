## Why

The repository currently contains only a minimal README, so there is no reproducible service boundary, dependency policy, configuration contract, or evidence process for validating Baymax and Stele. The first roadmap milestone must establish those foundations before runtime probes or product features can produce trustworthy results.

## What Changes

- Create the initial Go project structure for the application domains and service entry points.
- Establish `github.com/FelixSeptem/danny` as the exact Go module path used by the repository and all internal package imports.
- Define validated configuration for the OpenAI-compatible model endpoint, pinned Baymax and Stele dependencies, storage mode, data directories, scheduler settings, listener address, and Docker profiles.
- Enforce a loopback-only default for the unauthenticated local workbench; reject non-loopback binding unless a future authenticated mode is explicitly enabled.
- Add separate Baymax and Stele upstream findings documents with the `blocked-upstream` lifecycle and structured reproduction records.
- Add a reproducible dependency lock policy and baseline checks for formatting, static analysis, unit tests, and documentation consistency.
- Add a small deterministic fixture corpus for later runtime and recovery probes.

## Capabilities

### New Capabilities

- `project-foundation`: Reproducible project configuration, dependency governance, local-only access boundary, upstream finding records, and baseline validation commands.

### Modified Capabilities

None. The repository has no existing OpenSpec capability specifications.

## Impact

- Affected code: new Go module packages under `cmd`, `server`, `agent`, `context`, `knowledge`, `tasks`, `storage`, and `web`; initial configuration and validation packages.
- Affected documentation: `docs/upstream/baymax-findings.md`, `docs/upstream/stele-findings.md`, and project development guidance.
- Affected dependencies: exact Baymax and Stele versions or reviewable commits become pinned inputs; no provider SDK may be called directly by application domains.
- Affected operations: local development and future Docker profiles use the new configuration contract; the default HTTP listener and published port are loopback-only.
- This change does not implement Baymax runtime orchestration, Stele context operations, knowledge compilation, task execution, scheduling, or Web UI behavior. Those are later roadmap milestones.
