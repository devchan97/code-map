// Package lexical provides tokenization, BM25 scoring, and the TokenStore
// interface for codemap's lexical retrieval layer.
package lexical

import "context"

// TokenStore is the persistence-layer contract used by lexical search.
//
// This interface is a pure contract — no implementation is provided in this
// package. The store package satisfies the interface through its Tx type,
// which operates on top of SQLite. The separation prevents import cycles:
// lexical imports only core; store imports lexical for the Token type.
//
// Method semantics:
//
//   - InsertTokens persists the given tokens, associating them with symbolID.
//   - DeleteTokensForFile removes all token rows whose parent symbol belongs
//     to the given repo-relative file path.
//   - SearchBM25 executes a BM25 ranked query and returns the top topN
//     candidate symbol IDs with their scores.
//   - Stats returns the corpus size n (number of distinct symbol documents)
//     and the average document length avgLen (sum of token weights per symbol,
//     averaged over all symbols). Both values are needed by Score.
type TokenStore interface {
	// InsertTokens stores the tokens for a single symbol.
	InsertTokens(symbolID int64, toks []Token) error

	// DeleteTokensForFile removes all token rows for symbols in the given file.
	DeleteTokensForFile(file string) error

	// SearchBM25 returns the top topN scoring symbols for the query string,
	// applying the structural filters f. ctx should be respected for
	// cancellation.
	SearchBM25(ctx context.Context, query string, topN int, f Filters) ([]ScoredID, error)

	// Stats returns the number of indexed symbol documents and the average
	// document length (sum of token weights) across the corpus.
	Stats(ctx context.Context) (n int, avgLen float64, err error)
}
