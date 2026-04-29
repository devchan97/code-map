// Package store is the SQLite gateway for codemap's per-repo index.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/devchan97/code-map/internal/core"
	"github.com/devchan97/code-map/internal/lexical"
)

// Tx is the per-transaction store API used by the indexer and search layers.
// Implementations must be safe to call multiple times within a single transaction.
type Tx interface {
	// UpsertFile inserts or updates a files row.
	UpsertFile(f core.File) error

	// DeleteFileArtifacts removes all symbols and edges whose file column
	// matches the given repo-relative path. Associated tokens are removed via
	// the ON DELETE CASCADE constraint on the symbols table.
	DeleteFileArtifacts(file string) error

	// InsertSymbols inserts the given symbols and returns their assigned
	// database IDs in the same order as the input slice.
	InsertSymbols(syms []core.Symbol) ([]int64, error)

	// InsertEdges inserts the given edges, binding the provided file path to
	// each row's file column.
	InsertEdges(edges []core.Edge, file string) error

	// InsertTokens stores the lexical tokens for a single symbol.
	InsertTokens(symbolID int64, toks []lexical.Token) error

	// DeleteTokensForFile removes all token rows whose parent symbol belongs
	// to the given repo-relative file path (satisfies lexical.TokenStore).
	DeleteTokensForFile(file string) error

	// SearchBM25 executes a BM25 ranked query over the tokens table and
	// returns up to topN scoring symbols, applying the structural filters f
	// (satisfies lexical.TokenStore).
	SearchBM25(ctx context.Context, query string, topN int, f lexical.Filters) ([]lexical.ScoredID, error)

	// Stats returns the corpus size n and the average document length avgLen
	// needed for BM25 scoring (satisfies lexical.TokenStore).
	Stats(ctx context.Context) (n int, avgLen float64, err error)

	// ReadMeta reads the meta table into a core.Meta value.
	ReadMeta() (core.Meta, error)

	// WriteMeta upserts each known key from m into the meta table.
	WriteMeta(m core.Meta) error

	// ListFiles returns all rows from the files table.
	ListFiles() ([]core.File, error)

	// HydrateSymbols fetches full symbol rows for the given IDs. Results are
	// returned in the same order as ids.
	HydrateSymbols(ids []int64) ([]core.Symbol, error)

	// EdgesTo returns all edges whose to_qualname matches qualname.
	EdgesTo(qualname string) ([]core.Edge, error)

	// EdgesFrom returns all edges whose from_qualname matches qualname.
	EdgesFrom(qualname string) ([]core.Edge, error)

	// ResolveEdges sets resolved=1 on every edge whose to_qualname matches
	// a qualname present in the symbols table. Call once after all files for
	// an indexing run have been inserted.
	ResolveEdges() error

	// SymbolsByFile returns all symbols whose file column equals the given
	// repo-relative path, ordered by file then line_start.
	SymbolsByFile(file string) ([]core.Symbol, error)

	// SymbolsByQualname returns all symbols whose qualname exactly matches the
	// given string, ordered by file then line_start.
	SymbolsByQualname(qualname string) ([]core.Symbol, error)

	// AllSymbols returns every symbol in the index, optionally filtered by a
	// forward-slash glob pattern (fileGlob) and/or a set of symbol kinds.
	// An empty fileGlob or nil kinds means no filtering. Results are ordered
	// by file then line_start.
	AllSymbols(fileGlob string, kinds []core.SymbolKind) ([]core.Symbol, error)

	// AllEdges returns every edge in the index.
	AllEdges() ([]core.Edge, error)
}

// txImpl wraps a *sql.Tx plus the context active for the transaction.
type txImpl struct {
	tx  *sql.Tx
	ctx context.Context
}

// --- files ---

// UpsertFile inserts or updates a files row using an ON CONFLICT clause.
func (t *txImpl) UpsertFile(f core.File) error {
	const q = `
INSERT INTO files(path, sha1, indexed_at, language, size_bytes)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(path) DO UPDATE SET
    sha1       = excluded.sha1,
    indexed_at = excluded.indexed_at,
    language   = excluded.language,
    size_bytes = excluded.size_bytes`

	_, err := t.tx.ExecContext(t.ctx, q,
		f.Path,
		f.SHA1,
		f.IndexedAt.UTC().Format(time.RFC3339Nano),
		f.Language,
		f.SizeBytes,
	)
	if err != nil {
		return fmt.Errorf("store.UpsertFile %q: %w", f.Path, err)
	}
	return nil
}

