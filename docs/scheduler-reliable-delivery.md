# Scheduler and Reliable Delivery

## Schedule identity

Schedules persist a five-field cron expression, an IANA timezone, an action type, retry limit, and one of `skip`, `coalesce_latest`, or bounded `backfill` misfire policies. Every occurrence stores the schedule revision, local civil timestamp, resolved UTC instant, the configured tzdata label, and a stable occurrence key.

Nonexistent local times during a daylight saving transition are skipped. Ambiguous local times use the earlier UTC instant. The decision is stored with the occurrence so a restart does not recalculate a different identity.

## Durable dispatch

Danny commits the occurrence and a `schedule_dispatch` outbox intent in one SQLite or PostgreSQL transaction. Restart reconciliation queries the original idempotency key and republishes that intent through the Baymax scheduler and mailbox adapters. Danny does not implement Baymax lease, retry, backoff, QoS, or dead-letter state machines.

The Baymax payload carries the occurrence key, schedule revision, policy digest, plan version, capability digest, projection digest, and effect namespace. Mailbox workers must pass the same occurrence idempotency key to task harness admission.

## Scheduled authority and Stele effects

Scheduled tool calls use `PolicyScheduled` and an occurrence write scope. Retrieved documents and other source text are untrusted data. They cannot authorize mutation tools such as `save_memory`, `create_task`, `commit_compilation`, or `send_webhook`.

Scheduled Stele writes use the occurrence effect key through the public provider adapter. The adapter sends the key in request metadata and the `Idempotency-Key` HTTP header; Danny does not add a local memory or reference store.

## Webhook delivery

Webhook delivery is a durable record with its own lease, attempt count, retry time, and terminal failure state. The terminal Agent result and webhook intent are committed together. Later delivery failure never changes a previously committed Agent success or failure.

