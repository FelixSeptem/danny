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

The system SHALL define a TaskProfile containing objective, complexity, dependency graph, parallelism, evidence breadth, latency, risk, write scope, and budget signals. An ExecutionPlanner SHALL produce a versioned ExecutionPlan with selected mode, rationale, limits, capability digest, verified runtime capability-set digest, and stable plan digest. With the same normalized profile, planner version, and verified capability set, plan selection SHALL be deterministic. Modes `direct`, bounded `subagent_fanout`, `team_parallel`, `team_vote`, `workflow`, and `mailbox_async` SHALL be eligible only after their complete execution contracts pass the compatibility gate; scheduled requests SHALL retain occurrence authority and select a verified execution form.

#### Scenario: Equivalent profiles produce equivalent plans
- **WHEN** two planner calls receive equivalent normalized TaskProfiles, the same planner version, and the same verified capability set
- **THEN** they SHALL produce the same mode, limits, rationale, and plan digest

#### Scenario: Simple task selects direct mode
- **WHEN** a profile has low complexity, no independent fanout, and a bounded write scope
- **THEN** the planner SHALL select verified `direct` execution and record the rationale in the plan

#### Scenario: Independent bounded work selects subagent fanout
- **WHEN** a profile contains independent bounded units with sufficient budget and no cross-unit write dependency and the subagent contract is verified
- **THEN** the planner SHALL select `subagent_fanout` with an explicit child count, per-child budget, aggregation limit, and parent capability digest

#### Scenario: Unsupported mode is rejected
- **WHEN** a caller requests a mode whose required execution contract is unavailable or unverified
- **THEN** the planner SHALL return a typed unsupported-mode result and SHALL NOT silently downgrade or emulate that mode

#### Scenario: Task characteristics select a verified execution shape
- **WHEN** the profile calls for collaborative aggregation, conflicting evidence arbitration, dependent steps, or asynchronous continuation and the corresponding contracts are verified
- **THEN** the planner SHALL deterministically select the applicable team, workflow, or mailbox form and persist its rationale and limits

### Requirement: Context projections are bounded and role-specific

The system SHALL build a bounded context projection for every parent and child run through the Baymax Context Assembler boundary. A projection SHALL contain a prefix baseline, capability digest, task-aware Stele disclosure, pressure compaction result, and reference-first handoff data. Stele SHALL own memory selection and memory budget assembly; Baymax SHALL own final model-context assembly, history pressure handling, and native handoff mechanics. Danny SHALL authorize role disclosure and mark source, memory, and generated text untrusted before final assembly. Budget units, provenance, omissions, and the admitted projection digest SHALL be explicit; byte limits SHALL NOT be represented as verified token limits.

#### Scenario: Parent projection preserves the execution contract
- **WHEN** a coordinator run is admitted
- **THEN** its projection SHALL include the prefix baseline, plan digest, capability digest, task profile summary, and bounded Stele references and provenance without copying the complete Stele store

#### Scenario: Child projection inherits authorized constraints
- **WHEN** a subagent child is created from a parent projection
- **THEN** the child SHALL retain the authorized prefix and capability digest and SHALL have a bounded role-specific native handoff containing source references, child assignment, and the applicable projection digest

#### Scenario: Context pressure compacts safely
- **WHEN** the available model context budget is insufficient for the full authorized candidate projection
- **THEN** the Baymax assembler SHALL compact task-aware history while preserving citation and source references and SHALL record the resulting budget view with its units

#### Scenario: Role disclosure is bounded
- **WHEN** ContextReader, KnowledgeNavigator, SectionAnalyst, CitationValidator, or TaskMemoryInterpreter receives a projection
- **THEN** Danny SHALL enforce that role's authorized fields, Stele SHALL select permitted memory through verified controls, and omitted data SHALL be identified by authorized reference or non-disclosing diagnostics

#### Scenario: Reassembly cannot silently change admitted context
- **WHEN** a resumed run would retrieve a different context selection for the same admitted projection
- **THEN** it SHALL use a verified retained projection or enter an explicit context refresh/replan boundary rather than reuse the old digest for different content

### Requirement: Run and Stream share terminal semantics and event projection

The system SHALL map actual Baymax parent, child, tool, diagnostic, orchestration, safe-point, and terminal events into durable execution evidence with stable native and product correlation identifiers. Equivalent Run and Stream executions SHALL produce equivalent source-owned terminal status and aggregation semantics even when transport events differ. Event replay SHALL read durable evidence without dispatching execution or tools, and observer state SHALL remain distinct from execution state.

#### Scenario: Parent and child evidence are correlated
- **WHEN** a KnowledgeAgent dispatches a bounded child
- **THEN** durable native execution evidence SHALL link parent run, child run, plan version, applicable projection digest, and terminal outcome

#### Scenario: Run and Stream agree at completion
- **WHEN** equivalent input is executed through Baymax Run and Stream
- **THEN** the projected source-owned terminal status, logical result, aggregate outcome, and failure class SHALL agree

