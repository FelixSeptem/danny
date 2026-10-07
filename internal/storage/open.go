package storage

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

func OpenSQLite(ctx context.Context, dsn string) (*Store, error) {
	if strings.TrimSpace(dsn) == "" {
		dsn = "data/danny.db"
	}
	if !strings.HasPrefix(dsn, "file:") && dsn != ":memory:" {
		if dir := filepath.Dir(dsn); dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, fmt.Errorf("create sqlite directory: %w", err)
			}
		}
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys = ON; PRAGMA busy_timeout = 5000;"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("configure sqlite: %w", err)
	}
	return &Store{db: db, driver: "sqlite"}, nil
}

func Open(ctx context.Context, driver, dsn string) (*Store, error) {
	switch strings.ToLower(strings.TrimSpace(driver)) {
	case "sqlite":
		return OpenSQLite(ctx, dsn)
	case "postgres", "postgresql":
		return OpenPostgres(ctx, dsn)
	default:
		return nil, fmt.Errorf("unsupported storage driver %q", driver)
	}
}

func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

func OpenPostgres(ctx context.Context, dsn string) (*Store, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, fmt.Errorf("postgres database URL is required")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return &Store{db: db, driver: "postgres"}, nil
}
