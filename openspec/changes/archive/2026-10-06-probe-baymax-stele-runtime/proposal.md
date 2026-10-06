## Why

The project foundation pins Baymax and Stele but has not exercised either dependency through its real API surface. The next milestone must produce repeatable runtime evidence before product domains depend on assumptions about Agent execution, context continuity, context assembly, or idempotent memory behavior.

## What Changes

- Add a minimal Baymax runtime adapter that exposes only probe-facing operations for parent Run and Stream, typed local tools, bounded child dispatch, teams, mailbox, scheduler, and Context Assembler evidence.
- Add a minimal Stele context adapter that exposes scoped context, memory, task/run references, bounded projection reconstruction, and scheduled-write idempotency probes.
- Define a shared correlation record linking a parent run to child runs, team runs, mailbox messages, schedule occurrences, context IDs, memory IDs, and terminal evidence.
- Add deterministic fake-model and fake-clock fixtures, plus an explicitly configured OpenAI-compatible live smoke command that uses the real Baymax and Stele adapters.
- Record verifiable results and upstream failures in dependency-specific findings documents. A required upstream repair marks dependent probe and later roadmap work `blocked-upstream`; no production workaround is added.

## Capabilities

### New Capabilities

- `runtime-dependency-probe`: Deterministic and live evidence that the pinned Baymax and Stele versions provide the required runtime, correlation, context, and idempotency contracts.

### Modified Capabilities

None. `project-foundation` already governs configuration, dependency pinning, and findings lifecycle; this change exercises those contracts without altering them.

## Impact

- Affected code: `internal/agent/baymax`, `internal/context/stele`, `internal/runtimeprobe`, configuration support for probe execution, and a dedicated probe command.
- Affected dependencies: direct integration imports from the pinned Baymax and Stele modules; application domains remain isolated from provider SDKs.
- Affected tests and evidence: deterministic fake-model, fake-clock, restart, and correlation fixtures; opt-in live OpenAI-compatible smoke test.
- Affected documentation: runtime-probe evidence guide and entries in `docs/upstream/baymax-findings.md` or `docs/upstream/stele-findings.md` when a deterministic defect is observed.
- This change does not implement KnowledgeAgent business policies, persistent product repositories, knowledge compilation, task scheduling features, HTTP APIs, Web UI, Docker, or production worker processes.
