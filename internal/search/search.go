// Package search runs lexical (and optionally encoder rerank) retrieval over a codemap index.
package search

import (
	"context"
	"fmt"
	"log/slog"
	"math"

	"github.com/devchan97/code-map/internal/core"
	"github.com/devchan97/code-map/internal/encoder"
	"github.com/devchan97/code-map/internal/lexical"
	"github.com/devchan97/code-map/internal/store"
)

// Query bundles all parameters for a search call.
type Query struct {
	// Text is the search query string.
	Text string
	// TopN is the maximum number of results to return; defaults to 10 when 0.
	TopN int
	// Kinds restricts results to the given symbol kinds.
	Kinds []core.SymbolKind
	// Scopes restricts results to the given lexical scopes.
	Scopes []core.Scope
	// FileGlob is a forward-slash doublestar glob pattern restricting which files are searched.
	FileGlob string
	// Rerank requests encoder-based cosine reranking when an encoder is available.
	Rerank bool
}

// Run executes a search against the codemap index in st.
//
// Steps:
//  1. BM25-fetch up to 50 candidate symbol IDs (subject to kind/scope/file filters).
//  2. Hydrate full Symbol rows for those IDs.
//  3. If Rerank == true and enc != nil: encode the query and each snippet,
//     cosine-rerank the candidates and trim to TopN.
//  4. Otherwise: take TopN from the BM25-ordered list.
//  5. Decorate each symbol as a core.SearchHit, attaching the file's IndexedAt.
func Run(ctx context.Context, st *store.Store, enc encoder.Encoder, q Query) ([]core.SearchHit, error) {
	topN := q.TopN
	if topN <= 0 {
		topN = 10
	}

	var hits []core.SearchHit

	err := st.WithTx(ctx, func(tx store.Tx) error {
		filters := lexical.Filters{
			Kinds:    q.Kinds,
			Scopes:   q.Scopes,
			FileGlob: q.FileGlob,
		}

		// Step 1: BM25 — fetch up to 50 candidates.
		scored, err := tx.SearchBM25(ctx, q.Text, 50, filters)
		if err != nil {
			return fmt.Errorf("search.Run BM25: %w", err)
		}
		if len(scored) == 0 {
			return nil
		}

		// Build score map for re-zip after hydration.
		scoreByID := make(map[int64]float64, len(scored))
		ids := make([]int64, 0, len(scored))
		for _, s := range scored {
			ids = append(ids, s.SymbolID)
			scoreByID[s.SymbolID] = s.Score
		}

		// Step 2: hydrate full Symbol rows.
		symbols, err := tx.HydrateSymbols(ids)
		if err != nil {
			return fmt.Errorf("search.Run hydrate: %w", err)
		}

		// Build file map for IndexedAt lookup.
		files, err := tx.ListFiles()
		if err != nil {
			return fmt.Errorf("search.Run list files: %w", err)
		}
		fileMap := make(map[string]core.File, len(files))
		for _, f := range files {
			fileMap[f.Path] = f
		}

		// Step 3 or 4: rerank or BM25-trim.
		if q.Rerank && enc == nil {
			// Rerank was requested but no encoder is available (e.g. stub build).
			// Log a warning and continue with BM25 alone — do NOT error.
			slog.Warn("search.Run: --rerank requested but encoder is unavailable; falling back to BM25 (rebuild with -tags encoder to enable)")
		}
		if q.Rerank && enc != nil {
			hits, err = rerankCosine(ctx, enc, symbols, scoreByID, fileMap, topN, q.Text)
			if err != nil {
				// Fall back to BM25 ordering with a warning.
				slog.Warn("search.Run encoder rerank failed; falling back to BM25", "err", err)
				hits = buildHits(symbols, scoreByID, fileMap, topN)
			}
		} else {
			hits = buildHits(symbols, scoreByID, fileMap, topN)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}
	return hits, nil
}

// rankedSymbol pairs a symbol with its retrieval score for sorting.
type rankedSymbol struct {
	sym   core.Symbol
	score float64
}

// rerankCosine encodes the query and each candidate's snippet, then reranks by
// cosine similarity, returning the top topN results.
//
// If the query vector has zero norm (the encoder produced a degenerate embedding),
// we fall back to BM25 ordering for all candidates rather than silently returning
// a meaningless cosine ranking.
func rerankCosine(
	_ context.Context,
	enc encoder.Encoder,
	symbols []core.Symbol,
	scoreByID map[int64]float64,
	fileMap map[string]core.File,
	topN int,
	queryText string,
) ([]core.SearchHit, error) {
	qVec, err := enc.EncodeQuery(queryText)
	if err != nil {
		return nil, fmt.Errorf("search.rerankCosine EncodeQuery: %w", err)
	}

	snippets := make([]string, len(symbols))
	for i, s := range symbols {
		snippets[i] = s.Snippet
	}

	vecs, err := enc.EncodeBatch(snippets)
	if err != nil {
		return nil, fmt.Errorf("search.rerankCosine EncodeBatch: %w", err)
	}

	qNorm := vecNorm(qVec)

	// If the query vector is degenerate (zero norm), cosine similarity is
	// undefined for all candidates. Fall back to BM25 ordering.
	if qNorm == 0 {
		slog.Warn("search.rerankCosine: query vector has zero norm; falling back to BM25 ordering")
		return buildHits(symbols, scoreByID, fileMap, topN), nil
	}

	candidates := make([]rankedSymbol, 0, len(symbols))
	for i, sym := range symbols {
		var score float64
		if i < len(vecs) {
			score = cosine(qVec, vecs[i], qNorm)
		}
		// Note: a cosine score of 0 (orthogonal vectors) is a legitimate result;
		// we do NOT fall back to BM25 here. Only a zero-norm query vector (handled
		// above) triggers a BM25 fallback.
		candidates = append(candidates, rankedSymbol{sym: sym, score: score})
	}

	// Sort descending by cosine score.
	sortRanked(candidates)

	// Trim to topN.
	if topN > 0 && len(candidates) > topN {
		candidates = candidates[:topN]
	}

	hits := make([]core.SearchHit, 0, len(candidates))
	for _, c := range candidates {
		hits = append(hits, symbolToHit(c.sym, c.score, fileMap))
	}
	return hits, nil
}

// buildHits constructs SearchHit slices from already-BM25-ordered symbols,
// preserving BM25 order and trimming to topN.
func buildHits(symbols []core.Symbol, scoreByID map[int64]float64, fileMap map[string]core.File, topN int) []core.SearchHit {
	n := len(symbols)
	if topN > 0 && n > topN {
		n = topN
	}
	hits := make([]core.SearchHit, 0, n)
	for i := 0; i < n; i++ {
		s := symbols[i]
		hits = append(hits, symbolToHit(s, scoreByID[s.ID], fileMap))
	}
	return hits
}

// symbolToHit converts a core.Symbol (plus its BM25/cosine score and the
// file map) into a core.SearchHit.
func symbolToHit(s core.Symbol, score float64, fileMap map[string]core.File) core.SearchHit {
	hit := core.SearchHit{
		File:      s.File,
		LineStart: s.LineStart,
		LineEnd:   s.LineEnd,
		Qualname:  s.Qualname,
		Kind:      s.Kind,
		Scope:     s.Scope,
		Snippet:   s.Snippet,
		Score:     score,
	}
	if f, ok := fileMap[s.File]; ok {
		hit.IndexedAt = f.IndexedAt
	}
	return hit
}

// cosine returns the cosine similarity between two vectors given the
// pre-computed norm of a. Returns 0 when either norm is zero.
func cosine(a, b []float32, aNorm float64) float64 {
	bNorm := vecNorm(b)
	if aNorm == 0 || bNorm == 0 {
		return 0
	}
	var dot float64
	minLen := len(a)
	if len(b) < minLen {
		minLen = len(b)
	}
	for i := 0; i < minLen; i++ {
		dot += float64(a[i]) * float64(b[i])
	}
	return dot / (aNorm * bNorm)
}

// vecNorm returns the Euclidean norm of v.
func vecNorm(v []float32) float64 {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	return math.Sqrt(sum)
}

// sortRanked sorts a slice of rankedSymbol in descending score order.
func sortRanked(r []rankedSymbol) {
	for i := 1; i < len(r); i++ {
		key := r[i]
		j := i - 1
		for j >= 0 && r[j].score < key.score {
			r[j+1] = r[j]
			j--
		}
		r[j+1] = key
	}
}
