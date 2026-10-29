package storage

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"regexp"
	"slices"
	"strconv"
	"time"
)

// Migrations live in migrations/NNNN_name.sql and are numbered from 0001
// without gaps. Each step of the plan adds its tables in a new file; files
// that have been applied anywhere are never edited.
//
//go:embed migrations/*.sql
var migrationFiles embed.FS

var migrationName = regexp.MustCompile(`^(\d{4})_([a-z0-9_]+)\.sql$`)

type migration struct {
	version int
	name    string
	sql     string
}

// loadMigrations reads the migration files in fsys, sorted by version.
func loadMigrations(fsys fs.FS) ([]migration, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("read migrations: %w", err)
	}
	var ms []migration
	for _, e := range entries {
		m := migrationName.FindStringSubmatch(e.Name())
		if e.IsDir() || m == nil {
			return nil, fmt.Errorf("migration %q: name must look like 0001_create_users.sql", e.Name())
		}
		version, err := strconv.Atoi(m[1])
		if err != nil {
			return nil, fmt.Errorf("migration %q: %w", e.Name(), err)
		}
		data, err := fs.ReadFile(fsys, e.Name())
		if err != nil {
			return nil, fmt.Errorf("read migration %q: %w", e.Name(), err)
		}
		ms = append(ms, migration{version: version, name: m[2], sql: string(data)})
	}
	slices.SortFunc(ms, func(a, b migration) int { return a.version - b.version })
	for i, m := range ms {
		if m.version != i+1 {
			return nil, fmt.Errorf("migration %04d_%s: expected version %04d, versions must go 1, 2, 3... without gaps or repeats", m.version, m.name, i+1)
		}
	}
	return ms, nil
}

// migrate applies the migrations the database does not have yet, each in its
// own transaction. It refuses a database migrated by a newer build.
func (s *Store) migrate(ctx context.Context) error {
	sub, err := fs.Sub(migrationFiles, "migrations")
	if err != nil {
		return fmt.Errorf("open migrations: %w", err)
	}
	ms, err := loadMigrations(sub)
	if err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    INTEGER PRIMARY KEY,
		name       TEXT    NOT NULL,
		applied_at INTEGER NOT NULL
	) STRICT`)
	if err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	current, err := s.SchemaVersion(ctx)
	if err != nil {
		return err
	}
	if current > len(ms) {
		return fmt.Errorf("database schema version %d is newer than this build supports (%d)", current, len(ms))
	}
	for _, m := range ms[current:] {
		if err := s.apply(ctx, m); err != nil {
			return fmt.Errorf("apply migration %04d_%s: %w", m.version, m.name, err)
		}
	}
	return nil
}

func (s *Store) apply(ctx context.Context, m migration) (err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer rollback(tx, &err)

	if _, err := tx.ExecContext(ctx, m.sql); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, ?)`,
		m.version, m.name, time.Now().Unix())
	if err != nil {
		return fmt.Errorf("record version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// SchemaVersion returns the version of the last applied migration, 0 for an
// empty database.
func (s *Store) SchemaVersion(ctx context.Context) (int, error) {
	var v int
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&v); err != nil {
		return 0, fmt.Errorf("read schema version: %w", err)
	}
	return v, nil
}
