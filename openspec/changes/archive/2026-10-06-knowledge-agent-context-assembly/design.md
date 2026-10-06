## Context

Milestones 1 and 2 provide real Baymax and Stele adapters plus durable application repositories, but there is not yet a product Agent boundary that controls tools, context disclosure, or execution shape. The existing Baymax adapter is intentionally probe-oriented and the Stele adapter exposes dependency operations; this change adds product orchestration without moving execution mechanics into Danny. See `proposal.md` and `specs/knowledge-agent-context-assembly/spec.md` for the observable contract.

## Goals / Non-Goals

**Goals:**

- Create one KnowledgeAgent boundary with explicit read, write, and scheduled policies.
- Make tool capability checks server-controlled and testable before invocation.
- Select direct or bounded subagent fanout deterministically from a normalized TaskProfile.
- Build bounded role-specific projections through Baymax Context Assembler and Stele references.
- Persist plan, projection, correlation, diagnostic, and checkpoint evidence using the storage foundation.
- Prove that untrusted source and generated text cannot authorize durable side effects.

**Non-Goals:**

- Implementing compilation, task workflows, scheduling, team aggregation, mailbox workers, or the HTTP workbench.
- Adding team, workflow, mailbox, or scheduled execution modes before their roadmap milestones.
- Copying the full Stele store into prompts or reimplementing Baymax Context Assembler, runner, subagent, or terminal-event mechanics.
- Adding a production workaround when Baymax or Stele lacks a required public contract.

## Decisions

### 1. Separate product policy from Baymax execution

KnowledgeAgent owns policy, tool capability digests, planner inputs, context requests, and durable evidence. Baymax owns Run, Stream, child dispatch, and Context Assembler execution. The adapter passes an authorized projection and receives typed events rather than recreating Baymax loops.

**Alternative considered:** build a Danny-specific runner around raw model calls. Rejected because it would duplicate Baymax semantics and invalidate the runtime evidence goal.

### 2. Use immutable capability and plan digests

Every admitted run records a policy capability digest, planner version, plan version, and plan digest. A child receives the parent's authorized prefix and capability digest as immutable constraints. Replanning creates a new plan version and records why it changed.

**Alternative considered:** infer authorization on each tool call from the latest configuration. Rejected because a long-running run could gain authority after admission and its evidence would no longer describe what was authorized.

### 3. Build projections as references first, content second

The projection pipeline starts with a small prefix baseline and capability digest, adds task-relevant Stele/KnowledgeStore disclosures, then applies pressure compaction and emits a reference-first `handoff.v1`. Large source content remains addressable through bounded read tools. This preserves citations and avoids copying every memory into every child.

**Alternative considered:** serialize the entire Stele context into each prompt. Rejected because it defeats bounded context assembly, increases injection surface, and prevents role-specific disclosure.

### 4. Treat all retrieved and generated text as untrusted

The context model labels source text, knowledge artifacts, summaries, and model output as data. Tool invocations are represented separately with server-created capability records. The policy gate evaluates tool name, capability digest, role, write scope, occurrence or confirmation requirement, and input schema before calling any adapter.

**Alternative considered:** rely on system prompt instructions saying that documents are untrusted. Rejected because prompt text alone cannot enforce side-effect authorization.

### 5. Start with direct and bounded subagent fanout

The planner initially supports direct execution and bounded subagent fanout. Fanout requires independent units, a parent budget, a child limit, a per-child budget, and an aggregation limit. Team, workflow, mailbox, and scheduled modes return typed unsupported results until their later contracts are implemented.

**Alternative considered:** expose every Baymax orchestration shape immediately. Rejected because planner semantics and durable recovery contracts for those shapes are roadmap work that has not yet been verified.

### 6. Persist execution evidence at application boundaries

The Agent writes plan and checkpoint evidence through storage repositories and uses existing outboxes for future Stele projection. Baymax events are correlated by parent, child, tool, plan, and projection IDs. Credentials and unredacted provider payloads are excluded from durable evidence.

**Alternative considered:** persist only the final answer. Rejected because dynamic planning, restart recovery, debugging, and upstream validation require intermediate evidence.

## Risks / Trade-offs

- [Risk] Planner rules can become opaque as task profiles grow. → Mitigation: version the rules, persist rationale and normalized profile, and require deterministic planner tests.
- [Risk] Bounded projections may omit context needed by a role. → Mitigation: preserve references and provide explicit bounded read tools; record omitted fields and projection digests for diagnosis.
- [Risk] Child fanout can multiply tool authority. → Mitigation: derive child capability digest from the parent intersection and reject any child expansion; test inherited prefix and digest equality.
- [Risk] Prompt injection may appear in summaries or generated artifacts. → Mitigation: label every text source as untrusted and enforce authorization outside model text at the tool gate.
- [Risk] Baymax or Stele APIs may not expose the required projection or event contract. → Mitigation: record a Chinese upstream finding and block the affected task; do not add a Danny workaround.

## Migration Plan

1. Add planner, policy, projection, and execution evidence types without changing existing runtime probe behavior.
2. Implement server-controlled tool registry and the direct execution path against the existing Baymax and Stele adapters.
3. Add Context Assembler projection and role-specific handoff fixtures, then add bounded subagent fanout.
4. Enable durable checkpoint and event projection after repository contract tests pass.
5. Roll back by disabling KnowledgeAgent admission while retaining probe and storage capabilities; no committed generation or durable execution record is deleted.

## Open Questions

- The exact Baymax Context Assembler constructor options and Stele projection endpoint fields can be finalized during implementation from the pinned dependency APIs, provided the projection and capability contracts remain unchanged.
- The initial planner threshold values can be tuned from deterministic fixtures without changing the supported mode set or plan digest semantics.
