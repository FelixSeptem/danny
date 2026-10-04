# Personal Knowledge and Task Agent Design

**Date:** 2026-10-04  
**Status:** Approved design

## Goal

Build a locally deployable personal knowledge and task agent that uses [Baymax](https://github.com/FelixSeptem/baymax) as the Agent runtime and [Stele](https://github.com/FelixSeptem/stele) as the context store. The first release must validate both repositories in real workflows rather than through isolated demos.

The product shape follows [OpenKB](https://github.com/VectifyAI/OpenKB): a browser-based knowledge workbench that imports documents, compiles durable knowledge artifacts, supports grounded conversations with citations, and exposes task and scheduled-agent capabilities.

## Product scope

The first release supports one local user and provides:

- A local Web workbench with knowledge, chat, tasks, scheduled tasks, and notifications views.
- Markdown and plain-text document ingestion from a configured local directory.
- Incremental document processing based on file identity and content changes.
- OpenKB-style compiled knowledge artifacts: source documents, summaries, concepts, entities, section trees, and a navigable index.
- A single Baymax `KnowledgeAgent` with explicit read, write, and scheduled execution policies.
- Baymax multi-agent collaboration for real workflows: coordinator runs, bounded subagents, parallel and voting teams, durable mailbox delivery, scheduler leases, retry, and dead-letter handling.
- Stele-backed conversation context, durable memories, task context, compile context, and Agent run events.
- Ordinary task management through conversation and the Web interface.
- Fixed-action scheduled tasks and natural-language scheduled Agent instructions.
- Web notifications and optional webhook delivery.
- Docker Compose deployment with SQLite by default and PostgreSQL as a configuration-selected alternative.
- Evidence-driven integration and end-to-end scenarios that exercise Baymax and Stele together.

The first release does not include multi-user authentication, cloud hosting, external web crawling, PDF or Office parsing, calendar synchronization, email/enterprise chat notifications, or autonomous background behavior outside explicitly configured schedules.

## Design principles

1. **Compile knowledge before relying on retrieval.** Imported documents become durable, browsable artifacts that accumulate cross-document understanding.
2. **Use Agent navigation for questions.** Querying follows the knowledge index, summaries, concepts, entities, links, and source sections instead of depending only on a single top-k vector lookup.
3. **Converge read and write capabilities in one domain Agent.** Query and compilation are use cases of the same Knowledge Agent because compilation must read existing knowledge before deciding what to create or update.
4. **Make write authority explicit.** Ordinary chat is read-only. Knowledge writes require a write or scheduled policy, a validated plan, and an atomic commit.
5. **Keep context and knowledge separate.** Stele stores Agent context and memory; KnowledgeStore stores durable knowledge artifacts and their source lineage.
6. **Keep infrastructure replaceable.** Local files and SQLite are the default; PostgreSQL is a supported deployment mode behind the same domain interfaces.
7. **Make every answer traceable.** Knowledge-derived responses carry source paths and section or page references where available.

## Architecture

```text
Browser
   │
   ▼
Web UI ─────────► HTTP API (app)
                    │
          ┌─────────┼─────────┐
          ▼         ▼         ▼
   KnowledgeAgent  Stele   Scheduler
      (Baymax)    Context    Worker
          │         │         │
          └─────────┴─────────┘
                    │
          KnowledgeStore + AppStore
          SQLite or PostgreSQL
                    │
             Documents and artifacts
```

The codebase is organized into focused modules:

- `web`: browser workbench for knowledge, chat, tasks, schedules, notifications, and source citations.
- `server`: HTTP API, streaming responses, request validation, and application-level authorization boundaries.
- `agent`: Baymax runtime adapter, KnowledgeAgent construction, policy-controlled tool registration, Agent run events, and streaming event translation.
- `context`: Stele adapter and context lifecycle. It maps conversations, memories, task context, compile context, and run events without exposing Stele-specific data structures to the rest of the application.
- `knowledge`: document scanning, normalization, incremental compilation, artifact navigation, citation resolution, and KnowledgeStore interfaces.
- `tasks`: task state transitions, scheduled-task definitions, execution leases, execution history, and notification generation.
- `storage`: database interfaces, SQLite/PostgreSQL implementations, migrations, and transaction helpers.
- `cmd`: application and scheduler entry points.

The Baymax integration is intentionally library-first. The application owns model credentials, HTTP persistence, domain authorization, and the mapping from product operations to Baymax runs. Baymax owns Agent execution, child-run limits, team aggregation, mailbox lifecycle, scheduler coordination, and structured run events.

## Baymax deep-integration contract

Baymax is a required runtime dependency and the core mechanism by which this project validates real Agent behavior. The implementation must not reduce Baymax to an OpenAI-compatible model client, a prompt helper, or an optional adapter hidden behind an application-owned orchestration loop.

The following product paths must invoke Baymax's actual runtime and orchestration packages:

- interactive Agent execution through Baymax Run/Stream semantics;
- tool calls and typed tool lifecycle events through Baymax's tool contract;
- child execution through Baymax composer/subagent dispatch with parent-child budgets and depth limits;
- parallel, serial, and vote aggregation through Baymax `teams`;
- ordered and checkpointed compilation through Baymax `workflow`;
- durable cross-process work through Baymax `mailbox` command/event/result envelopes, leases, ack, requeue, and DLQ;
- timed execution through Baymax `scheduler` lease, retry, QoS, and delayed-dispatch semantics;
- context projection through Baymax `context/assembler`, including prefix guards, staged disclosure, pressure handling, handoff, and derived budget views;
- operational evidence through Baymax timeline, diagnostics, and runtime recorder integration.

The application may wrap these APIs with domain services, but it must not create parallel implementations of:

- Agent run or stream loops;
- subagent lifecycle or parent-child budget accounting;
- team state, vote resolution, or aggregation counters;
- mailbox delivery state, consumer lease, retry, or dead-letter transitions;
- scheduler claim, lease expiry, retry ownership, or delayed dispatch;
- Context Assembler pressure and handoff state;
- Baymax terminal outcome or event ownership.

The model provider remains configurable through the selected OpenAI-compatible endpoint, but provider configuration is injected into Baymax's model adapter. The app does not call the provider SDK directly from knowledge, task, scheduler, or Web handlers.

## Upstream findings and feedback loop

Baymax and Stele are active upstream dependencies whose real integration may expose bugs, missing contracts, performance bottlenecks, or useful optimization opportunities. These findings must remain visible and actionable for their maintainers.

The project maintains separate living documents:

- `docs/upstream/baymax-findings.md`
- `docs/upstream/stele-findings.md`

Each finding uses the same structured record:

```markdown
## BAYMAX-YYYY-MM-DD-short-slug

- Status: observed | upstream-issue-filed | blocked-upstream | fixed-upstream | closed
- Dependency version/commit: exact module version or commit
- Discovered in: application workflow and test name
- Capability: runtime, tool, subagent, teams, workflow, mailbox, scheduler, context, memory, or diagnostics
- Symptom: concise actual behavior
- Expected: behavior required by this project
- Minimal reproduction: exact command, fixture, or test case
- Evidence: logs, event timeline, correlation IDs, and relevant output
- Impact: affected user flow and severity
- Local handling: diagnostics or a test-only reproduction aid. Production workarounds are not permitted for findings that require an upstream behavior change.
- Proposed upstream change: smallest maintainable fix or optimization
- Validation: regression test and expected result after the fix
- Upstream reference: issue, PR, or discussion URL when available
- Unblock condition: exact upstream version or commit and verification required before dependent work resumes
```

The implementation workflow is:

1. capture the exact dependency version and failing scenario;
2. reduce the behavior to a deterministic fixture or integration test;
3. add a record to the dependency-specific findings document;
4. decide whether the behavior is an upstream defect or an upstream capability/optimization required by the current task;
5. when an upstream change is required, mark the finding and every dependent implementation task `blocked-upstream`; do not merge a production workaround or silently weaken the requirement;
6. keep the deterministic regression fixture and upstream-facing explanation in this project while the task is blocked;
7. after the upstream fix is available in a released version or an explicitly pinned, reviewable commit, rerun the fixture, update the record to `fixed-upstream`, and unblock the dependent tasks;
8. close the finding only after the real integration path is verified against the repaired dependency.

Findings are linked from the relevant integration module, blocked task, and release notes when they affect behavior. A discovery is not considered complete until the reproduction, upstream-facing explanation, blocked task reference, and unblocking condition are present. The documents are intentionally separate so Baymax and Stele maintainers can consume only the findings for their repository.

### Required integration evidence

The first implementation is not considered a Baymax integration until the end-to-end evidence includes:

1. a real Baymax parent run with streamed events;
2. at least one bounded child run created through Baymax dispatch;
3. a real Baymax team path with parallel or vote aggregation;
4. a mailbox message that is published, consumed under lease, acknowledged, and requeued or dead-lettered in a failure case;
5. a scheduled Baymax dispatch that survives worker restart;
6. a Context Assembler projection and handoff digest attached to parent and child runs;
7. Baymax timeline/diagnostic records correlated with Stele context and application execution records;
8. Run and Stream paths that preserve equivalent terminal and aggregation semantics.

These checks are contract tests against the pinned Baymax version and are run with a deterministic fake model plus a live OpenAI-compatible smoke configuration. The fake model verifies orchestration semantics; the live smoke path verifies provider wiring and actual model/tool interaction.

## Multi-agent collaboration model

The project uses Baymax's orchestration primitives as product behavior. They are not optional wrappers kept for a later scale-out phase.

```text
Knowledge Coordinator (parent run)
        │
        ├─ mailbox command: context.read
        ├─ mailbox command: knowledge.navigate
        ├─ mailbox command: task.inspect
        │       │
        │       └─ bounded subagents
        │
        ├─ teams.parallel: collect independent findings
        ├─ teams.vote: resolve conflicting summaries or citations
        ├─ workflow: validate → authorize → commit
        └─ mailbox result/event: progress, retry, terminal outcome
```

### Dynamic execution planning

Task execution does not select a Baymax orchestration primitive statically. Each user task, compilation request, and scheduled instruction first passes through an `ExecutionPlanner` that chooses the smallest suitable execution shape from the task's observed characteristics.

The planner receives a normalized `TaskProfile`:

- objective and requested outcome;
- estimated complexity and number of distinct work items;
- dependency graph and whether work can run independently;
- evidence breadth, source count, and expected context size;
- freshness and latency requirements;
- failure sensitivity and whether partial results are acceptable;
- required write capabilities and approval requirements;
- current parent/child budget, concurrency limit, and model availability;
- whether the task is interactive, scheduled, or a recovery attempt.

It emits a versioned `ExecutionPlan` before dispatch. The plan contains the selected mode, rationale, task decomposition, agent roles, budgets, timeout, retry policy, aggregation rule, write authority, and fallback mode. The plan and its digest are stored in Stele and the application execution record before work starts.

The initial mode set maps directly to Baymax capabilities:

| Mode | Selection signal | Baymax shape |
|---|---|---|
| `direct` | Small, single-step, low-risk task with no independent work | One parent Agent run |
| `subagent_fanout` | A few independent bounded subtasks with one clear owner | Coordinator plus bounded child runs |
| `team_parallel` | Several peer tasks can run concurrently and partial failure is tolerable | `teams` with `parallel` strategy |
| `team_vote` | Multiple analyses must choose or validate one result | `teams` with `vote` strategy |
| `workflow` | Steps have ordering, branching, checkpoints, or a commit gate | Baymax `workflow` |
| `mailbox_async` | Work may outlive the request, needs durable delivery, or crosses process boundaries | Mailbox command/event/result with leased workers |
| `scheduled` | Work is triggered by a cron occurrence | Baymax scheduler plus one of the modes above |

Selection is deterministic for equivalent `TaskProfile` inputs. The default planner uses explicit thresholds and constraints; an optional planner Agent may propose a plan for ambiguous tasks, but the proposal is validated against the same domain rules before it can be used. A model cannot grant itself additional write authority, exceed parent budgets, or select a mode that the runtime has disabled.

The planner supports bounded replanning at declared safe points:

- after task decomposition, when the number of discovered work items differs materially from the estimate;
- after team collection, when evidence conflicts or a required validator is unavailable;
- before commit, when validation finds unsupported claims or missing sources;
- after a retryable infrastructure failure, when the original mode cannot make progress.

Replanning creates a new plan version linked to the previous version. It must preserve completed idempotent results, explain the trigger, recalculate remaining budget, and pass authorization again if capabilities become broader. The planner cannot silently switch from read-only to write mode or from interactive execution to a durable background task.

Representative decisions:

- “Remember this preference” → `direct` with a memory write capability.
- “Compare two concepts from three documents” → `team_parallel`, followed by `team_vote` if claims conflict.
- “Compile a new document and update cross-document concepts” → `workflow` with section subagents, parallel analysis, citation validation, and an atomic commit step.
- “Every morning summarize my open tasks” → `scheduled` selecting `subagent_fanout` when task/context reads are independent, or `direct` when the context is small.
- “Process a large directory and continue after the browser closes” → `mailbox_async` with leased child work and a terminal aggregation event.

### Harness-inspired control loop

The planner is embedded in a harness-like control loop that separates task control from Agent execution. The loop is intentionally explicit so a complex task can be inspected, paused, resumed, or safely replanned.

```text
request
  │
  ▼
admission and intent normalization
  │
  ▼
TaskProfile + ExecutionPlan v1
  │
  ▼
checkpointed dispatch through Baymax
  │
  ├─ subagents / teams / workflow / mailbox
  ├─ Context Assembler projections per child
  └─ structured events and tool outcomes
  │
  ▼
observe evidence at safe point
  │
  ├─ continue current plan
  ├─ replan with version v2+
  └─ pause for user input or authorization
  │
  ▼
validate result and commit domain effects
```

The harness responsibilities belong to the application `execution` module:

- **Admission:** validate the request, storage/model readiness, policy, write authority, and parent budget before dispatch.
- **Plan notebook:** persist plan versions, step status, checkpoints, decision rationale, and pending user questions. This is a control record, not a second task state machine.
- **Execution controller:** instantiate the selected Baymax primitive and pass the plan's bounded child and tool capabilities.
- **Event stream:** consume Baymax run, team, mailbox, scheduler, and tool events and project them to the Web UI and Stele context.
- **Safe-point controller:** allow continuation, bounded retry, pause, recovery, or replan only at declared workflow boundaries.
- **Commit gate:** validate citations, artifact shapes, task transitions, and authorization before applying durable domain changes.

Baymax remains the owner of execution mechanics and terminal run semantics. The harness does not reimplement Baymax's runner, team state machine, mailbox lifecycle, scheduler lease state, or budget accounting. It stores references and control projections that make those facts usable by the product.

The harness supports explicit pause reasons such as `input_required`, `approval_required`, `budget_exhausted`, `provider_unavailable`, and `context_rebuild_required`. A paused run can resume from its last checkpoint with the same plan version, or resume under a new plan version after the planner records the changed assumptions. User approval is required only when a replan expands write scope, external side effects, or execution duration beyond the request's original authority.

### Planner evidence and evaluation

Planner decisions are evaluated against a versioned task corpus rather than judged only by final text. Each case records the expected minimum viable mode, allowed fallbacks, maximum fan-out, budget envelope, and whether a replan is expected. The integration suite verifies that equivalent profiles choose equivalent modes, high-risk tasks do not bypass the commit gate, and a failed child causes the declared retry or fallback behavior.

### Coordinator

Every interactive query, compilation, or scheduled instruction starts with a parent Knowledge Coordinator run. It owns the user-facing outcome, correlation ID, policy, child budget, and final aggregation. It never treats a child result as complete until the child's terminal result has been acknowledged.

### Subagents

Subagents are used for bounded, independent work:

- `ContextReader`: retrieves relevant Stele memories, conversation facts, and task context;
- `KnowledgeNavigator`: walks the index, summary, concept, entity, and source-section graph;
- `SectionAnalyst`: summarizes one document section and extracts candidate concepts/entities;
- `CitationValidator`: checks that proposed claims have valid source references;
- `TaskMemoryInterpreter`: identifies task updates and explicit memory requests.

Each child receives a narrowed context projection, a timeout, a maximum token or execution budget, and a parent run reference. Child depth and active-child limits are enforced through Baymax's subagent safeguards. A child result is a typed finding, not an untrusted free-form instruction.

### Agent teams

Baymax `teams` is used where independent findings need controlled aggregation:

- `parallel` for section analysis, context retrieval, and query evidence collection;
- `serial` for dependent compilation stages where later work requires the prior artifact;
- `vote` for citation conflicts, concept merge decisions, and validation of a proposed compilation plan.

Team tasks have stable IDs, roles, priorities, failure policy, and correlation fields. The coordinator records team totals, failures, cancellations, and the chosen aggregation outcome in the Agent run and Stele context.

### Mailbox

Baymax `mailbox` is the command/event/result transport between a parent run, child workers, and the scheduler worker. Envelopes include message ID, idempotency key, correlation ID, source and target Agent, task/run/team IDs, payload, not-before time, expiry, and attempt count.

Mailbox behavior used by the MVP:

- command publish for child dispatch and scheduled execution;
- event publish for progress and lifecycle updates;
- result publish for typed child and team outcomes;
- consumer leases and heartbeats for long-running analysis;
- ack on successful handling;
- requeue on transient model or storage failures;
- dead-letter handling after the configured attempt budget;
- query and snapshot support for the Web run inspector.

The default mailbox backend is Baymax's file backend under the mounted data directory so local Docker restarts can recover in-flight work. An in-memory backend is used only by unit tests. The application storage mode remains independently configurable as SQLite or PostgreSQL.

### Scheduler and delayed dispatch

The product scheduler owns the user-facing schedule definition and occurrence records. At trigger time it publishes a delayed or due mailbox command. A worker claims the command with a lease and starts a parent scheduled run. This uses Baymax scheduler semantics for lease, retry, QoS, and DLQ while the application database remains the source of truth for schedule metadata and Web history.

### Workflow and recovery

Compilation uses an explicit workflow:

1. normalize and hash sources;
2. fan out section subagents;
3. collect and aggregate findings;
4. run concept/entity and citation validation teams;
5. prepare a compilation plan;
6. authorize and commit the plan;
7. publish the terminal result and update Stele.

The workflow carries parent and child correlation IDs, supports bounded retries, and records checkpoint state. A process restart resumes or reclaims work through mailbox leases and the application execution record. The commit stage remains idempotent so replay cannot publish duplicate artifacts.

### Collaboration observability

The Web run inspector exposes a compact timeline for:

- parent and child run IDs;
- team and task IDs;
- mailbox message state, delivery attempts, ack/requeue/DLQ reason;
- child budget and timeout decisions;
- aggregation or vote outcome;
- final Agent and storage status.

Stele stores the semantic context and findings needed to continue the workflow. Baymax timeline and diagnostic events remain the operational record for execution mechanics.

## Context assembly and Stele projection

Stele is the context source, while Baymax Context Assembler is the policy and projection layer. The application does not concatenate the complete Stele transcript into every model request or child run.

Each model invocation passes through the assembler in four bounded phases:

1. **Prefix baseline**: assemble the stable role instructions, execution policy, tool capability digest, knowledge-base identity, and schema version. Hash and guard this prefix for the lifetime of the run so a child cannot silently inherit a different authority or tool set.
2. **Stage 2 disclosure**: decide whether to retrieve additional Stele or KnowledgeStore context. The default route is rules-based; an explicit AgenticRouter may be enabled for complex requests. Retrieval results carry source, scope, matched count, and transport/protocol/semantic error classification.
3. **Pressure control**: when a projection approaches its budget, apply task-aware tail recap, bounded pruning, and spill/swapback. Semantic compaction is optional and must preserve source references and fall back to a safe bounded recap when it fails.
4. **Handoff**: pass a compact `handoff.v1` payload to a subagent or next workflow stage containing confirmed facts, bounded inferences, open questions, source references, and the parent run/task identifiers. The handoff never contains unrestricted reasoning or an unbounded transcript.

### Role-specific projections

The coordinator and each worker receive a role-specific projection:

- `ContextReader`: conversation tail, relevant Stele memories, task facts, and no write capabilities;
- `KnowledgeNavigator`: knowledge index, candidate artifact references, query, and citation requirements;
- `SectionAnalyst`: one section or bounded source range, document metadata, compile policy, and existing concept/entity briefs relevant to that section;
- `CitationValidator`: proposed claims, source references, section ranges, and the compilation plan, without unrelated chat history;
- `TaskMemoryInterpreter`: user message, current task state, schedule policy, and explicit memory scope;
- coordinator: child findings, aggregate metadata, unresolved conflicts, remaining budget projection, and the final user-facing request.

Stele records the full context lineage and projection digest, but only the bounded projection is sent to the model. Every child result includes the projection digest used to produce it. A mismatch prevents silent aggregation and causes the coordinator to request a fresh projection.

### Budget and recovery rules

Baymax remains the owner of run, tool-call, scheduler-parent, and child budgets. The application consumes a read-only derived budget projection to decide whether to fan out more workers, compact context, or switch to a smaller evidence set. It must not write remaining budget values back into Stele or create a parallel budget ledger.

Context assembly journals intent and commit metadata for restart diagnosis. If compaction or external retrieval fails, the original context snapshot is retained and the run follows the configured best-effort or fail-fast policy. A handoff restore is idempotent by operation identity and references existing Stele records rather than copying them.

### Context validation scenarios

- A query with a long conversation uses a task-aware recap and keeps the citations needed for the final answer.
- Parallel section subagents receive disjoint bounded sections and do not receive each other's full transcripts.
- A child run is rejected when its prefix or tool capability digest differs from the coordinator's authorized projection.
- A scheduler restart reconstructs a child projection from Stele references and the stored handoff without duplicating the conversation.
- Context pressure and semantic compaction produce the same answer-relevant references on Run and Stream paths.

The `app` and `scheduler` processes share domain packages and the same configured storage. The scheduler does not write to HTTP session state directly; it creates an independent Agent run through the application service layer.

## Knowledge model

Knowledge is represented as durable artifacts with explicit lineage:

- **Source document:** normalized Markdown or text retained with its original path, content hash, and ingestion metadata.
- **Summary page:** one concise summary per source document with links to the source and related concepts.
- **Concept page:** cross-document synthesis for an abstract recurring topic. It records the source summaries that support it.
- **Entity page:** accumulated information about a named person, organization, place, product, work, or event.
- **Section tree:** heading-based hierarchy for Markdown/text documents. Each section records its title, position, parent, summary, and source range.
- **Knowledge index:** a compact catalog of documents, summaries, concepts, entities, and explorations with one-line descriptions and navigable links.
- **Exploration:** an explicitly saved analysis or comparison produced from a conversation.

Each generated page carries metadata for its type, description, source references, and update time. Source content remains available for citation and direct inspection. The system never treats generated knowledge text as executable instructions.

## Ingestion and compilation flow

```text
Configured document directory
        │
        ▼
File scan and content hashing
        │
        ▼
Markdown/text normalization and section-tree extraction
        │
        ▼
KnowledgeAgent in write policy
        │
        ├─ reads the current knowledge index
        ├─ reads relevant existing summaries/concepts/entities
        ├─ prepares a compilation plan
        ├─ generates or rewrites artifacts
        └─ updates source links and the index
        │
        ▼
Validated atomic KnowledgeStore commit
        │
        └─ Stele records the compile run and source decisions
```

The compiler is incremental. Unchanged files are skipped. A changed file is recompiled, and existing concept/entity pages are considered for update rather than blindly duplicated. Removed files are marked for cleanup through an explicit mutation path so that source references and orphaned artifacts can be handled consistently.

The compiler produces a `CompilationPlan` before writing. The plan contains:

- source identity and content hash;
- summary create/update operation;
- concept create/update operations;
- entity create/update operations;
- link and index changes;
- source references for every proposed artifact.

Domain validation rejects empty pages, invalid paths, duplicate identities, unknown link targets, and source references that do not exist. Only a validated plan reaches `CommitCompilation`, which executes the write as one transaction or recoverable filesystem mutation.

## Unified Baymax KnowledgeAgent

There is one Agent domain model with explicit execution policies rather than separate query and compiler Agents.

### Read capabilities

- `read_knowledge_index`
- `read_summary`
- `read_concept`
- `read_entity`
- `read_source_section`
- `search_knowledge_candidates`
- `read_task_context`
- `read_memory`

Read tools return typed results with artifact identity, source lineage, and bounded content. The Agent is instructed to start from the index, follow relevant summaries and linked pages, then read only the necessary source sections.

### Write capabilities

- `prepare_compilation_plan`
- `commit_compilation`
- `save_memory`
- `create_task`
- `update_task`
- `create_schedule`
- `update_schedule`
- `record_exploration`

Write tools are registered only when the run policy authorizes them. The Agent cannot enable write capabilities through natural-language output alone.

### Policies

- `read`: used for ordinary user chat. Knowledge writes are unavailable.
- `write`: used for document ingestion and explicit user requests to save or compile. Read and knowledge-write capabilities are available; task changes require the relevant task capability.
- `scheduled`: used by the scheduler. Capabilities are derived from the schedule definition, and the run is isolated from interactive chat.

Every run is labeled `query`, `compile`, `scheduled_query`, or `scheduled_compile`. The policy, prompt inputs, tool calls, outputs, and final status are recorded as Agent run events.

## Stele context model

The Stele adapter exposes domain operations rather than Stele internals:

- append and read conversation messages;
- retrieve relevant long-term memories;
- save explicitly requested or policy-approved memories;
- attach task definitions and execution summaries to context;
- attach compilation plans, source decisions, and artifact updates to a compile run;
- record Agent run events and failure status;
- retrieve the context needed by a scheduled run.

Context is partitioned by stable identifiers:

- user scope;
- conversation scope;
- task scope;
- schedule and execution scope;
- knowledge-base and compile-run scope.

Knowledge artifacts are not duplicated into Stele. Stele stores references, decisions, summaries, and context needed for continuity; KnowledgeStore remains the source of truth for generated pages and source content.

## Query flow

```text
User message
   │
   ▼
Create/resume conversation
   │
   ├─ read conversation context from Stele
   ├─ read relevant long-term and task memory from Stele
   └─ start KnowledgeAgent with read policy
          │
          ├─ read knowledge index
          ├─ inspect summaries/concepts/entities
          ├─ follow links when useful
          ├─ read bounded source sections
          └─ answer with citations
   │
   ▼
Stream text and typed tool events to Web
   │
   ├─ save messages and citations
   └─ append run events and query path to Stele
```

Keyword or full-text search may provide candidate artifacts, but the final answer is grounded by the Agent's navigation and bounded source reads. Embedding search is an extension point, not a required dependency for the first release.

## Task and scheduler model

The task domain supports:

- title, description, status, due time, labels, and source conversation;
- status transitions: `open`, `in_progress`, `completed`, `cancelled`;
- execution history for scheduled tasks;
- links to Stele context and Agent runs.

Scheduled tasks support two action types:

1. **Fixed action:** create a Web reminder, update a task state, or record an event.
2. **Natural-language Agent action:** execute a user-provided instruction through a `scheduled` KnowledgeAgent run.

Each schedule stores a cron expression, timezone, action type, instruction or fixed-action payload, enabled state, next run time, retry policy, and optional webhook configuration.

The scheduler loop:

1. finds due enabled schedules;
2. atomically acquires an execution lease;
3. creates an execution record;
4. loads the schedule's Stele context;
5. starts the allowed Agent run or fixed action;
6. records success, failure, output summary, and duration;
7. creates a Web notification;
8. sends a webhook asynchronously when configured;
9. calculates the next run and releases the lease.

The lease has an expiry so a scheduler restart can recover abandoned work. A schedule execution is idempotent by schedule ID and occurrence timestamp. Webhook failure never rolls back a successful Agent run; it is retried independently with exponential backoff and a terminal failure record.

## Storage modes

The application uses a storage interface for sessions, tasks, schedules, execution records, notifications, source metadata, knowledge artifacts, and index state.

Configuration:

```text
STORAGE_DRIVER=sqlite
SQLITE_PATH=./data/app.db

# PostgreSQL mode
STORAGE_DRIVER=postgres
DATABASE_URL=postgres://user:password@postgres:5432/personal_agent
```

SQLite is the default for local development and single-machine use. PostgreSQL is available through the same repositories, migrations, and domain services. `app` and `scheduler` always use the same configured database.

The local knowledge implementation stores source files and generated artifacts under a mounted data directory while SQLite stores metadata and searchable indexes. The PostgreSQL implementation stores metadata and artifacts through the same KnowledgeStore interface; source files may remain in a configured mounted document directory. This keeps the deployment switch explicit without coupling Agent code to a database representation.

Docker Compose provides:

- a default local profile using SQLite and mounted `data/` and `documents/` directories;
- a PostgreSQL profile that adds a PostgreSQL service and points `app` and `scheduler` at it;
- shared environment configuration for the OpenAI-compatible model endpoint;
- health checks and dependency ordering for app, scheduler, and database startup.

## Web workbench

The browser UI contains:

- **Knowledge:** document directory configuration, indexing action, file status, artifact browser, and source view;
- **Chat:** streaming assistant output, citations, tool events, task changes, and memory writes;
- **Tasks:** task list, status transitions, due dates, conversation links, and execution history;
- **Schedules and notifications:** schedule creation/editing, enable/disable, next run, execution history, Web notifications, and webhook status.

The first UI is intentionally functional and local-first. It does not expose internal implementation details such as Baymax prompt text or Stele record shapes; it shows decisions and outcomes that help the user understand and correct the Agent.

## Failure handling

- **Baymax tool failure:** preserve completed tool events, return a readable failure, and mark the Agent run failed.
- **Stele unavailable:** interactive chat may continue in explicitly marked no-memory mode; scheduled runs that require context fail and follow the schedule retry policy.
- **Knowledge compile failure:** do not publish a partial compilation plan. Preserve the previous artifact set and record the failed plan and reason.
- **Single-file ingestion failure:** continue other files and expose a file-level error.
- **Invalid Agent write:** reject at the domain boundary before storage mutation.
- **Scheduler restart:** recover expired leases and avoid duplicate execution using the schedule occurrence key.
- **Webhook failure:** retain the Agent result, retry delivery independently, and expose the final delivery status.
- **Model endpoint failure:** record provider status, latency, and retryable/non-retryable classification without losing conversation or execution records.

## Validation scenarios

The implementation is accepted only when these scenarios work in a clean Docker deployment:

1. Import two Markdown documents, compile them, ask a question, and receive a grounded answer with correct source citations.
2. Ask the Agent to remember a stable personal preference, start a new conversation, and retrieve it through Stele.
3. Create a task in conversation, update it in a later message, and verify Web state, task history, and Stele context agree.
4. Schedule “every day at 09:00, summarize today's open tasks and save the result to memory”, trigger it with the scheduler, and verify the Agent run, Stele memory, execution record, and Web notification.
5. Schedule a fixed reminder with a webhook, force the receiver to fail, and verify that the reminder remains recorded while webhook delivery retries independently.
6. Restart `app` and `scheduler` while work is pending, then verify that the work is recovered without duplicate execution.
7. Run the same application with SQLite and PostgreSQL configuration and verify the core chat, compile, task, and schedule flows through the same API.
8. Compile a document with multiple sections and verify that the parent run fans out bounded `SectionAnalyst` subagents, a team aggregates their findings, the citation validator rejects an unsupported claim, and the final compilation commits only validated artifacts.
9. Run a knowledge query that requires Stele memory, knowledge navigation, and task state, and verify that mailbox command/event/result records, team aggregation, child budgets, and the final coordinator answer are all correlated by run and task IDs.
10. Force a transient child failure, verify mailbox requeue and eventual ack, then force repeated failure and verify DLQ state without losing the parent run's terminal outcome.
11. Submit equivalent low, medium, and high complexity task profiles and verify the planner chooses `direct`, bounded `subagent_fanout` or `team_parallel`, and checkpointed `workflow`/`mailbox_async` respectively, with an auditable rationale.
12. Force a discovery-time complexity increase, verify a new plan version is created at a safe point, completed child results are reused, remaining budget is recalculated, and no new write authority is granted implicitly.
13. Pause a run for `input_required` or `approval_required`, resume it from the stored checkpoint, and verify Baymax run IDs, plan version, Context Assembler projection digests, and Stele references remain correlated.
14. Introduce a controlled dependency failure or behavior mismatch, verify a deterministic regression fixture and a complete record in the corresponding Baymax or Stele findings document, then verify that the dependent implementation task is blocked until the upstream repair is available.

Verification is layered:

- unit tests for policy-controlled tool registration, task transitions, cron handling, lease recovery, storage repositories, Stele mapping, and compilation-plan validation;
- integration tests for Baymax + Stele + KnowledgeStore, including Context Assembler projection, handoff, budget-derived view, mailbox, teams, and subagent paths;
- planner and harness tests for deterministic mode selection, admission, plan-versioned replanning, pause/resume, commit gates, and Baymax terminal-event ownership;
- dependency compatibility tests tied to every active Baymax/Stele finding, with version-aware upgrade checks and evidence that blocked tasks unblock only after the upstream repair is verified;
- Docker end-to-end tests for API chat, compile, task, schedule, notification, and storage switching;
- manual browser checks for streaming output, citations, artifact browsing, tasks, schedules, and notifications.

## Completion criteria for the design

This design is complete when the implementation plan maps every scope item to a focused module and an executable validation step, and when the first implementation can demonstrate a real multi-turn workflow that crosses Baymax Agent execution, Stele context retrieval, compiled knowledge navigation, task management, and scheduled execution.
