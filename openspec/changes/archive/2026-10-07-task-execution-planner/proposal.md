## Why

Milestones 0 through 4 provide durable storage, a unified KnowledgeAgent, deterministic direct/subagent planning primitives, and compiled knowledge workflows, but task execution is not yet recoverable across process restarts. Task revision checks, plan versions, checkpoints, and replan behavior currently exist only as partial domain or in-memory foundations, so a real task run cannot be admitted, paused, resumed, retried, or safely committed from durable state.

This change turns those foundations into a persistent task execution harness while keeping Baymax responsible for execution mechanics and limiting the first implementation to the already supported `direct` and `subagent_fanout` modes.

## What Changes

- Add durable execution records and a plan notebook for plan versions, task profiles, checkpoints, pause reasons, evidence references, and replan history.
- Extend task execution persistence with SQLite/PostgreSQL repositories using the existing driver-neutral contract.
- Make planner decisions deterministic, versioned, digestable, and auditable from normalized task profiles.
- Implement a harness controller for admission, direct execution, bounded subagent fanout, safe-point checkpoints, bounded retry, pause, resume, replan, and commit gating.
- Connect task revision conflicts to Agent reread and replan behavior without overwriting newer task state.
- Enforce capability, projection, plan-version, budget, and occurrence-independent effect boundaries when a run resumes or replans.
- Return typed unsupported-mode results for `team_parallel`, `team_vote`, `workflow`, `mailbox_async`, and `scheduled` until their roadmap milestones are implemented.
- Add deterministic fake-executor, storage-driver, restart-recovery, concurrency, and planner decision fixtures.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `knowledge-agent-context-assembly`: extend the existing KnowledgeAgent planner and checkpoint contract with durable task execution records, harness control flow, safe-point resume, bounded replan, and persistent evidence.
- `storage-durability`: extend the storage contract with execution records, plan notebook entries, checkpoints, and conditional task execution mutations shared by SQLite and PostgreSQL.

## Impact

- Affected code: `internal/agent/knowledge`, `internal/agent/baymax`, `internal/storage`, task execution adapters, and deterministic fixtures.
- Affected persistence: execution, plan, checkpoint, evidence, and replan records will use the existing SQLite/PostgreSQL migration and transaction boundaries.
- Affected Baymax integration: the harness will call the existing public parent and child boundaries and will not reimplement Baymax runner, subagent, budget, terminal, or Context Assembler semantics.
- Affected Stele integration: resumed and replanned executions retain projection and reference digests through the existing Stele adapter; no duplicate context store is introduced.
- Compatibility: unsupported execution modes remain explicit typed results and are not silently downgraded or emulated.
