package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

var migrationStatements = []string{
	`CREATE TABLE IF NOT EXISTS conversations (id TEXT PRIMARY KEY, scope TEXT NOT NULL, payload BYTEA NOT NULL DEFAULT '', revision BIGINT NOT NULL DEFAULT 1, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS tasks (id TEXT PRIMARY KEY, title TEXT NOT NULL, description TEXT NOT NULL DEFAULT '', status TEXT NOT NULL DEFAULT 'open', revision BIGINT NOT NULL DEFAULT 1, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS schedules (id TEXT PRIMARY KEY, cron TEXT NOT NULL, timezone TEXT NOT NULL, action_type TEXT NOT NULL, instruction TEXT NOT NULL DEFAULT '', enabled INTEGER NOT NULL DEFAULT 1, revision BIGINT NOT NULL DEFAULT 1, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS executions (id TEXT PRIMARY KEY, task_id TEXT, schedule_id TEXT, occurrence_key TEXT, status TEXT NOT NULL, result BYTEA NOT NULL DEFAULT '', revision BIGINT NOT NULL DEFAULT 1, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS notifications (id TEXT PRIMARY KEY, execution_id TEXT, channel TEXT NOT NULL, status TEXT NOT NULL, payload BYTEA NOT NULL DEFAULT '', revision BIGINT NOT NULL DEFAULT 1, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS source_metadata (id TEXT PRIMARY KEY, path TEXT NOT NULL, content_hash TEXT NOT NULL, content BYTEA NOT NULL, format TEXT NOT NULL DEFAULT 'md', parser_version TEXT NOT NULL DEFAULT 'danny-native', provider_mode TEXT NOT NULL DEFAULT 'local', source_fingerprint TEXT NOT NULL DEFAULT '', warnings BYTEA NOT NULL DEFAULT '', untrusted_source INTEGER NOT NULL DEFAULT 1, created_at TEXT NOT NULL, updated_at TEXT NOT NULL, UNIQUE(path, content_hash))`,
	`CREATE TABLE IF NOT EXISTS knowledge_generations (id BIGINT PRIMARY KEY, status TEXT NOT NULL, commit_marker TEXT, created_at TEXT NOT NULL, committed_at TEXT)`,
	`CREATE TABLE IF NOT EXISTS artifacts (id TEXT PRIMARY KEY, generation_id BIGINT NOT NULL, kind TEXT NOT NULL, content_hash TEXT NOT NULL, content BYTEA NOT NULL, lineage BYTEA NOT NULL DEFAULT '', created_at TEXT NOT NULL, UNIQUE(generation_id, kind, content_hash))`,
	`CREATE TABLE IF NOT EXISTS manifests (generation_id BIGINT PRIMARY KEY, payload BYTEA NOT NULL, created_at TEXT NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS revisions (subject_type TEXT NOT NULL, subject_id TEXT NOT NULL, revision BIGINT NOT NULL, updated_at TEXT NOT NULL, PRIMARY KEY(subject_type, subject_id))`,
	`CREATE TABLE IF NOT EXISTS idempotency_effects (id TEXT PRIMARY KEY, namespace TEXT NOT NULL, subject TEXT NOT NULL, effect_key TEXT NOT NULL, status TEXT NOT NULL, request BYTEA NOT NULL, result BYTEA NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL, UNIQUE(namespace, subject, effect_key))`,
	`CREATE TABLE IF NOT EXISTS leases (resource_type TEXT NOT NULL, resource_id TEXT NOT NULL, owner TEXT NOT NULL, fencing BIGINT NOT NULL, lease_until TEXT NOT NULL, heartbeat_at TEXT NOT NULL, PRIMARY KEY(resource_type, resource_id))`,
	`CREATE TABLE IF NOT EXISTS outboxes (id TEXT PRIMARY KEY, kind TEXT NOT NULL, idempotency_key TEXT NOT NULL, payload_version INTEGER NOT NULL, payload BYTEA NOT NULL, status TEXT NOT NULL DEFAULT 'pending', attempts INTEGER NOT NULL DEFAULT 0, owner TEXT NOT NULL DEFAULT '', lease_until TEXT, available_at TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL, UNIQUE(kind, idempotency_key))`,
	`CREATE INDEX IF NOT EXISTS idx_outboxes_claim ON outboxes(kind, status, available_at, lease_until)`,
	`CREATE INDEX IF NOT EXISTS idx_generations_status ON knowledge_generations(status, id)`,
	`CREATE TABLE IF NOT EXISTS task_harness_executions (id TEXT PRIMARY KEY, task_id TEXT NOT NULL, task_revision BIGINT NOT NULL DEFAULT 0, status TEXT NOT NULL, revision BIGINT NOT NULL DEFAULT 1, mode TEXT NOT NULL, plan_version TEXT NOT NULL, plan_digest TEXT NOT NULL, capability_digest TEXT NOT NULL, projection_digest TEXT NOT NULL, parent_run_id TEXT NOT NULL DEFAULT '', correlation_id TEXT NOT NULL, idempotency_key TEXT NOT NULL, input TEXT NOT NULL DEFAULT '', result BYTEA NOT NULL DEFAULT '', failure_class BYTEA NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL, UNIQUE(idempotency_key))`,
	`CREATE TABLE IF NOT EXISTS task_harness_plans (execution_id TEXT NOT NULL, version TEXT NOT NULL, parent_version TEXT NOT NULL DEFAULT '', mode TEXT NOT NULL, profile_digest TEXT NOT NULL, plan_digest TEXT NOT NULL, capability_digest TEXT NOT NULL, projection_digest TEXT NOT NULL, rationale TEXT NOT NULL DEFAULT '', replan_reason TEXT NOT NULL DEFAULT '', budget BIGINT NOT NULL DEFAULT 0, write_scope BIGINT NOT NULL DEFAULT 0, child_limit BIGINT NOT NULL DEFAULT 0, per_child_budget BIGINT NOT NULL DEFAULT 0, aggregation_limit BIGINT NOT NULL DEFAULT 0, authority_expanded INTEGER NOT NULL DEFAULT 0, completed_effect_keys BYTEA NOT NULL DEFAULT '', diagnostics BYTEA NOT NULL DEFAULT '', created_at TEXT NOT NULL, PRIMARY KEY(execution_id, version))`,
	`CREATE TABLE IF NOT EXISTS task_harness_checkpoints (execution_id TEXT PRIMARY KEY, run_id TEXT NOT NULL, plan_version TEXT NOT NULL, plan_digest TEXT NOT NULL, capability_digest TEXT NOT NULL, projection_digest TEXT NOT NULL, current_step TEXT NOT NULL DEFAULT '', pause_reason TEXT NOT NULL, completed_effect_keys BYTEA NOT NULL DEFAULT '', diagnostics BYTEA NOT NULL DEFAULT '', revision BIGINT NOT NULL DEFAULT 1, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS task_harness_replans (id TEXT PRIMARY KEY, execution_id TEXT NOT NULL, previous_plan_version TEXT NOT NULL, new_plan_version TEXT NOT NULL, reason TEXT NOT NULL, previous_profile_digest TEXT NOT NULL, new_profile_digest TEXT NOT NULL, authority_expanded INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS task_harness_evidence (id TEXT PRIMARY KEY, execution_id TEXT NOT NULL, effect_key TEXT NOT NULL, parent_run_id TEXT NOT NULL DEFAULT '', child_run_id TEXT NOT NULL DEFAULT '', plan_version TEXT NOT NULL, capability_digest TEXT NOT NULL, projection_digest TEXT NOT NULL, status BYTEA NOT NULL, failure_class BYTEA NOT NULL DEFAULT '', diagnostic BYTEA NOT NULL DEFAULT '', result BYTEA NOT NULL DEFAULT '', created_at TEXT NOT NULL, UNIQUE(execution_id, effect_key))`,
	`CREATE INDEX IF NOT EXISTS idx_task_harness_executions_status ON task_harness_executions(status, updated_at)`,
	`CREATE INDEX IF NOT EXISTS idx_task_harness_replans_execution ON task_harness_replans(execution_id, created_at)`,
	`CREATE TABLE IF NOT EXISTS schedule_definitions (id TEXT PRIMARY KEY, cron TEXT NOT NULL, timezone TEXT NOT NULL, action_type TEXT NOT NULL, instruction TEXT NOT NULL DEFAULT '', payload TEXT NOT NULL DEFAULT '', enabled INTEGER NOT NULL DEFAULT 1, retry_limit INTEGER NOT NULL DEFAULT 0, misfire_policy TEXT NOT NULL DEFAULT 'skip', backfill_limit INTEGER NOT NULL DEFAULT 0, revision BIGINT NOT NULL DEFAULT 1, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS schedule_occurrences (id TEXT PRIMARY KEY, schedule_id TEXT NOT NULL, schedule_revision BIGINT NOT NULL, occurrence_key TEXT NOT NULL, local_time TEXT NOT NULL, resolved_at TEXT NOT NULL, timezone TEXT NOT NULL, tzdata_version TEXT NOT NULL, dst_decision TEXT NOT NULL, status TEXT NOT NULL, revision BIGINT NOT NULL DEFAULT 1, attempt_count INTEGER NOT NULL DEFAULT 0, execution_id TEXT NOT NULL DEFAULT '', correlation_id TEXT NOT NULL DEFAULT '', idempotency_key TEXT NOT NULL, policy_digest TEXT NOT NULL DEFAULT '', capability_digest TEXT NOT NULL DEFAULT '', plan_version TEXT NOT NULL DEFAULT '', projection_digest TEXT NOT NULL DEFAULT '', effect_namespace TEXT NOT NULL DEFAULT '', last_error TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL, UNIQUE(schedule_id, occurrence_key))`,
	`CREATE INDEX IF NOT EXISTS idx_schedule_occurrences_schedule ON schedule_occurrences(schedule_id, local_time)`,
	`CREATE TABLE IF NOT EXISTS schedule_dispatch_attempts (id TEXT PRIMARY KEY, occurrence_id TEXT NOT NULL, outbox_id TEXT NOT NULL, status TEXT NOT NULL, attempt INTEGER NOT NULL DEFAULT 0, owner TEXT NOT NULL DEFAULT '', fencing BIGINT NOT NULL DEFAULT 0, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS webhook_deliveries (id TEXT PRIMARY KEY, execution_id TEXT NOT NULL DEFAULT '', occurrence_id TEXT NOT NULL DEFAULT '', effect_key TEXT NOT NULL, url TEXT NOT NULL, payload BYTEA NOT NULL DEFAULT '', status TEXT NOT NULL, revision BIGINT NOT NULL DEFAULT 1, attempt_count BIGINT NOT NULL DEFAULT 0, next_attempt_at TEXT NOT NULL, owner TEXT NOT NULL DEFAULT '', lease_until TEXT, last_error TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL, UNIQUE(effect_key))`,
	`CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_claim ON webhook_deliveries(status, next_attempt_at, lease_until)`,
	`CREATE TABLE IF NOT EXISTS parser_jobs (id TEXT PRIMARY KEY, contract_version TEXT NOT NULL, idempotency_key TEXT NOT NULL, staging_object_id TEXT NOT NULL, source_fingerprint TEXT NOT NULL, content_hash TEXT NOT NULL, filename TEXT NOT NULL, format_hint TEXT NOT NULL DEFAULT '', policy BYTEA NOT NULL DEFAULT '', status TEXT NOT NULL, attempt_count INTEGER NOT NULL DEFAULT 0, result BYTEA NOT NULL DEFAULT '', failure BYTEA NOT NULL DEFAULT '', parser_version TEXT NOT NULL DEFAULT '', provider_mode TEXT NOT NULL DEFAULT '', cleanup_state TEXT NOT NULL DEFAULT 'pending', owner TEXT NOT NULL DEFAULT '', lease_until TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL, UNIQUE(idempotency_key))`,
	`CREATE INDEX IF NOT EXISTS idx_parser_jobs_claim ON parser_jobs(status, lease_until, created_at)`,
}

