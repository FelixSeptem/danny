## 1. Domain contracts and policy model

- [x] 1.1 Define KnowledgeAgent request, policy, capability, tool invocation, terminal, pause, and approval types; verify read, write, and scheduled policies have distinct capabilities and side-effect scopes.
- [x] 1.2 Define TaskProfile, ExecutionPlan, planner version, plan digest, capability digest, and typed unsupported-mode results; verify equivalent normalized profiles produce deterministic plan digests.
- [x] 1.3 Define durable execution evidence, checkpoint, correlation, diagnostic, and projection reference records against the Milestone 2 repositories; verify credentials and unredacted provider payloads are rejected from evidence.

## 2. Server-controlled tools and untrusted data boundary

- [x] 2.1 Implement the server-controlled tool registry with input schemas, capability requirements, write scopes, confirmation requirements, and occurrence constraints; verify unauthorized tools are rejected before adapter invocation.
- [x] 2.2 Add untrusted-data labels for imported source, knowledge artifacts, summaries, and model output; verify instruction-like text cannot authorize memory, task, schedule, webhook, Stele, or compilation mutations.
- [x] 2.3 Add approval-required and policy-violation terminal/pause events; verify protected writes do not execute before confirmation and policy failures are durably correlated.

## 3. Baymax and Stele projection adapters

- [x] 3.1 Extend the Baymax adapter boundary for authorized Run and Stream execution, Context Assembler input, child dispatch, and typed event projection; verify Run and Stream terminal semantics agree.
- [x] 3.2 Extend the Stele adapter boundary for bounded context disclosure, memory/reference lookup, projection digest, and reference-first handoff; verify the adapter does not copy the complete Stele store into a child projection.
- [x] 3.3 Implement prefix baseline, capability digest, task-aware disclosure, pressure compaction, budget view, and `handoff.v1`; verify role-specific projections preserve citations and source references under pressure.
- [x] 3.4 Add coordinator, ContextReader, KnowledgeNavigator, SectionAnalyst, CitationValidator, and TaskMemoryInterpreter projection fixtures; verify each role receives only its authorized fields.

## 4. Execution planner and KnowledgeAgent runtime

- [x] 4.1 Implement deterministic planner rules for `direct` and bounded `subagent_fanout`, including child count, budgets, aggregation limits, rationale, and plan digest; verify unsupported team, workflow, mailbox, and scheduled modes return typed results without downgrade.
- [x] 4.2 Implement KnowledgeAgent admission, policy binding, direct execution, and terminal event projection; verify parent run, plan version, capability digest, projection digest, and result are correlated.
- [x] 4.3 Implement bounded subagent fanout through Baymax composer/subagent dispatch; verify children inherit the authorized prefix and capability digest and cannot expand write authority.
- [x] 4.4 Persist checkpoints, pause reasons, plan versions, and idempotent effect references; verify a restart reconstructs authorized state and replanning creates an explicit new plan version.

## 5. Evidence and upstream validation

- [x] 5.1 Add deterministic fake-model and fake-context tests for direct, fanout, policy rejection, approval pause, context pressure, Run/Stream equivalence, and prompt-injection attempts; verify no side effect occurs through untrusted text.
- [x] 5.2 Add Baymax and Stele live integration fixtures using the pinned versions; verify missing or incorrect upstream contracts are recorded in the Chinese findings document and block the affected task without a workaround.
- [x] 5.3 Run the full unit, integration, provider-boundary, documentation, and OpenSpec validation suites; verify the Milestone 3 exit gate and no changes to user-owned `.tmp/` or `debug.log`.
