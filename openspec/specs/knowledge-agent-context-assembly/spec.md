# knowledge-agent-context-assembly Specification

## Purpose
This capability provides a single policy-controlled KnowledgeAgent that combines Baymax execution with bounded Stele context projections while preserving safe tool boundaries and deterministic execution planning.

## Requirements

### Requirement: Unified KnowledgeAgent policies control capabilities

The system SHALL expose one KnowledgeAgent domain boundary with explicit `read`, `write`, and `scheduled` policies. Each policy SHALL define the allowed tools, side-effect scope, context disclosure level, and whether human confirmation is required. Tool registration SHALL be server-controlled and SHALL reject calls that are absent from the policy capability digest.

#### Scenario: Read policy permits bounded retrieval
- **WHEN** a KnowledgeAgent runs with the `read` policy and requests an allowed retrieval tool
- **THEN** the tool SHALL execute with the policy's bounded context and SHALL not expose write, task, schedule, webhook, or compilation mutations

#### Scenario: Write policy requires its declared authority
- **WHEN** a KnowledgeAgent requests a mutation under the `write` policy
- **THEN** the server SHALL verify the tool capability and declared write scope before invocation and SHALL reject a tool outside that scope

#### Scenario: Scheduled policy is explicit
- **WHEN** a scheduled KnowledgeAgent is created
- **THEN** it SHALL carry a schedule occurrence identity, effect-key scope, policy digest, and the exact side-effect capabilities granted to that occurrence

### Requirement: Execution plans are deterministic and versioned

The system SHALL define a TaskProfile containing objective, complexity, dependency graph, parallelism, evidence breadth, latency, risk, write scope, and budget signals. An ExecutionPlanner SHALL produce a versioned ExecutionPlan with selected mode, rationale, limits, capability digest, and stable plan digest. The initial supported modes SHALL be `direct` and bounded `subagent_fanout`.

#### Scenario: Equivalent profiles produce equivalent plans
- **WHEN** two planner calls receive equivalent normalized TaskProfiles and the same planner version
- **THEN** they SHALL produce the same mode, limits, rationale, and plan digest

#### Scenario: Simple task selects direct mode
- **WHEN** a profile has low complexity, no independent fanout, and a bounded write scope
- **THEN** the planner SHALL select `direct` and record the rationale in the plan

#### Scenario: Independent bounded work selects subagent fanout
- **WHEN** a profile contains independent bounded units with sufficient budget and no cross-unit write dependency
- **THEN** the planner SHALL select `subagent_fanout` with an explicit child count, per-child budget, aggregation limit, and parent capability digest

#### Scenario: Unsupported mode is rejected
- **WHEN** a caller requests team, workflow, mailbox, or scheduled execution before its roadmap milestone is enabled
- **THEN** the planner SHALL return a typed unsupported-mode result and SHALL not silently downgrade or emulate that mode

### Requirement: Context projections are bounded and role-specific

The system SHALL build a bounded context projection for every parent and child run through the Baymax Context Assembler boundary. A projection SHALL contain a prefix baseline, capability digest, task-aware Stele disclosure, pressure compaction result, and reference-first handoff data. Each supported role SHALL receive only the fields authorized for that role.

#### Scenario: Parent projection preserves the execution contract
- **WHEN** a coordinator run is admitted
- **THEN** its projection SHALL include the prefix baseline, plan digest, capability digest, task profile summary, and bounded Stele references without copying the complete Stele store

#### Scenario: Child projection inherits authorized constraints
- **WHEN** a subagent child is created from a parent projection
- **THEN** the child SHALL retain the authorized prefix and capability digest and SHALL have a bounded role-specific handoff containing source references and the child assignment

#### Scenario: Context pressure compacts safely
- **WHEN** the available context budget is insufficient for the full candidate projection
- **THEN** the assembler SHALL compact task-aware history while preserving citation and source references and SHALL record the resulting budget view