func (s *Store) Migrate(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, s.bind(`CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`)); err != nil {
		return fmt.Errorf("create schema migrations: %w", err)
	}
	var applied int
	if err := tx.QueryRowContext(ctx, s.bind(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`)).Scan(&applied); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if applied > CurrentSchemaVersion {
		return fmt.Errorf("database schema version %d is newer than application version %d", applied, CurrentSchemaVersion)
	}
	if applied < CurrentSchemaVersion {
		for _, statement := range migrationStatements {
			if _, err := tx.ExecContext(ctx, s.bind(statement)); err != nil {
				return fmt.Errorf("apply schema migration %d: %w", CurrentSchemaVersion, err)
			}
		}
		if err := ensureSourceMetadataColumns(ctx, tx, s.driver); err != nil {
			return fmt.Errorf("ensure source metadata columns: %w", err)
		}
		if _, err := tx.ExecContext(ctx, s.bind(`INSERT INTO schema_migrations(version, applied_at) VALUES (?, ?)`), CurrentSchemaVersion, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return fmt.Errorf("record schema migration: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration: %w", err)
	}
	return nil
}

func ensureSourceMetadataColumns(ctx context.Context, tx *sql.Tx, driver string) error {
	columns := map[string]bool{}
	if driver == "postgres" {
		rows, err := tx.QueryContext(ctx, `SELECT column_name FROM information_schema.columns WHERE table_name='source_metadata'`)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				return err
			}
			columns[name] = true
		}
		if err := rows.Err(); err != nil {
			return err
		}
	} else {
		rows, err := tx.QueryContext(ctx, `PRAGMA table_info(source_metadata)`)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var cid, notNull, primaryKey int
			var name, dataType string
			var defaultValue sql.NullString
			if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
				return err
			}
			columns[name] = true
		}
		if err := rows.Err(); err != nil {
			return err
		}
	}
	additions := []struct {
		name, statement string
	}{
		{"format", `ALTER TABLE source_metadata ADD COLUMN format TEXT NOT NULL DEFAULT 'md'`},
		{"parser_version", `ALTER TABLE source_metadata ADD COLUMN parser_version TEXT NOT NULL DEFAULT 'danny-native'`},
		{"provider_mode", `ALTER TABLE source_metadata ADD COLUMN provider_mode TEXT NOT NULL DEFAULT 'local'`},
		{"source_fingerprint", `ALTER TABLE source_metadata ADD COLUMN source_fingerprint TEXT NOT NULL DEFAULT ''`},
		{"warnings", `ALTER TABLE source_metadata ADD COLUMN warnings BYTEA NOT NULL DEFAULT ''`},
		{"untrusted_source", `ALTER TABLE source_metadata ADD COLUMN untrusted_source INTEGER NOT NULL DEFAULT 1`},
	}
	for _, addition := range additions {
		if columns[addition.name] {
			continue
		}
		statement := addition.statement
		if driver == "postgres" {
			statement = strings.Replace(statement, "ADD COLUMN ", "ADD COLUMN IF NOT EXISTS ", 1)
		}
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) SchemaVersion(ctx context.Context) (int, error) {
	var version int
	if err := s.db.QueryRowContext(ctx, s.bind(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`)).Scan(&version); err != nil {
		return 0, err
	}
	return version, nil
}
