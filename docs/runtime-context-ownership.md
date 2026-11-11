# Runtime and Context Ownership

This matrix is the product boundary for Milestone 8. Danny keeps product
authority and durable business state. Baymax owns execution mechanics. Stele
owns memory selection and continuity. A public upstream API is an integration
boundary, not a transfer of Danny's authorization or commit authority.

| Responsibility | Owner | Public contract | Caller | Persistence owner | Evidence |
| --- | --- | --- | --- | --- | --- |
| Model Run/Stream and registered tools | Baymax | `core/runner.Engine.Run`, `Stream`, host admission | Danny execution adapter | Baymax source; Danny projection | `docs/baymax-contract-evidence.md` |
| Child dispatch and bounded assignments | Baymax | `orchestration/composer.DispatchChild` | Danny execution adapter | Baymax source; Danny evidence | `internal/agent/baymax/adapter.go` |
| Team strategy and workflow steps | Baymax | `orchestration/teams`, workflow packages | Danny execution adapter | Baymax source; Danny evidence | `docs/baymax-contract-evidence.md` |
| Mailbox lease, retry, acknowledgement and DLQ | Baymax | `orchestration/mailbox` | Scheduled/common execution adapter | Baymax mailbox store; Danny dispatch intent | `internal/agent/baymax/scheduled.go` |
| Scheduled admission and occurrence dispatch | Danny + Baymax | Danny occurrence contract plus `orchestration/scheduler` | Scheduled task service | Danny occurrence/outbox; Baymax scheduler | `internal/agent/baymax/scheduled.go` |
| Safe points, checkpoint schema and native restore | Baymax | runner checkpoint/control APIs | Danny recovery adapter | Baymax checkpoint store plus Danny binding | `docs/baymax-contract-evidence.md` |
| Native cancellation, timeout and causal retry | Baymax | `Engine.CancelRun`, `RetryRun` | Danny control adapter | Baymax source; Danny control evidence | `docs/baymax-contract-evidence.md` |
| Native event identity, source order and terminal arbitration | Baymax | `types.EventEnvelope`, host event handler | Danny event projector | Baymax source; Danny observation projection | `internal/runtimeboundary/contracts.go` |
| Source trust classification and tool authorization | Danny | `knowledge.Policy`, `ToolRegistry` | Product admission and tool callbacks | Danny AppStore | `internal/agent/knowledge/agent.go` |
| Plan selection, capability manifest and task revision | Danny | `runtimeboundary.ExecutionRequest`, planner | Product harness | Danny AppStore | `internal/runtimeboundary/contracts.go` |
| Tasks, generations, source bytes and citations | Danny | storage repositories | Knowledge and task services | Danny AppStore/filesystem generation journal | `docs/storage-durability.md` |
| Product observation log and replay cursor | Danny | runtimeboundary event projection repository | Protocol-neutral observers | Danny AppStore | `openspec/changes/runtime-context-ownership-boundary/specs/storage-durability/spec.md` |
| Effect ledger and application outboxes | Danny | storage effect/outbox repositories | Schedulers, tools and delivery workers | Danny AppStore | `docs/scheduler-reliable-delivery.md` |
| Memory ingestion, path and provenance | Stele | documented memory/provider API | Stele adapter | Stele store | `docs/upstream/stele-findings.md` |
| Memory retrieval, ranking and selection | Stele | documented context/retrieve API | Stele adapter | Stele store | `internal/context/stele/adapter.go` |
| Memory sessions, turns and context continuity | Stele | documented session/context fields | Stele adapter | Stele store plus Danny binding | `docs/upstream/stele-findings.md` |
| Final model history pressure and native handoff | Baymax | context assembler and runner context API | Danny execution adapter | Baymax source | `docs/baymax-contract-evidence.md` |
| Product context trust, role disclosure and references | Danny | versioned `ContextEnvelope` | Product context assembler | Danny admitted projection snapshot | `internal/runtimeboundary/context.go` |
| Knowledge compilation and product aggregation | Danny | knowledge task/generation repositories and execution boundary | Knowledge compiler | Danny generation commit, effects and outboxes | `internal/agent/knowledge`, `internal/storage` |

## Boundary Rules

1. Domain modules use Danny-owned versioned DTOs. Only adapter packages may
   import Baymax public Go packages or decode Stele HTTP responses.
2. A capability is available only when its exact dependency pin and a
   reproducible evidence fixture are recorded in the capability manifest.
3. A failed or unverified upstream guarantee is recorded in the appropriate
   Chinese upstream findings document and blocks dependent production work.
4. Danny may project and persist upstream facts, but it must not manufacture a
   native event, native checkpoint, native ordering claim, memory ranking, or
   final model history decision.
5. Product revisions, write scopes, effect namespaces, generation commits and
   outboxes remain Danny-owned even when a Baymax or Stele operation executes
   them through a callback.
