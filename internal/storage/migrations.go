package storage

import (
	"context"
	"fmt"
	"time"
)

var migrationStatements = []string{
	`CREATE TABLE IF NOT EXISTS conversations (id TEXT PRIMARY KEY, scope TEXT NOT NULL, payload BYTEA NOT NULL DEFAULT '', revision BIGINT NOT NULL DEFAULT 1, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS tasks (id TEXT PRIMARY KEY, title TEXT NOT NULL, description TEXT NOT NULL DEFAULT '', status TEXT NOT NULL DEFAULT 'open', revision BIGINT NOT NULL DEFAULT 1, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS schedules (id TEXT PRIMARY KEY, cron TEXT NOT NULL, timezone TEXT NOT NULL, action_type TEXT NOT NULL, instruction TEXT NOT NULL DEFAULT '', enabled INTEGER NOT NULL DEFAULT 1, revision BIGINT NOT NULL DEFAULT 1, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS executions (id TEXT PRIMARY KEY, task_id TEXT, schedule_id TEXT, occurrence_key TEXT, status TEXT NOT NULL, result BYTEA NOT NULL DEFAULT '', revision BIGINT NOT NULL DEFAULT 1, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS notifications (id TEXT PRIMARY KEY, execution_id TEXT, channel TEXT NOT NULL, status TEXT NOT NULL, payload BYTEA NOT NULL DEFAULT '', revision BIGINT NOT NULL DEFAULT 1, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS source_metadata (id TEXT PRIMARY KEY, path TEXT NOT NULL, content_hash TEXT NOT NULL, content BYTEA NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL, UNIQUE(path, content_hash))`,
	`CREATE TABLE IF NOT EXISTS knowledge_generations (id BIGINT PRIMARY KEY, status TEXT NOT NULL, commit_marker TEXT, created_at TEXT NOT NULL, committed_at TEXT)`,
	`CREATE TABLE IF NOT EXISTS artifacts (id TEXT PRIMARY KEY, generation_id BIGINT NOT NULL, kind TEXT NOT NULL, content_hash TEXT NOT NULL, content BYTEA NOT NULL, lineage BYTEA NOT NULL DEFAULT '', created_at TEXT NOT NULL, UNIQUE(generation_id, kind, content_hash))`,
	`CREATE TABLE IF NOT EXISTS manifests (generation_id BIGINT PRIMARY KEY, payload BYTEA NOT NULL, created_at TEXT NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS revisions (subject_type TEXT NOT NULL, subject_id TEXT NOT NULL, revision BIGINT NOT NULL, updated_at TEXT NOT NULL, PRIMARY KEY(subject_type, subject_id))`,
	`CREATE TABLE IF NOT EXISTS idempotency_effects (id TEXT PRIMARY KEY, namespace TEXT NOT NULL, subject TEXT NOT NULL, effect_key TEXT NOT NULL, status TEXT NOT NULL, request BYTEA NOT NULL, result BYTEA NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL, UNIQUE(namespace, subject, effect_key))`,
	`CREATE TABLE IF NOT EXISTS leases (resource_type TEXT NOT NULL, resource_id TEXT NOT NULL, owner TEXT NOT NULL, fencing BIGINT NOT NULL, lease_until TEXT NOT NULL, heartbeat_at TEXT NOT NULL, PRIMARY KEY(resource_type, resource_id))`,
	`CREATE TABLE IF NOT EXISTS outboxes (id TEXT PRIMARY KEY, kind TEXT NOT NULL, idempotency_key TEXT NOT NULL, payload_version INTEGER NOT NULL, payload BYTEA NOT NULL, status TEXT NOT NULL DEFAULT 'pending', attempts INTEGER NOT NULL DEFAULT 0, owner TEXT NOT NULL DEFAULT '', lease_until TEXT, available_at TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL, UNIQUE(kind, idempotency_key))`,
	`CREATE INDEX IF NOT EXISTS idx_outboxes_claim ON outboxes(kind, status, available_at, lease_until)`,
	`CREATE INDEX IF NOT EXISTS idx_generations_status ON knowledge_generations(status, id)`,
}

func (s *Store) Migrate(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration: %w", err)
	}
	defer tx.Rollback()
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
		if _, err := tx.ExecContext(ctx, s.bind(`INSERT INTO schema_migrations(version, applied_at) VALUES (?, ?)`), CurrentSchemaVersion, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return fmt.Errorf("record schema migration: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration: %w", err)
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