#### Scenario: Role disclosure is bounded
- **WHEN** ContextReader, KnowledgeNavigator, SectionAnalyst, CitationValidator, or TaskMemoryInterpreter receives a projection
- **THEN** the projection SHALL contain only that role's authorized context fields and SHALL identify omitted data by reference rather than silently copying it

### Requirement: Run and Stream share terminal semantics and event projection

The system SHALL map Baymax parent, child, tool, diagnostic, and terminal events into durable execution evidence with stable correlation identifiers. Equivalent Run and Stream executions SHALL produce equivalent terminal status and aggregation semantics even when transport events differ.

#### Scenario: Parent and child evidence are correlated
- **WHEN** a KnowledgeAgent dispatches a bounded child
- **THEN** durable execution evidence SHALL link parent run, child run, plan version, projection digest, and terminal outcome

#### Scenario: Run and Stream agree at completion
- **WHEN** equivalent input is executed through Baymax Run and Stream
- **THEN** the projected terminal status, logical result, and failure class SHALL agree

#### Scenario: Diagnostics preserve actionable references
- **WHEN** Baymax emits a diagnostic or tool failure
- **THEN** the execution record SHALL preserve the diagnostic class, correlation ID, plan version, and referenced context or source identity without storing credentials or unredacted provider payloads

### Requirement: Untrusted context cannot authorize side effects

The system SHALL mark imported source text, retrieved knowledge text, generated knowledge text, and model-produced summaries as untrusted data. Tool authorization SHALL be decided by server-controlled policy and capability checks, not by instructions contained in those data fields. Write, task, schedule, webhook, Stele mutation, and compilation tools SHALL require an authorized capability and any configured confirmation or occurrence constraint.

#### Scenario: Instruction-like source text remains data
- **WHEN** a retrieved document contains text requesting `save_memory`, `create_task`, or another mutation
- **THEN** the Agent SHALL treat that text as untrusted content and SHALL not invoke the mutation without an independent authorized tool request

#### Scenario: Unauthorized tool call is rejected
- **WHEN** model output requests a tool absent from the active policy capability digest
- **THEN** the server SHALL reject the invocation, record a policy violation event, and preserve the run's terminal failure or approval-required state

#### Scenario: Confirmation is required for protected writes
- **WHEN** a tool is policy-allowed but marked confirmation-required
- **THEN** the Agent SHALL pause with an approval-required reason and SHALL not commit the side effect before confirmation

### Requirement: Agent state is resumable from durable checkpoints

The system SHALL persist the plan version, projection digest, correlation identifiers, terminal or pause reason, and idempotent effect references needed to reconstruct a KnowledgeAgent run after restart. A resumed run SHALL not silently expand its tool authority or change its plan version without an explicit replan record.

#### Scenario: Restart resumes a bounded run
- **WHEN** a process restarts after a checkpointed Agent run
- **THEN** the system SHALL reconstruct the authorized projection and resume or report a typed pause state using the persisted plan and correlation data

#### Scenario: Replan is explicit
- **WHEN** a task profile changes or a budget boundary requires replanning
- **THEN** the system SHALL create a new plan version with rationale and SHALL retain references to completed idempotent results from the previous version

### Requirement: Task execution is controlled by a durable harness

The system SHALL admit each task run through a harness that persists the selected ExecutionPlan before dispatch, records parent and child correlation identifiers, and controls admission, dispatch, checkpoint, retry, pause, resume, replan, and commit transitions. The first implementation SHALL execute `direct` and bounded `subagent_fanout`; `team_parallel`, `team_vote`, `workflow`, `mailbox_async`, and `scheduled` SHALL return typed unsupported-mode results until their roadmap milestones are enabled.

#### Scenario: Direct task is admitted and committed
- **WHEN** a low-complexity task profile selects `direct` and the run has an authorized policy and projection
- **THEN** the harness SHALL persist the plan before invoking the parent execution boundary and SHALL commit one terminal result with the plan and correlation digests

