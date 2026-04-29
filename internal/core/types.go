package core

import "time"

// File represents a source file that has been indexed.
type File struct {
	// Path is the repo-relative file path, always forward-slash separated.
	Path string `json:"path"`
	// SHA1 is the hex SHA-1 digest of the file contents at index time.
	SHA1 string `json:"sha1"`
	// IndexedAt is the UTC timestamp when this file was last indexed.
	IndexedAt time.Time `json:"indexed_at"`
	// Language is the detected programming language (e.g. "python", "go").
	Language string `json:"language"`
	// SizeBytes is the file size in bytes at index time.
	SizeBytes int64 `json:"size_bytes"`
}

// Symbol represents a named entity (function, class, variable, etc.) extracted from source.
type Symbol struct {
	// ID is the database primary key for this symbol.
	ID int64 `json:"id"`
	// Name is the short identifier name (e.g. "compute_total").
	Name string `json:"name"`
	// Qualname is the fully qualified name (e.g. "app.billing.compute_total").
	Qualname string `json:"qualname"`
	// Kind is the symbol classification (function, method, class, etc.).
	Kind SymbolKind `json:"kind"`
	// Scope is the lexical scope of the symbol (global, class, local, param).
	Scope Scope `json:"scope"`
	// File is the repo-relative path of the file containing this symbol.
	File string `json:"file"`
	// LineStart is the 1-based line number where the symbol definition begins.
	LineStart int `json:"line_start"`
	// LineEnd is the 1-based line number where the symbol definition ends.
	LineEnd int `json:"line_end"`
	// ParentQualname is the qualified name of the enclosing symbol, if any.
	ParentQualname string `json:"parent_qualname,omitempty"`
	// Docstring is the extracted documentation comment, if any.
	Docstring string `json:"docstring,omitempty"`
	// Snippet is a short source excerpt (typically 6–10 lines).
	Snippet string `json:"snippet"`
	// Vec is the optional dense embedding vector; not serialized to JSON.
	Vec []float32 `json:"-"`
}

// Edge represents a directed relationship between two symbols.
type Edge struct {
	// FromQualname is the qualified name of the source symbol.
	FromQualname string `json:"from_qualname"`
	// ToQualname is the qualified name of the target symbol (may be unresolved).
	ToQualname string `json:"to_qualname"`
	// Kind is the type of relationship (call, reference, inherit, import).
	Kind EdgeKind `json:"kind"`
	// Resolved indicates whether ToQualname was matched to a known symbol.
	Resolved bool `json:"resolved"`
}

// Meta holds the top-level metadata for a repo index.
type Meta struct {
	// SchemaVer is the integer schema version stored in the index.
	// Bumped when the on-disk SQLite layout changes; mismatch is a hard
	// error (the DB literally can't be read by a newer/older binary).
	SchemaVer int `json:"schema_ver"`
	// IndexerVer is bumped when the meaning of the data changes — a
	// new tokenizer rule, a parser that emits qualnames differently,
	// etc. The DB is still readable across an indexer_ver skew, but
	// search results will be wrong until the user runs `codemap
	// reindex`. Surfaced via `codemap status` rather than failing
	// open(); see internal/store/schema.go for the current value.
	IndexerVer int `json:"indexer_ver"`
	// RepoRoot is the absolute path to the indexed repository root.
	RepoRoot string `json:"repo_root"`
	// IndexedAt is the UTC timestamp of the most recent successful index run.
	IndexedAt time.Time `json:"indexed_at"`
	// Embedder identifies the retrieval backend used (e.g. "lexical", "bge-small").
	Embedder string `json:"embedder"`
	// SymbolCount is the total number of symbols in the index.
	SymbolCount int `json:"symbol_count"`
	// FileCount is the total number of indexed files.
	FileCount int `json:"file_count"`
}

// SearchHit is a single result returned by a search query.
type SearchHit struct {
	// File is the repo-relative path of the file containing the matching symbol.
	File string `json:"file"`
	// LineStart is the 1-based start line of the matching symbol.
	LineStart int `json:"line_start"`
	// LineEnd is the 1-based end line of the matching symbol.
	LineEnd int `json:"line_end"`
	// Qualname is the fully qualified name of the matching symbol.
	Qualname string `json:"qualname"`
	// Kind is the symbol kind of the matching symbol.
	Kind SymbolKind `json:"kind"`
	// Scope is the lexical scope of the matching symbol.
	Scope Scope `json:"scope"`
	// Snippet is the source excerpt for the matching symbol.
	Snippet string `json:"snippet"`
	// Score is the retrieval relevance score.
	Score float64 `json:"score"`
	// IndexedAt is the timestamp when the file containing this symbol was last indexed.
	IndexedAt time.Time `json:"indexed_at"`
}

// RegistryEntry is a single row in the global ~/.codemap/registry.toml.
type RegistryEntry struct {
	// Name is the short human-readable slug for this repo.
	Name string `json:"name"         toml:"name"`
	// Path is the absolute filesystem path to the repo root.
	Path string `json:"path"         toml:"path"`
	// CreatedAt is the UTC timestamp when this entry was first registered.
	CreatedAt time.Time `json:"created_at"   toml:"created_at"`
	// LastIndexed is the UTC timestamp of the most recent successful index run.
	LastIndexed time.Time `json:"last_indexed" toml:"last_indexed"`
	// FileCount is the number of files in the most recent index.
	FileCount int `json:"file_count"   toml:"file_count"`
	// SymbolCount is the number of symbols in the most recent index.
	SymbolCount int `json:"symbol_count" toml:"symbol_count"`
	// Embedder identifies the retrieval backend used for this index.
	Embedder string `json:"embedder"     toml:"embedder"`
	// SchemaVer is the schema version of the index database.
	SchemaVer int `json:"schema_ver"   toml:"schema_ver"`
}
