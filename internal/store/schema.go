// Package store is the SQLite gateway for codemap's per-repo index.
package store

// SchemaVer is the current on-disk schema version.
const SchemaVer = 1

// schemaSQL is the DDL applied to a fresh database.
// Statements are separated by semicolons and applied via a single db.Exec call.
// PRAGMAs are applied separately in Open after the connection is established.
const schemaSQL = `
CREATE TABLE IF NOT EXISTS meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS files (
    path        TEXT PRIMARY KEY,
    sha1        TEXT NOT NULL,
    indexed_at  TEXT NOT NULL,
    language    TEXT NOT NULL,
    size_bytes  INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_files_indexed_at ON files(indexed_at);

CREATE TABLE IF NOT EXISTS symbols (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    name            TEXT NOT NULL,
    qualname        TEXT NOT NULL,
    kind            TEXT NOT NULL,
    scope           TEXT NOT NULL,
    file            TEXT NOT NULL,
    line_start      INTEGER NOT NULL,
    line_end        INTEGER NOT NULL,
    parent_qualname TEXT,
    docstring       TEXT,
    snippet         TEXT NOT NULL,
    vec             BLOB,
    FOREIGN KEY(file) REFERENCES files(path) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_symbols_file       ON symbols(file);
CREATE INDEX IF NOT EXISTS idx_symbols_qualname   ON symbols(qualname);
CREATE INDEX IF NOT EXISTS idx_symbols_kind_scope ON symbols(kind, scope);

CREATE TABLE IF NOT EXISTS edges (
    from_qualname TEXT NOT NULL,
    to_qualname   TEXT NOT NULL,
    kind          TEXT NOT NULL,
    resolved      INTEGER NOT NULL,
    file          TEXT NOT NULL,
    FOREIGN KEY(file) REFERENCES files(path) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_edges_to   ON edges(to_qualname);
CREATE INDEX IF NOT EXISTS idx_edges_from ON edges(from_qualname);
CREATE INDEX IF NOT EXISTS idx_edges_file ON edges(file);

CREATE TABLE IF NOT EXISTS tokens (
    symbol_id  INTEGER NOT NULL,
    token      TEXT NOT NULL,
    field      TEXT NOT NULL,
    weight     REAL NOT NULL,
    FOREIGN KEY(symbol_id) REFERENCES symbols(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_tokens_token ON tokens(token);
`
