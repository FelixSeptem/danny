# Task Execution Harness

The task execution harness is the application-owned coordination boundary for
durable task runs. It persists the selected plan before calling Baymax and
keeps execution recovery independent from Baymax process state and logs.

## State and repository boundaries

SQLite is the default local store. PostgreSQL uses the same
`storage.ExecutionRepository` contract. The repository persists:

- the execution row and its optimistic revision;
- immutable plan notebook versions and replan lineage;
- safe-point checkpoints and pause reasons;
- replan history; and
- parent and child execution evidence keyed by stable effect keys.

Admission writes the execution and initial plan in one database transaction.
Terminal writes require the expected execution revision. A stale worker gets a
typed `storage.ErrRevisionConflict` and cannot overwrite a newer pause,
replan, or terminal result.

## Supported modes

The first harness implementation supports `direct` and bounded
`subagent_fanout`. The planner persists child count and per-child budget, and
the commit gate requires every planned child to finish successfully. A failed,
missing, or unauthorized child leaves the execution blocked.

`team_parallel`, `team_vote`, `workflow`, `mailbox_async`, and `scheduled`
return a typed unsupported-mode result until their roadmap changes add the
corresponding durable boundaries. They are never silently downgraded to
`direct`.

## Pause, resume, and replan

Checkpoints retain the run identity, plan version and digest, capability and
projection digests, current step, pause reason, and completed effect keys.
Resume validates those values before dispatch. A task revision change is
converted into an immutable replan version. If the replan expands write
scope, external effects, or budget, the execution enters
`approval_required` pause before it can dispatch.

An uncertain parent dispatch is recorded as `recovery_required`. The persisted
admission idempotency key and correlation identifiers allow a later recovery
worker to reconcile the external Baymax result without creating a second
application terminal result.

## Validation

The harness contract is covered by SQLite restart, optimistic concurrency,
idempotent evidence, planner, pause/resume, replan, fanout failure, and
unsupported-mode fixtures. PostgreSQL runs the same execution repository
contract when `DANNY_TEST_POSTGRES_URL` is configured.
