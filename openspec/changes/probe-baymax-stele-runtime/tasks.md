## 1. Dependency API inventory and probe contracts

- [ ] 1.1 Inventory the pinned Baymax public packages for Run, Stream, tools, composer/subagents, teams, mailbox, scheduler, and Context Assembler; record the resolved package paths and compile a minimal import probe without adding business logic.
- [ ] 1.2 Inventory the pinned Stele public packages for scoped contexts, memory, references, projection reconstruction, persistence, and idempotency; record the resolved package paths and compile a minimal import probe without adding local replacements.
- [ ] 1.3 Define dependency-neutral correlation, evidence, terminal, capability-status, and redaction types under `internal/runtimeprobe`; verify validation rejects missing IDs, duplicate terminal events, sequence gaps, and unredacted secret sentinels.
- [ ] 1.4 Define adapter interfaces and version-reporting contracts for `internal/agent/baymax` and `internal/context/stele`; verify application packages do not import vendor types outside those adapters.

## 2. Baymax adapter and deterministic probes

- [ ] 2.1 Implement the Baymax adapter constructor and configured model transport boundary against the pinned module; verify `go test` compiles the adapter without a live endpoint.
- [ ] 2.2 Implement parent Run and Stream probe cases with typed local tool invocation and ordered lifecycle events; verify deterministic fake-model tests produce matching terminal and aggregate evidence.
- [ ] 2.3 Implement bounded child/composer dispatch and parent-child correlation; verify the child cannot exceed the configured budget or capability digest in the contract test.
- [ ] 2.4 Implement team parallel and team vote probe cases; verify independent results, vote aggregation, and terminal evidence are recorded with the parent correlation ID.
- [ ] 2.5 Implement mailbox publish, lease, consume, ack, requeue, and dead-letter probe cases; verify the deterministic fixture records every state transition and rejects an invalid lease transition.
- [ ] 2.6 Implement scheduler lease and delayed-dispatch probe cases plus the Context Assembler projection case; verify prefix guard, handoff digest, and derived budget view are present in validated evidence.

## 3. Stele adapter and continuity probes

- [ ] 3.1 Implement the Stele adapter constructor and scoped context operations against the pinned module; verify a local persistence configuration can create and read one bounded context.
- [ ] 3.2 Implement long-term memory append/query and task/run reference operations; verify the same scope returns the recorded identifiers and values in a deterministic test.
- [ ] 3.3 Implement restart reconstruction using a fresh adapter instance over persisted Stele state; verify the bounded projection preserves scope, references, and correlation IDs.
- [ ] 3.4 Implement the occurrence-scoped scheduled memory-write probe; verify replaying the same idempotency key returns the original logical result without duplicate durable memory effects.

## 4. Evidence fixtures and offline runner

- [ ] 4.1 Add fake-model and fake-clock fixtures under `testdata/runtimeprobe` with stable IDs, timestamps, dependency versions, and expected capability outcomes; verify fixture hashes are deterministic after LF normalization.
- [ ] 4.2 Implement the offline probe runner that executes the Baymax and Stele deterministic cases without endpoint configuration; verify it completes without network access and emits a schema-valid evidence bundle.
- [ ] 4.3 Add correlation/evidence contract tests covering parent, child, team, mailbox, scheduler, context, memory, and terminal records; verify every non-parent record links to a valid ancestor.
- [ ] 4.4 Add redaction tests using sentinel credentials, authorization headers, prompt text, and raw provider payloads; verify serialized evidence excludes secrets and unbounded content while retaining digests.

## 5. Explicit live smoke path

- [ ] 5.1 Add a dedicated live smoke command that requires explicit endpoint and model configuration and selects a bounded parent Run/Stream plus Stele round trip; verify absent configuration reports `skipped-unconfigured` without network access.
- [ ] 5.2 Verify the configured live smoke path emits the same redacted evidence schema as offline probes and never prints credentials; use a controlled OpenAI-compatible endpoint or documented smoke fixture for the verification.

## 6. Upstream findings and blocking workflow

- [ ] 6.1 Add runtime-probe evidence documentation describing commands, fixture IDs, dependency versions, expected/observed behavior, and evidence locations; verify each capability result can be traced to one artifact.
- [ ] 6.2 For each deterministic Baymax contract failure, record the exact version, reproduction, evidence, affected roadmap tasks, upstream reference, and unblock condition in `docs/upstream/baymax-findings.md`; verify dependent tasks are marked `blocked-upstream` and no fallback implementation is introduced.
- [ ] 6.3 For each deterministic Stele context or idempotency failure, record the equivalent structured finding in `docs/upstream/stele-findings.md`; verify dependent tasks are marked `blocked-upstream` and no adapter-local deduplication masks the result.
- [ ] 6.4 Add regression fixtures for every recorded finding and a repair verification procedure; verify a finding changes to `fixed-upstream` only after the pinned repair passes the original reproduction and affected integration tests.

## 7. Runtime-probe exit gate

- [ ] 7.1 Run all deterministic Baymax and Stele probes, evidence validation, redaction tests, provider-boundary checks, `go test ./...`, and `go vet ./...` without a live model service; verify all required local capabilities pass or are explicitly blocked with evidence.
- [ ] 7.2 Run the opt-in OpenAI-compatible smoke path when configured and record its result separately from deterministic results; verify no unconfigured live call is attempted.
- [ ] 7.3 Confirm every Milestone 1 evidence item has a correlation record and that unresolved upstream findings block dependent roadmap tasks; verify the Milestone 1 exit report is complete and reviewable offline.
- [ ] 7.4 Run `openspec validate probe-baymax-stele-runtime --strict` and resolve every proposal, spec, design, or task validation error before implementation begins.
