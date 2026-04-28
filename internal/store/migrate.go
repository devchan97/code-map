// Package store is the SQLite gateway for codemap's per-repo index.
package store

import (
	"database/sql"
	"fmt"
	"strconv"

	"github.com/devchan97/code-map/internal/core"
)

// CheckSchema validates the on-disk schema_ver against SchemaVer.
// Returns core.ErrSchemaMismatch if they differ, wrapped with human-readable context.
// For v1, no auto-migration is performed — the caller must run `codemap reindex`.
func CheckSchema(db *sql.DB) error {
	var val string
	err := db.QueryRow("SELECT value FROM meta WHERE key = 'schema_ver'").Scan(&val)
	if err == sql.ErrNoRows {
		// No schema_ver row means the DB was created by an older tool version
		// or is partially initialised — treat as mismatch.
		return fmt.Errorf(
			"store: schema version missing (supported %d): run `codemap reindex`: %w",
			SchemaVer, core.ErrSchemaMismatch,
		)
	}
	if err != nil {
		return fmt.Errorf("store: read schema_ver: %w", err)
	}

	onDisk, err := strconv.Atoi(val)
	if err != nil {
		return fmt.Errorf("store: parse schema_ver %q: %w", val, err)
	}

	if onDisk != SchemaVer {
		return fmt.Errorf(
			"store: schema version %d on disk, supported %d: run `codemap reindex`: %w",
			onDisk, SchemaVer, core.ErrSchemaMismatch,
		)
	}
	return nil
}
