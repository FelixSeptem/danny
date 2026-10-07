## ADDED Requirements

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
