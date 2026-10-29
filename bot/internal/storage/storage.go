// Package storage keeps the bot data in SQLite.
package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

// dsnParams are applied by modernc.org/sqlite to every new connection: wait
// up to 5 s for a lock instead of failing, WAL so that reads do not block the
// writer, foreign keys on, and BEGIN IMMEDIATE so that a write transaction
// takes the write lock up front rather than failing midway.
const dsnParams = "?_busy_timeout=5000&_journal_mode=WAL&_synchronous=NORMAL&_foreign_keys=1&_txlock=immediate"

// maxOpenConns bounds the pool: SQLite has a single writer anyway, and each
// connection carries its own page cache.
const maxOpenConns = 4

// Store is the bot database. It is safe for concurrent use.
type Store struct {
	db *sql.DB
}

// Open opens the database at path, creating the file and its directory if
// needed, and applies pending migrations.
func Open(ctx context.Context, path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}
	db, err := sql.Open("sqlite", path+dsnParams)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	db.SetMaxOpenConns(maxOpenConns)

	s := &Store{db: db}
	if err := db.PingContext(ctx); err != nil {
		return nil, errors.Join(fmt.Errorf("connect to database: %w", err), db.Close())
	}
	if err := s.migrate(ctx); err != nil {
		return nil, errors.Join(fmt.Errorf("migrate database: %w", err), db.Close())
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error {
	return s.db.Close()
}

// rollback undoes tx unless it has been committed, adding a failed rollback
// to *err.
func rollback(tx *sql.Tx, err *error) {
	if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
		*err = errors.Join(*err, fmt.Errorf("rollback: %w", rbErr))
	}
}
