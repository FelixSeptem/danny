## Why

The runtime probe has established that Baymax and Stele can provide the execution and context primitives required by the product, while Milestone 2 now supplies durable application state. The project needs a single policy-controlled KnowledgeAgent and bounded context projection layer before compilation, tasks, or scheduled instructions can safely use those primitives without leaking tools, copying unbounded memory, or allowing imported text to authorize side effects.

## What Changes

- Introduce one KnowledgeAgent domain boundary for read, write, and scheduled policies.
- Register tools from server-controlled capabilities and reject unauthorized or policy-incompatible calls.
- Add an ExecutionPlanner contract with task profiles, versioned plans, plan digests, and initial `direct` and bounded `subagent_fanout` modes.
- Adapt Baymax Context Assembler and Stele projections into bounded, role-specific context inputs for parent and child runs.
- Add coordinator, ContextReader, KnowledgeNavigator, SectionAnalyst, CitationValidator, and TaskMemoryInterpreter role projections.
- Project Baymax lifecycle events and diagnostics into durable execution records and Stele references.
- Treat retrieved source and generated knowledge text as untrusted data at the tool boundary, with tests proving it cannot authorize mutations.
- Preserve Baymax ownership of Run, Stream, subagent, and Context Assembler execution mechanics and Stele ownership of context continuity.

## Capabilities

### New Capabilities

- `knowledge-agent-context-assembly`: Unified KnowledgeAgent policies, capability-controlled tools, dynamic initial execution planning, bounded Stele context projections, Baymax event projection, and prompt-injection boundaries.

### Modified Capabilities

- None.

## Impact

- Affected code: `internal/agent`, `internal/context`, `internal/storage`, and new KnowledgeAgent/planner/projection packages.
- Affected integrations: pinned Baymax runner/composer and Context Assembler APIs; pinned Stele context and memory APIs.
- Affected persistence: execution records, plan versions, projection digests, and Stele reference metadata use the Milestone 2 repositories and outboxes.
- Affected security boundary: tool capabilities and side effects are server-controlled; imported source and generated knowledge are explicitly untrusted inputs.
- Initial execution scope is limited to direct runs and bounded subagent fanout; team, workflow, mailbox, and scheduled modes remain later roadmap milestones.
