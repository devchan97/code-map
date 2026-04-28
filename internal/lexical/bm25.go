// Package lexical provides tokenization, BM25 scoring, and the TokenStore
// interface for codemap's lexical retrieval layer.
package lexical

import (
	"math"

	"github.com/devchan97/code-map/internal/core"
)

// Filters narrows BM25 candidates by structural properties.
type Filters struct {
	// Kinds restricts results to the listed symbol kinds; empty means no restriction.
	Kinds []core.SymbolKind
	// Scopes restricts results to the listed scopes; empty means no restriction.
	Scopes []core.Scope
	// FileGlob is a forward-slash doublestar glob pattern; empty means no restriction.
	FileGlob string
}

// ScoredID is a search candidate produced by the BM25 layer.
type ScoredID struct {
	// SymbolID is the database primary key of the matching symbol.
	SymbolID int64
	// Score is the BM25 relevance score summed across all query terms.
	Score float64
}

// BM25 parameters (Robertson-Sparck-Jones defaults).
const (
	// BM25K1 is the term-frequency saturation parameter.
	BM25K1 = 1.5
	// BM25B is the document-length normalisation parameter.
	BM25B = 0.75
)

// Score computes a single term contribution under BM25.
//
// Parameters:
//
//	tf     = term frequency in the document
//	df     = number of documents containing the term
//	docLen = current document length (sum of token weights)
//	avgLen = average document length over the corpus
//	n      = corpus size (total number of documents)
//	k1, b  = BM25 hyperparameters
//
// The smoothed IDF form is:
//
//	idf = ln( (n - df + 0.5) / (df + 0.5) + 1.0 )
//
// TF normalisation:
//
//	norm_tf = (tf * (k1 + 1)) / (tf + k1 * (1 - b + b * docLen/avgLen))
//
// Returns idf * norm_tf.
func Score(tf, df int, docLen, avgLen float64, n int, k1, b float64) float64 {
	if n <= 0 || avgLen <= 0 {
		return 0
	}
	idf := math.Log((float64(n)-float64(df)+0.5)/(float64(df)+0.5) + 1.0)
	normTF := (float64(tf) * (k1 + 1.0)) / (float64(tf) + k1*(1.0-b+b*docLen/avgLen))
	return idf * normTF
}