#### Scenario: Diagnostics preserve actionable references
- **WHEN** Baymax emits a diagnostic or tool failure
- **THEN** the execution record SHALL preserve its native diagnostic class, correlation ID, plan version, and referenced context or source identity without storing credentials or unredacted provider payloads

#### Scenario: Reconnect reads retained evidence
- **WHEN** an authorized observer resumes from a valid durable cursor
- **THEN** it SHALL receive retained evidence after that cursor and the same available native terminal outcome without invoking retry, resume, or any tool

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

The system SHALL persist the plan version, projection digest, correlation identifiers, terminal or pause reason, completed idempotent effect references, and a verified native checkpoint reference needed to resume a KnowledgeAgent run after restart. Danny SHALL validate application authority before Baymax restores runtime state through the applicable public checkpoint contract. A resumed run SHALL NOT silently expand tool authority, change plan version, or repeat completed effects. An application record alone SHALL NOT authorize restarting an uncertain execution from its beginning.

#### Scenario: Restart resumes a bounded run
- **WHEN** a process restarts after a checkpointed Agent run
- **THEN** Danny SHALL validate the persisted plan, context, effects, and checkpoint binding before Baymax resumes the runtime or reports a typed pause/recovery state

#### Scenario: Replan is explicit
- **WHEN** a task profile changes or a budget boundary requires replanning
- **THEN** the system SHALL create a new plan version with rationale and SHALL retain references to completed idempotent results and native recovery evidence from the previous version

#### Scenario: Native checkpoint is missing
- **WHEN** an interrupted execution has application authority records but no valid runtime checkpoint or reconciled terminal evidence
- **THEN** it SHALL enter a typed recovery-required state and SHALL NOT automatically rerun parent or child tools

### Requirement: Task execution is controlled by a durable harness

The system SHALL admit each task run through a product authority harness that persists the selected ExecutionPlan before dispatch, records parent and child correlation identifiers, and controls authorization, revision validation, approval, replan, and product commit transitions. Baymax SHALL own execution dispatch mechanics, orchestration, runtime checkpoints, safe points, retry, cancellation, timeout, and terminal outcomes through verified public contracts. The harness SHALL delegate each verified execution form to Baymax and SHALL reject unavailable forms explicitly. Scheduled admission SHALL retain the occurrence policy, schedule revision, and effect namespace before delegating to its selected execution form.

#### Scenario: Direct task is admitted and committed
- **WHEN** a low-complexity task profile selects verified `direct` execution and the run has an authorized policy and projection
- **THEN** the harness SHALL persist the plan before invoking the native execution boundary and SHALL conditionally commit one product result from correlated terminal evidence

#### Scenario: Bounded subagent task stays within the parent plan
- **WHEN** a task profile selects verified `subagent_fanout`
- **THEN** Baymax SHALL dispatch only the planned child count and budget, provide child terminal outcomes, and a failed or unauthorized required child SHALL block the product commit gate

#### Scenario: Unsupported execution mode is explicit
- **WHEN** a caller requests a workflow, mailbox, team, or scheduled execution path whose required contracts are unavailable or unverified
- **THEN** admission SHALL return a typed unsupported-mode result and SHALL NOT silently execute the task as `direct`

#### Scenario: Runtime success and product conflict remain distinct
- **WHEN** Baymax completes successfully but a concurrent update invalidates the task revision at commit
- **THEN** the harness SHALL retain the native success evidence and return a product revision conflict without repeating execution or overwriting the newer task

### Requirement: Task runs resume and replan only at authorized safe points

The system SHALL persist authority checkpoints containing run identity, plan version/digest, capability digest, projection digest, write scope, effect namespace, task revision, current step, pause reason, completed effect references, and the native checkpoint identity/version/digest. Baymax SHALL establish runtime safe points and restore runtime checkpoints. Danny SHALL validate checkpoint bindings and current authority before admitting resume; replan SHALL create a new plan version with a reason and preserve prior plans and completed effects. An application checkpoint and its pause transition SHALL become visible together, while reconciliation SHALL explicitly handle the non-atomic boundary with the upstream runtime store.

#### Scenario: Paused run resumes with the same authority
- **WHEN** a run pauses for `input_required`, `approval_required`, or `budget_exhausted` and the process restarts
- **THEN** the harness SHALL validate the application authority binding and native checkpoint and SHALL resume only when the requested plan, capability, projection, write scope, and effect constraints match

#### Scenario: Changed task revision triggers explicit replan
- **WHEN** a task revision changes before a safe point is resumed
- **THEN** the Agent SHALL reread the task, persist a new plan version with the changed profile digest and replan reason, and SHALL retain completed effect references and prior native checkpoint provenance

#### Scenario: Authority expansion is not implicit
- **WHEN** a replan would expand write scope, external side effects, or execution budget beyond the prior plan
- **THEN** the harness SHALL pause for the configured approval boundary and SHALL NOT dispatch the expanded plan before authorization

#### Scenario: Retry has causal identity and stable effects
- **WHEN** an authorized retry is admitted for a terminal or interrupted attempt through a supported native contract
- **THEN** the retry SHALL retain causal linkage, plan authority, and stable logical effect keys, use the native attempt/run identity rules, and leave the previous terminal evidence immutable

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
