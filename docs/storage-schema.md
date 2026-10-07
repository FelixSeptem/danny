# Storage schema review checklist

This checklist is the review artifact for the Milestone 2 canonical schema. The migration runner creates the tables in one versioned migration and records the version in `schema_migrations`.

## Entity coverage

- [x] `conversations` stores scoped context references and optimistic revisions.
- [x] `tasks` stores mutable task state and monotonic revisions.
- [x] `schedules` stores cron, IANA timezone, action type, instruction, enabled state, and revisions.
- [x] `executions` stores task/schedule correlation, occurrence identity, result, and revision.
- [x] `notifications` stores channel delivery state and payload.
- [x] `source_metadata` stores path, content hash, and committed source bytes.
- [x] `knowledge_generations` stores monotonic generation ID, status, commit marker, and commit time.
- [x] `artifacts` stores generation identity, kind, content hash, bytes, and lineage.
- [x] `manifests` stores the immutable generation manifest.
- [x] `revisions` provides a shared subject revision namespace for future entities.
- [x] `idempotency_effects` stores operation namespace, subject, effect key, status, request, and original result.
- [x] `leases` stores owner, fencing value, expiry, and heartbeat metadata.
- [x] `outboxes` stores typed delivery intent, payload version, stable idempotency key, attempts, lease, and retry time.
- [x] `task_harness_executions` stores durable task run status, revision, plan/capability/projection digests, correlation, and admission idempotency.
- [x] `task_harness_plans` stores immutable plan notebook versions, authority bounds, child limits, effect references, and replan rationale.
- [x] `task_harness_checkpoints` stores safe-point recovery identity, pause reason, current step, and completed effect references.
- [x] `task_harness_replans` stores plan lineage and authority comparison history.
- [x] `task_harness_evidence` stores parent/child terminal evidence with stable effect keys.

## Invariants

- [x] Entity IDs and generation IDs are unique.
- [x] `(namespace, subject, effect_key)` is unique for idempotent effects.
- [x] `(kind, idempotency_key)` is unique for outbox delivery intent.
- [x] `(generation_id, kind, content_hash)` is unique for artifact identity.
- [x] `(execution_id, version)` is unique for immutable plan notebook entries.
- [x] `(execution_id, effect_key)` is unique for replay-safe execution evidence.
- [x] Execution terminal and checkpoint mutations require the expected revision.
- [x] Committed generations require a non-null commit marker.
- [x] Mutable task updates require the expected revision predicate.
- [x] Outbox claims require a pending row or an expired leased row.
- [x] Lease replacement increments fencing and stale owners cannot release the replacement lease.
- [x] Filesystem materialization reads only committed generation rows.

## Transaction boundaries

- [x] Schema migration and schema version record commit together.
- [x] Product state and its outbox intent commit together through `WithTransaction`.
- [x] Effect result and the protected mutation are required to share a transaction boundary.
- [x] Generation manifest and commit marker commit together.
- [x] Materialization uses atomic temporary-file rename and can be repeated from the manifest.
- [x] Execution admission writes the execution row and initial plan notebook entry in one transaction.
- [x] Execution evidence replay returns the original result for the same effect key.