#### Scenario: Bounded subagent task stays within the parent plan
- **WHEN** a task profile selects `subagent_fanout`
- **THEN** the harness SHALL dispatch only the planned child count and budget, require child terminal outcomes, and SHALL keep a failed or unauthorized child from reaching the commit gate

#### Scenario: Unsupported execution mode is explicit
- **WHEN** a caller requests `workflow`, `mailbox_async`, or `scheduled` before its roadmap milestone is enabled
- **THEN** the harness SHALL return a typed unsupported-mode result and SHALL not silently execute the task as `direct`

### Requirement: Task runs resume and replan only at authorized safe points

The system SHALL persist checkpoints containing the run identity, plan version and digest, capability digest, projection digest, current step, pause reason, and completed effect references. A resumed run SHALL validate those values before dispatch, and a replan SHALL create a new plan version with a reason while retaining references to completed idempotent results and the previous plan.

#### Scenario: Paused run resumes with the same authority
- **WHEN** a run pauses for `input_required`, `approval_required`, or `budget_exhausted` and the process restarts
- **THEN** the harness SHALL reconstruct the checkpoint and SHALL resume only when the requested plan, capability, and projection constraints still match

#### Scenario: Changed task revision triggers explicit replan
- **WHEN** a task revision changes before a safe point is resumed
- **THEN** the Agent SHALL reread the task, persist a new plan version with the changed profile digest and replan reason, and SHALL retain completed effect references from the prior plan

#### Scenario: Authority expansion is not implicit
- **WHEN** a replan would expand write scope, external side effects, or execution budget beyond the prior plan
- **THEN** the harness SHALL pause for the configured approval boundary and SHALL not dispatch the expanded plan before authorization

### Requirement: Scheduled Agent runs retain occurrence-scoped authority

The system SHALL construct scheduled KnowledgeAgent runs with the persisted schedule occurrence, policy digest, capability digest, projection digest, plan version, and occurrence-scoped effect namespace. Retrieved source and Stele context SHALL remain untrusted data, and a retry SHALL not expand tool authority without an explicit approval boundary.

#### Scenario: Scheduled run carries occurrence identity
- **WHEN** a Baymax mailbox message starts a scheduled Agent run
- **THEN** the Agent request SHALL include the stable occurrence key and the exact schedule policy and capability digests used for admission

#### Scenario: Scheduled retry preserves authority
- **WHEN** a scheduled run resumes after a lease loss or process restart
- **THEN** the resumed run SHALL reuse the persisted plan, projection, capability, and occurrence effect namespace or enter an explicit replan or approval state

#### Scenario: Untrusted scheduled input cannot authorize mutation
- **WHEN** a scheduled instruction or retrieved document asks the Agent to perform an undeclared mutation
- **THEN** server-controlled policy SHALL reject the tool call and SHALL record a policy violation or approval-required outcome

### Requirement: Agent terminal status and delivery status remain separate

The system SHALL record the scheduled Agent terminal result independently from Stele projection and webhook delivery outcomes. A successful Agent run SHALL remain successful when delivery retries or terminal delivery failure occur, and delivery records SHALL retain the occurrence and execution correlations.

#### Scenario: Agent success with pending webhook
- **WHEN** the Agent commits success but a webhook delivery is pending
- **THEN** the execution SHALL remain successful and the delivery SHALL remain independently retryable

#### Scenario: Stele memory replay is correlated to execution
- **WHEN** a scheduled Agent retries a Stele memory write
- **THEN** the adapter SHALL send the same occurrence effect key and SHALL retain the execution and occurrence correlations in durable evidence

#### Scenario: Delivery failure does not authorize a second Agent run
- **WHEN** webhook delivery reaches terminal failure
- **THEN** the application SHALL expose the delivery failure without replaying the Agent execution as an implicit recovery action
