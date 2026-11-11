## Why

The updated roadmap places Milestone 8 before Agent UI protocols so that Danny can expose verified runtime and context behavior through a stable product boundary. Current production adapters still reuse probe callbacks, synthesize execution events, and maintain application recovery logic that overlaps Baymax, while the Stele projection adapter does not preserve the full public context response and its provenance.

## What Changes

- Establish a responsibility matrix for Baymax execution mechanics, Stele memory/context continuity, and Danny admission, authorization, product state, and cross-store durability.
- Introduce a versioned Danny execution-host adapter over verified Baymax public contracts, covering plan projection, native events, direct execution, children, parallel/vote teams, workflows, and mailbox continuation. Preserve scheduled occurrence authority at admission.
- Replace production probe routing and fabricated runtime lifecycle events with native execution and orchestration evidence, preserving Run/Stream terminal and aggregation parity.
- Bind application authority checkpoints to native Baymax checkpoint references and safe points. Resume, retry, cancellation, and timeout use the verified source contract; uncertain dispatch or missing recovery evidence fails closed.
- Introduce a versioned Stele context envelope for scoped queries, explicit budget units, authorized role/policy disclosure, bounded references, provenance, selection diagnostics, session/turn correlation, and application projection digests. Delegate memory selection to Stele and final model assembly/handoff to Baymax.
- Add durable protocol-neutral execution evidence and replay cursors, plus checkpoint/context bindings, with equivalent SQLite and PostgreSQL behavior. Distinguish application observation order from upstream event ordering.
- Require pinned public-contract evidence and Chinese upstream findings for confirmed missing or incorrect dependency behavior. Block affected implementation tasks until a pinned upstream repair passes the original reproduction; do not add production workarounds.
- Supply the contracts required by Milestone 9 without introducing AG-UI, A2UI, SSE/HTTP endpoints, or frontend dependencies in this change.

## Capabilities

### New Capabilities

- `runtime-context-ownership-boundary`: Versioned execution/context adapters, responsibility ownership, verified mode admission, and upstream compatibility gates.

### Modified Capabilities

- `knowledge-agent-context-assembly`: Capability-aware deterministic planning, native execution evidence and recovery, Stele-owned memory selection, Baymax-owned final assembly, and a product authority harness that delegates runtime mechanics.
- `storage-durability`: Durable native-event projections and replay cursors, and recoverable runtime checkpoint/context authority bindings across SQLite and PostgreSQL.

## Impact

- Runtime integration: `internal/agent/baymax/adapter.go`, `compile.go`, and `scheduled.go`; `internal/agent/knowledge`; and `internal/tasks/harness.go`.
- Context integration: `internal/context/stele` and `internal/context/assembly`, retaining server-controlled trust and tool authorization.
- Persistence: driver-neutral execution repositories and additive migrations for native evidence identity, cursor checkpoints, and runtime/context bindings. Existing task revisions, effect records, knowledge generation commits, and transactional outboxes remain authoritative.
- Dependencies: current Baymax pin `v0.0.0-20261103091945-d2e08cd6dc8c` and Stele pin `v0.0.0-20261025080141-f38016ec0cba`. The Baymax pin has renewed source-history and host-control evidence, but does not imply every execution form has durable replay or restore support. Stele `STELE-006` and `STELE-007` are repaired; the pin has passed Stele's fresh PostgreSQL/pgvector assurance and Danny's 2026-10-26 context/reference/compilation/restart/idempotency smoke. Remaining Milestone 8 gaps are tracked independently for Baymax live evidence and protocol-neutral handoff acceptance.
- Consumers: knowledge compilation and scheduled execution migrate to the verified adapter without changing generation commit, occurrence idempotency, or delivery transaction guarantees.
- Documentation and acceptance: roadmap, runtime/context ownership matrix, harness/probe documentation, integration evidence, provider boundary checks, and `golangci-lint` acceptance. Upstream findings retain separate resolved/unresolved sections in Chinese.
