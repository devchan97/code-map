// Package store is the SQLite gateway for codemap's per-repo index.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"

	"github.com/devchan97/code-map/internal/platform"

	// Pure-Go SQLite driver; imported for its side-effect of registering the
	// "sqlite" database/sql driver.
	_ "modernc.org/sqlite"
)

// Store holds an open SQLite handle for a single repo's .codemap/index.db.
type Store struct {
	db       *sql.DB
	repoRoot string
}

// Open returns a Store for repoRoot. If <repoRoot>/.codemap/index.db does not
// exist the schema is initialised; otherwise the existing schema_ver is checked
// and core.ErrSchemaMismatch is returned on version skew.
func Open(repoRoot string) (*Store, error) {
	dir := platform.RepoCodemapDir(repoRoot)
	if err := platform.EnsureDir(platform.FromSlash(dir)); err != nil {
		return nil, fmt.Errorf("store.Open: ensure dir: %w", err)
	}

	dbPath := filepath.ToSlash(filepath.Join(platform.FromSlash(dir), "index.db"))

	// The _pragma URI option sets foreign_keys at connection open time as well;
	// we also apply it manually below for clarity.
	dsn := "file:" + dbPath + "?_pragma=foreign_keys%3Don"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store.Open: sql.Open: %w", err)
	}

	// Apply PRAGMAs in the required order.
	pragmas := []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA foreign_keys = ON",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("store.Open: %s: %w", p, err)
		}
	}

	// Detect whether this is a fresh database by checking for the meta table.
	var tableCount int
	row := db.QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='meta'",
	)
	if err := row.Scan(&tableCount); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store.Open: probe schema: %w", err)
	}

	if tableCount == 0 {
		// Fresh database — apply DDL and seed the schema_ver row so subsequent
		// opens pass CheckSchema.
		if _, err := db.Exec(schemaSQL); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("store.Open: apply schema: %w", err)
		}
		if _, err := db.Exec(
			`INSERT INTO meta(key, value) VALUES ('schema_ver', ?)`,
			fmt.Sprintf("%d", SchemaVer),
		); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("store.Open: seed schema_ver: %w", err)
		}
	} else {
		// Existing database — verify schema version.
		if err := CheckSchema(db); err != nil {
			_ = db.Close()
			return nil, err
		}
	}

	return &Store{db: db, repoRoot: platform.ToSlash(repoRoot)}, nil
}

// Close releases the database handle.
func (s *Store) Close() error {
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("store.Close: %w", err)
	}
	return nil
}

// RepoRoot returns the absolute repo root associated with this store,
// forward-slash normalised.
func (s *Store) RepoRoot() string {
	return s.repoRoot
}

// WithTx runs fn within a transaction. The transaction is committed when fn
// returns nil; it is rolled back if fn returns an error or panics.
func (s *Store) WithTx(ctx context.Context, fn func(Tx) error) (retErr error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store.WithTx: begin: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p) // re-panic after rollback
		}
		if retErr != nil {
			_ = tx.Rollback()
		}
	}()

	impl := &txImpl{tx: tx, ctx: ctx}
	if err := fn(impl); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store.WithTx: commit: %w", err)
	}
	return nil
}