// --- artifacts ---

// DeleteFileArtifacts removes all symbols and edges for the given file.
// The ON DELETE CASCADE constraint on the tokens table removes token rows when
// their parent symbol is deleted; the ON DELETE CASCADE on edges likewise.
// We also delete edges by their own file column to catch any that cascade missed.
func (t *txImpl) DeleteFileArtifacts(file string) error {
	if _, err := t.tx.ExecContext(t.ctx,
		"DELETE FROM symbols WHERE file = ?", file); err != nil {
		return fmt.Errorf("store.DeleteFileArtifacts symbols %q: %w", file, err)
	}
	if _, err := t.tx.ExecContext(t.ctx,
		"DELETE FROM edges WHERE file = ?", file); err != nil {
		return fmt.Errorf("store.DeleteFileArtifacts edges %q: %w", file, err)
	}
	return nil
}

// --- symbols ---

// InsertSymbols inserts symbols one by one and collects their LastInsertId.
func (t *txImpl) InsertSymbols(syms []core.Symbol) ([]int64, error) {
	const q = `
INSERT INTO symbols(name, qualname, kind, scope, file,
                    line_start, line_end, parent_qualname, docstring, snippet, vec)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	stmt, err := t.tx.PrepareContext(t.ctx, q)
	if err != nil {
		return nil, fmt.Errorf("store.InsertSymbols prepare: %w", err)
	}
	defer stmt.Close()

	ids := make([]int64, 0, len(syms))
	for i := range syms {
		s := &syms[i]
		var vecBlob interface{}
		if len(s.Vec) > 0 {
			vecBlob = encodeVec(s.Vec)
		}
		res, err := stmt.ExecContext(t.ctx,
			s.Name,
			s.Qualname,
			string(s.Kind),
			string(s.Scope),
			s.File,
			s.LineStart,
			s.LineEnd,
			nullableString(s.ParentQualname),
			nullableString(s.Docstring),
			s.Snippet,
			vecBlob,
		)
		if err != nil {
			return nil, fmt.Errorf("store.InsertSymbols %q: %w", s.Qualname, err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return nil, fmt.Errorf("store.InsertSymbols last id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// --- edges ---

// InsertEdges inserts edges and binds file to each row.
func (t *txImpl) InsertEdges(edges []core.Edge, file string) error {
	if len(edges) == 0 {
		return nil
	}
	const q = `
INSERT INTO edges(from_qualname, to_qualname, kind, resolved, file)
VALUES (?, ?, ?, ?, ?)`

	stmt, err := t.tx.PrepareContext(t.ctx, q)
	if err != nil {
		return fmt.Errorf("store.InsertEdges prepare: %w", err)
	}
	defer stmt.Close()

	for i := range edges {
		e := &edges[i]
		resolved := 0
		if e.Resolved {
			resolved = 1
		}
		if _, err := stmt.ExecContext(t.ctx,
			e.FromQualname, e.ToQualname, string(e.Kind), resolved, file,
		); err != nil {
			return fmt.Errorf("store.InsertEdges %q->%q: %w", e.FromQualname, e.ToQualname, err)
		}
	}
	return nil
}

// --- tokens ---

// InsertTokens stores the tokens for a single symbol.
func (t *txImpl) InsertTokens(symbolID int64, toks []lexical.Token) error {
	if len(toks) == 0 {
		return nil
	}
	const q = `INSERT INTO tokens(symbol_id, token, field, weight) VALUES (?, ?, ?, ?)`

	stmt, err := t.tx.PrepareContext(t.ctx, q)
	if err != nil {
		return fmt.Errorf("store.InsertTokens prepare: %w", err)
	}
	defer stmt.Close()

	for i := range toks {
		tok := &toks[i]
		if _, err := stmt.ExecContext(t.ctx, symbolID, tok.Text, tok.Field, tok.Weight); err != nil {
			return fmt.Errorf("store.InsertTokens symbol %d token %q: %w", symbolID, tok.Text, err)
		}
	}
	return nil
}

// DeleteTokensForFile removes all token rows for symbols that belong to file.
func (t *txImpl) DeleteTokensForFile(file string) error {
	const q = `
DELETE FROM tokens
WHERE symbol_id IN (SELECT id FROM symbols WHERE file = ?)`

	if _, err := t.tx.ExecContext(t.ctx, q, file); err != nil {
		return fmt.Errorf("store.DeleteTokensForFile %q: %w", file, err)
	}
	return nil
}

// --- BM25 search ---

// SearchBM25 implements lexical.TokenStore. It fetches candidate symbol IDs
// with per-term frequencies, then computes BM25 scores in Go.
//
// Dynamic SQL: the only dynamic part is building the IN (?, ?, ...) placeholder
// list from the query token count. Values are always bound — never interpolated.
func (t *txImpl) SearchBM25(ctx context.Context, query string, topN int, f lexical.Filters) ([]lexical.ScoredID, error) {
	queryTerms := lexical.TokenizeQuery(query)
	if len(queryTerms) == 0 {
		return nil, nil
	}

	// Deduplicate query terms.
	seen := make(map[string]struct{}, len(queryTerms))
	var terms []string
	for _, qt := range queryTerms {
		if _, ok := seen[qt]; !ok {
			seen[qt] = struct{}{}
			terms = append(terms, qt)
		}
	}

	// Issue #2: token IN (...) is exact-match only, so a query for "parse"
	// misses every "parser"/"parsed" symbol. Expand each query term of
	// length >= prefixExpandMin with stored tokens that begin with it.
	// SQLite uses the index on tokens(token) for `LIKE 'parse%'` because
	// the column is text and the pattern has no leading wildcard.
	//
	// We cap the expansion per term so that a one-letter prefix can't
	// pull in tens of thousands of tokens; that case stays exact-match.
	const (
		prefixExpandMin    = 3
		prefixExpandPerTok = 64
	)
	if len(terms) > 0 {
		expandedSet := make(map[string]struct{}, len(terms))
		for _, qt := range terms {
			expandedSet[qt] = struct{}{}
		}
		for _, qt := range terms {
			if len([]rune(qt)) < prefixExpandMin {
				continue
			}
			rows, err := t.tx.QueryContext(ctx,
				"SELECT DISTINCT token FROM tokens WHERE token LIKE ? ESCAPE '\\' LIMIT ?",
				escapeLike(qt)+"%", prefixExpandPerTok+1)
			if err != nil {
				return nil, fmt.Errorf("store.SearchBM25 expand %q: %w", qt, err)
			}
			count := 0
			overflow := false
			for rows.Next() {
				var tok string
				if err := rows.Scan(&tok); err != nil {
					rows.Close()
					return nil, fmt.Errorf("store.SearchBM25 expand scan: %w", err)
				}
				count++
				if count > prefixExpandPerTok {
					// Too many matches — skip expansion for this term;
					// the original exact term stays in expandedSet.
					overflow = true
					break
				}
				expandedSet[tok] = struct{}{}
			}
			rows.Close()
			_ = overflow
		}
		expanded := make([]string, 0, len(expandedSet))
		for tok := range expandedSet {
			expanded = append(expanded, tok)
		}
		terms = expanded
	}

	// Fetch corpus stats needed for BM25.
	n, avgLen, err := t.Stats(ctx)
	if err != nil {
		return nil, fmt.Errorf("store.SearchBM25 stats: %w", err)
	}
	if n == 0 {
		return nil, nil
	}

	// Build the base candidate query: symbol_id, token, weight, and per-symbol
	// total token count (docLen approximated as COUNT of token rows).
	// We join with the symbols table to apply kind/scope/file filters.
	placeholder := buildPlaceholders(len(terms))
	args := make([]interface{}, 0, len(terms)+10)
	for _, term := range terms {
		args = append(args, term)
	}

	// Build optional WHERE clauses for filters.
	var filterClauses []string
	if len(f.Kinds) > 0 {
		kp := buildPlaceholders(len(f.Kinds))
		filterClauses = append(filterClauses, "s.kind IN ("+kp+")")
		for _, k := range f.Kinds {
			args = append(args, string(k))
		}
	}
	if len(f.Scopes) > 0 {
		sp := buildPlaceholders(len(f.Scopes))
		filterClauses = append(filterClauses, "s.scope IN ("+sp+")")
		for _, sc := range f.Scopes {
			args = append(args, string(sc))
		}
	}
	if f.FileGlob != "" {
		// Convert doublestar glob to a LIKE pattern for SQLite.
		likePattern := globToLike(f.FileGlob)
		filterClauses = append(filterClauses, "s.file LIKE ?")
		args = append(args, likePattern)
	}

	filterSQL := ""
	if len(filterClauses) > 0 {
		filterSQL = " AND " + strings.Join(filterClauses, " AND ")
	}

	// Step 1: fetch (symbol_id, token, sum_weight) for matching terms.
	//
	// We also fetch the document frequency (df) for each token in one pass by
	// counting distinct symbol_ids per token.
	//
	// docLen per symbol = COUNT(*) of token rows for that symbol.
	// We approximate docLen with a subquery.
	candidateSQL := `
SELECT t.symbol_id, t.token, SUM(t.weight) AS tw,
       (SELECT COUNT(*) FROM tokens t2 WHERE t2.symbol_id = t.symbol_id) AS doc_len
FROM tokens t
JOIN symbols s ON s.id = t.symbol_id
WHERE t.token IN (` + placeholder + `)` + filterSQL + `
GROUP BY t.symbol_id, t.token`

	rows, err := t.tx.QueryContext(ctx, candidateSQL, args...)
	if err != nil {
		return nil, fmt.Errorf("store.SearchBM25 candidates: %w", err)
	}
	defer rows.Close()

	type termHit struct {
		tw     float64
		docLen float64
	}
	// symbolID → term → termHit
	symbolTerms := make(map[int64]map[string]termHit)
	for rows.Next() {
		var sid int64
		var tok string
		var tw, docLen float64
		if err := rows.Scan(&sid, &tok, &tw, &docLen); err != nil {
			return nil, fmt.Errorf("store.SearchBM25 scan: %w", err)
		}
		if symbolTerms[sid] == nil {
			symbolTerms[sid] = make(map[string]termHit)
		}
		symbolTerms[sid][tok] = termHit{tw: tw, docLen: docLen}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store.SearchBM25 rows: %w", err)
	}

	// Step 2: compute per-term document frequency.
	dfSQL := `
SELECT token, COUNT(DISTINCT symbol_id) AS df
FROM tokens
WHERE token IN (` + placeholder + `)
GROUP BY token`

	dfArgs := make([]interface{}, len(terms))
	for i, term := range terms {
		dfArgs[i] = term
	}
	dfRows, err := t.tx.QueryContext(ctx, dfSQL, dfArgs...)
	if err != nil {
		return nil, fmt.Errorf("store.SearchBM25 df: %w", err)
	}
	defer dfRows.Close()

	df := make(map[string]int, len(terms))
	for dfRows.Next() {
		var tok string
		var cnt int
		if err := dfRows.Scan(&tok, &cnt); err != nil {
			return nil, fmt.Errorf("store.SearchBM25 df scan: %w", err)
		}
		df[tok] = cnt
	}
	if err := dfRows.Err(); err != nil {
		return nil, fmt.Errorf("store.SearchBM25 df rows: %w", err)
	}

	// Step 3: score each candidate symbol.
	scored := make([]lexical.ScoredID, 0, len(symbolTerms))
	for sid, termMap := range symbolTerms {
		// Use the docLen from the first term hit (all rows for the same symbol
		// share the same value since it's a scalar subquery).
		var docLen float64
		for _, h := range termMap {
			docLen = h.docLen
			break
		}

		var total float64
		for _, term := range terms {
			h, ok := termMap[term]
			if !ok {
				continue
			}
			termDF := df[term]
			if termDF == 0 {
				termDF = 1
			}
			// tf is treated as the sum of weights for the term in this symbol.
			// We convert to an integer approximation for the Score function.
			tfInt := int(h.tw + 0.5)
			if tfInt < 1 {
				tfInt = 1
			}
			total += lexical.Score(tfInt, termDF, docLen, avgLen, n, lexical.BM25K1, lexical.BM25B)
		}
		scored = append(scored, lexical.ScoredID{SymbolID: sid, Score: total})
	}

	// Sort descending by score.
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].Score > scored[j].Score
	})

	if topN > 0 && len(scored) > topN {
		scored = scored[:topN]
	}
	return scored, nil
}

// Stats returns n (corpus size) and avgLen (average document length).
func (t *txImpl) Stats(ctx context.Context) (int, float64, error) {
	var n int
	if err := t.tx.QueryRowContext(ctx, "SELECT COUNT(DISTINCT id) FROM symbols").Scan(&n); err != nil {
		return 0, 0, fmt.Errorf("store.Stats count: %w", err)
	}
	if n == 0 {
		return 0, 0, nil
	}

	var avgLen float64
	const avgQ = `SELECT AVG(c) FROM (SELECT COUNT(*) c FROM tokens GROUP BY symbol_id)`
	if err := t.tx.QueryRowContext(ctx, avgQ).Scan(&avgLen); err != nil {
		return n, 0, fmt.Errorf("store.Stats avgLen: %w", err)
	}
	return n, avgLen, nil
}

// --- meta ---

// ReadMeta reads all known keys from the meta table into a core.Meta value.
func (t *txImpl) ReadMeta() (core.Meta, error) {
	rows, err := t.tx.QueryContext(t.ctx, "SELECT key, value FROM meta")
	if err != nil {
		return core.Meta{}, fmt.Errorf("store.ReadMeta query: %w", err)
	}
	defer rows.Close()

	kv := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return core.Meta{}, fmt.Errorf("store.ReadMeta scan: %w", err)
		}
		kv[k] = v
	}
	if err := rows.Err(); err != nil {
		return core.Meta{}, fmt.Errorf("store.ReadMeta rows: %w", err)
	}

	var m core.Meta

	if v, ok := kv["schema_ver"]; ok {
		m.SchemaVer, _ = strconv.Atoi(v)
	}
	if v, ok := kv["indexer_ver"]; ok {
		m.IndexerVer, _ = strconv.Atoi(v)
	}
	m.RepoRoot = kv["repo_root"]
	m.Embedder = kv["embedder"]
	if v, ok := kv["indexed_at"]; ok {
		m.IndexedAt, _ = time.Parse(time.RFC3339, v)
	}
	if v, ok := kv["symbol_count"]; ok {
		m.SymbolCount, _ = strconv.Atoi(v)
	}
	if v, ok := kv["file_count"]; ok {
		m.FileCount, _ = strconv.Atoi(v)
	}

	return m, nil
}

// WriteMeta upserts each known key from m into the meta table.
func (t *txImpl) WriteMeta(m core.Meta) error {
	type kv struct{ k, v string }
	pairs := []kv{
		{"schema_ver", strconv.Itoa(m.SchemaVer)},
		{"indexer_ver", strconv.Itoa(m.IndexerVer)},
		{"repo_root", m.RepoRoot},
		{"indexed_at", m.IndexedAt.UTC().Format(time.RFC3339)},
		{"embedder", m.Embedder},
		{"symbol_count", strconv.Itoa(m.SymbolCount)},
		{"file_count", strconv.Itoa(m.FileCount)},
	}

	const q = `
INSERT INTO meta(key, value) VALUES(?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value`

	stmt, err := t.tx.PrepareContext(t.ctx, q)
	if err != nil {
		return fmt.Errorf("store.WriteMeta prepare: %w", err)
	}
	defer stmt.Close()

	for _, p := range pairs {
		if _, err := stmt.ExecContext(t.ctx, p.k, p.v); err != nil {
			return fmt.Errorf("store.WriteMeta key %q: %w", p.k, err)
		}
	}
	return nil
}

// --- files list ---

// ListFiles returns all rows from the files table.
func (t *txImpl) ListFiles() ([]core.File, error) {
	const q = `SELECT path, sha1, indexed_at, language, size_bytes FROM files`
	rows, err := t.tx.QueryContext(t.ctx, q)
	if err != nil {
		return nil, fmt.Errorf("store.ListFiles query: %w", err)
	}
	defer rows.Close()

	var files []core.File
	for rows.Next() {
		var f core.File
		var indexedAtStr string
		if err := rows.Scan(&f.Path, &f.SHA1, &indexedAtStr, &f.Language, &f.SizeBytes); err != nil {
			return nil, fmt.Errorf("store.ListFiles scan: %w", err)
		}
		f.IndexedAt, _ = time.Parse(time.RFC3339Nano, indexedAtStr)
		files = append(files, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store.ListFiles rows: %w", err)
	}
	return files, nil
}

// --- hydrate symbols ---

// HydrateSymbols fetches full symbol rows for the given IDs, returning results
// in the same order as the input ids slice.
func (t *txImpl) HydrateSymbols(ids []int64) ([]core.Symbol, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	placeholder := buildPlaceholders(len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		args[i] = id
	}

	q := `
SELECT id, name, qualname, kind, scope, file,
       line_start, line_end, parent_qualname, docstring, snippet, vec
FROM symbols
WHERE id IN (` + placeholder + `)`

	rows, err := t.tx.QueryContext(t.ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("store.HydrateSymbols query: %w", err)
	}
	defer rows.Close()

	byID := make(map[int64]core.Symbol, len(ids))
	for rows.Next() {
		var s core.Symbol
		var parentQ, docstring sql.NullString
		var vecBlob []byte
		if err := rows.Scan(
			&s.ID, &s.Name, &s.Qualname,
			(*string)(&s.Kind), (*string)(&s.Scope),
			&s.File, &s.LineStart, &s.LineEnd,
			&parentQ, &docstring, &s.Snippet, &vecBlob,
		); err != nil {
			return nil, fmt.Errorf("store.HydrateSymbols scan: %w", err)
		}
		if parentQ.Valid {
			s.ParentQualname = parentQ.String
		}
		if docstring.Valid {
			s.Docstring = docstring.String
		}
		if len(vecBlob) > 0 {
			s.Vec = decodeVec(vecBlob)
		}
		byID[s.ID] = s
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store.HydrateSymbols rows: %w", err)
	}

	// Preserve input order.
	result := make([]core.Symbol, 0, len(ids))
	for _, id := range ids {
		if s, ok := byID[id]; ok {
			result = append(result, s)
		}
	}
	return result, nil
}

// --- edges ---

// EdgesTo returns all edges whose to_qualname matches qualname.
func (t *txImpl) EdgesTo(qualname string) ([]core.Edge, error) {
	const q = `
SELECT from_qualname, to_qualname, kind, resolved
FROM edges
WHERE to_qualname = ?`

	return t.queryEdges(q, qualname)
}

// EdgesFrom returns all edges whose from_qualname matches qualname.
func (t *txImpl) EdgesFrom(qualname string) ([]core.Edge, error) {
	const q = `
SELECT from_qualname, to_qualname, kind, resolved
FROM edges
WHERE from_qualname = ?`

	return t.queryEdges(q, qualname)
}

func (t *txImpl) queryEdges(q, qualname string) ([]core.Edge, error) {
	rows, err := t.tx.QueryContext(t.ctx, q, qualname)
	if err != nil {
		return nil, fmt.Errorf("store.queryEdges %q: %w", qualname, err)
	}
	defer rows.Close()

	var edges []core.Edge
	for rows.Next() {
		var e core.Edge
		var resolved int
		if err := rows.Scan(&e.FromQualname, &e.ToQualname, (*string)(&e.Kind), &resolved); err != nil {
			return nil, fmt.Errorf("store.queryEdges scan: %w", err)
		}
		e.Resolved = resolved != 0
		edges = append(edges, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store.queryEdges rows: %w", err)
	}
	return edges, nil
}

// ResolveEdges sets resolved=1 on every edge whose to_qualname matches a
// qualname present in the symbols table.
func (t *txImpl) ResolveEdges() error {
	const q = `
UPDATE edges SET resolved = 1
WHERE to_qualname IN (SELECT qualname FROM symbols)`

	if _, err := t.tx.ExecContext(t.ctx, q); err != nil {
		return fmt.Errorf("store.ResolveEdges: %w", err)
	}
	return nil
}

// --- symbols by file / qualname ---

// SymbolsByFile returns all symbols whose file column equals the given
// repo-relative path, ordered by file then line_start.
func (t *txImpl) SymbolsByFile(file string) ([]core.Symbol, error) {
	const q = `
SELECT id, name, qualname, kind, scope, file,
       line_start, line_end, parent_qualname, docstring, snippet, vec
FROM symbols
WHERE file = ?
ORDER BY file, line_start`

	return t.querySymbols(q, file)
}

// SymbolsByQualname returns all symbols whose qualname exactly matches the
// given string, ordered by file then line_start.
func (t *txImpl) SymbolsByQualname(qualname string) ([]core.Symbol, error) {
	const q = `
SELECT id, name, qualname, kind, scope, file,
       line_start, line_end, parent_qualname, docstring, snippet, vec
FROM symbols
WHERE qualname = ?
ORDER BY file, line_start`

	return t.querySymbols(q, qualname)
}

// querySymbols is a shared row scanner for SymbolsByFile and SymbolsByQualname.
func (t *txImpl) querySymbols(query, arg string) ([]core.Symbol, error) {
	rows, err := t.tx.QueryContext(t.ctx, query, arg)
	if err != nil {
		return nil, fmt.Errorf("store.querySymbols: %w", err)
	}
	defer rows.Close()

	var syms []core.Symbol
	for rows.Next() {
		var s core.Symbol
		var parentQ, docstring sql.NullString
		var vecBlob []byte
		if err := rows.Scan(
			&s.ID, &s.Name, &s.Qualname,
			(*string)(&s.Kind), (*string)(&s.Scope),
			&s.File, &s.LineStart, &s.LineEnd,
			&parentQ, &docstring, &s.Snippet, &vecBlob,
		); err != nil {
			return nil, fmt.Errorf("store.querySymbols scan: %w", err)
		}
		if parentQ.Valid {
			s.ParentQualname = parentQ.String
		}
		if docstring.Valid {
			s.Docstring = docstring.String
		}
		if len(vecBlob) > 0 {
			s.Vec = decodeVec(vecBlob)
		}
		syms = append(syms, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store.querySymbols rows: %w", err)
	}
	return syms, nil
}

// AllSymbols returns every symbol, optionally filtered by fileGlob and/or kinds.
// Results are ordered by file then line_start.
func (t *txImpl) AllSymbols(fileGlob string, kinds []core.SymbolKind) ([]core.Symbol, error) {
	var whereClauses []string
	var args []interface{}

	if fileGlob != "" {
		whereClauses = append(whereClauses, "file LIKE ?")
		args = append(args, globToLike(fileGlob))
	}

	if len(kinds) > 0 {
		ph := buildPlaceholders(len(kinds))
		whereClauses = append(whereClauses, "kind IN ("+ph+")")
		for _, k := range kinds {
			args = append(args, string(k))
		}
	}

	q := `SELECT id, name, qualname, kind, scope, file,
	       line_start, line_end, parent_qualname, docstring, snippet, vec
	FROM symbols`
	if len(whereClauses) > 0 {
		q += " WHERE " + strings.Join(whereClauses, " AND ")
	}
	q += " ORDER BY file, line_start"

	rows, err := t.tx.QueryContext(t.ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("store.AllSymbols query: %w", err)
	}
	defer rows.Close()

	var syms []core.Symbol
	for rows.Next() {
		var s core.Symbol
		var parentQ, docstring sql.NullString
		var vecBlob []byte
		if err := rows.Scan(
			&s.ID, &s.Name, &s.Qualname,
			(*string)(&s.Kind), (*string)(&s.Scope),
			&s.File, &s.LineStart, &s.LineEnd,
			&parentQ, &docstring, &s.Snippet, &vecBlob,
		); err != nil {
			return nil, fmt.Errorf("store.AllSymbols scan: %w", err)
		}
		if parentQ.Valid {
			s.ParentQualname = parentQ.String
		}
		if docstring.Valid {
			s.Docstring = docstring.String
		}
		if len(vecBlob) > 0 {
			s.Vec = decodeVec(vecBlob)
		}
		syms = append(syms, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store.AllSymbols rows: %w", err)
	}
	return syms, nil
}

// AllEdges returns every edge in the index.
func (t *txImpl) AllEdges() ([]core.Edge, error) {
	const q = `SELECT from_qualname, to_qualname, kind, resolved FROM edges`
	rows, err := t.tx.QueryContext(t.ctx, q)
	if err != nil {
		return nil, fmt.Errorf("store.AllEdges query: %w", err)
	}
	defer rows.Close()

	var edges []core.Edge
	for rows.Next() {
		var e core.Edge
		var resolved int
		if err := rows.Scan(&e.FromQualname, &e.ToQualname, (*string)(&e.Kind), &resolved); err != nil {
			return nil, fmt.Errorf("store.AllEdges scan: %w", err)
		}
		e.Resolved = resolved != 0
		edges = append(edges, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store.AllEdges rows: %w", err)
	}
	return edges, nil
}

// --- helpers ---

// buildPlaceholders returns a comma-separated string of n question-mark
// bind-parameter placeholders, e.g. "?, ?, ?" for n=3.
func buildPlaceholders(n int) string {
	if n <= 0 {
		return ""
	}
	sb := strings.Builder{}
	for i := 0; i < n; i++ {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteByte('?')
	}
	return sb.String()
}

// nullableString returns nil for the empty string and the value otherwise,
// so that optional TEXT columns store NULL rather than "".
func nullableString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

// globToLike converts a simple forward-slash doublestar glob pattern to a
// SQLite LIKE pattern. Only the common wildcard characters (* and ?) are
// translated; this is sufficient for file-path filtering.
//
// Conversion rules:
//   - "**" → "%"  (matches any path component sequence)
//   - "*"  → "%"  (matches any sequence within one component — simplified)
//   - "?"  → "_"  (matches exactly one character)
//   - "%", "_" in the input are escaped with "\" to avoid false matches.
func globToLike(glob string) string {
	// Normalise to forward slashes (glob patterns are defined as forward slash).
	glob = filepath.ToSlash(glob)

	var sb strings.Builder
	i := 0
	for i < len(glob) {
		c := glob[i]
		switch c {
		case '%', '_':
			sb.WriteByte('\\')
			sb.WriteByte(c)
		case '*':
			// Consume a potential second '*' for "**".
			if i+1 < len(glob) && glob[i+1] == '*' {
				i++
			}
			sb.WriteByte('%')
		case '?':
			sb.WriteByte('_')
		default:
			sb.WriteByte(c)
		}
		i++
	}
	return sb.String()
}

// escapeLike escapes the LIKE wildcards (`%`, `_`) and the escape char `\`
// itself so a user-provided string can be safely used as the literal portion
// of a `WHERE col LIKE 'literal%' ESCAPE '\'` query.
func escapeLike(s string) string {
	if !strings.ContainsAny(s, `%_\`) {
		return s
	}
	var sb strings.Builder
	sb.Grow(len(s) + 4)
	for _, r := range s {
		switch r {
		case '%', '_', '\\':
			sb.WriteByte('\\')
		}
		sb.WriteRune(r)
	}
	return sb.String()
}

// encodeVec encodes a []float32 to a little-endian byte slice with a 4-byte
// length prefix (number of float32 elements). Format matches the encoder
// package's blob convention: 4-byte LE length header + payload.
func encodeVec(v []float32) []byte {
	n := len(v)
	b := make([]byte, 4+n*4)
	b[0] = byte(n)
	b[1] = byte(n >> 8)
	b[2] = byte(n >> 16)
	b[3] = byte(n >> 24)
	for i, f := range v {
		bits := math.Float32bits(f)
		off := 4 + i*4
		b[off] = byte(bits)
		b[off+1] = byte(bits >> 8)
		b[off+2] = byte(bits >> 16)
		b[off+3] = byte(bits >> 24)
	}
	return b
}

// decodeVec decodes a []float32 from the format produced by encodeVec.
func decodeVec(b []byte) []float32 {
	if len(b) < 4 {
		return nil
	}
	n := int(b[0]) | int(b[1])<<8 | int(b[2])<<16 | int(b[3])<<24
	if n < 0 || len(b) < 4+n*4 {
		return nil
	}
	v := make([]float32, n)
	for i := range v {
		off := 4 + i*4
		bits := uint32(b[off]) | uint32(b[off+1])<<8 | uint32(b[off+2])<<16 | uint32(b[off+3])<<24
		v[i] = math.Float32frombits(bits)
	}
	return v
}
