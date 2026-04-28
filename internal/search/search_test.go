package search

import (
	"context"
	"testing"
	"time"

	"github.com/devchan97/code-map/internal/core"
	"github.com/devchan97/code-map/internal/lexical"
	"github.com/devchan97/code-map/internal/store"
)

// helper: build a small index in a temp store.
func setupIndex(t *testing.T) *store.Store {
	t.Helper()
	root := t.TempDir()
	st, err := store.Open(root)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	ctx := context.Background()
	now := time.Now().UTC()

	err = st.WithTx(ctx, func(tx store.Tx) error {
		if err := tx.UpsertFile(core.File{Path: "a.py", SHA1: "h", IndexedAt: now, Language: "python", SizeBytes: 1}); err != nil {
			return err
		}
		syms := []core.Symbol{
			{Name: "applyRateLimit", Qualname: "a.applyRateLimit", Kind: core.SymbolFunction, Scope: core.ScopeGlobal,
				File: "a.py", LineStart: 1, LineEnd: 5, Snippet: "def applyRateLimit"},
			{Name: "doOther", Qualname: "a.doOther", Kind: core.SymbolFunction, Scope: core.ScopeGlobal,
				File: "a.py", LineStart: 10, LineEnd: 12, Snippet: "def doOther"},
			{Name: "MyClass", Qualname: "a.MyClass", Kind: core.SymbolClass, Scope: core.ScopeGlobal,
				File: "a.py", LineStart: 20, LineEnd: 25, Snippet: "class MyClass"},
		}
		ids, err := tx.InsertSymbols(syms)
		if err != nil {
			return err
		}
		for i, s := range syms {
			toks := lexical.Tokenize(s)
			if err := tx.InsertTokens(ids[i], toks); err != nil {
				return err
			}
		}
		return tx.WriteMeta(core.Meta{
			SchemaVer: store.SchemaVer, RepoRoot: root, IndexedAt: now,
			Embedder: "lexical", SymbolCount: len(syms), FileCount: 1,
		})
	})
	if err != nil {
		t.Fatalf("setup tx: %v", err)
	}
	return st
}

func TestSearchRun_Basic(t *testing.T) {
	st := setupIndex(t)
	hits, err := Run(context.Background(), st, nil, Query{Text: "applyRateLimit"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("no hits")
	}
	if hits[0].Qualname != "a.applyRateLimit" {
		t.Errorf("top hit = %s; want a.applyRateLimit", hits[0].Qualname)
	}
	if hits[0].IndexedAt.IsZero() {
		t.Error("IndexedAt should be populated")
	}
}

func TestSearchRun_KindFilter(t *testing.T) {
	st := setupIndex(t)
	hits, err := Run(context.Background(), st, nil, Query{
		Text:  "applyRateLimit MyClass doOther",
		Kinds: []core.SymbolKind{core.SymbolClass},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, h := range hits {
		if h.Kind != core.SymbolClass {
			t.Errorf("filter violated: got %s", h.Kind)
		}
	}
}

func TestSearchRun_TopNDefault(t *testing.T) {
	st := setupIndex(t)
	hits, err := Run(context.Background(), st, nil, Query{Text: "doOther applyRateLimit MyClass", TopN: 0})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(hits) > 10 {
		t.Errorf("expected default TopN=10; got %d", len(hits))
	}
}

func TestSearchRun_RerankWithStubEncoder(t *testing.T) {
	st := setupIndex(t)
	// stubEncoder returns deterministic vectors; test that rerank path runs without panic.
	enc := &stubEncoder{}
	hits, err := Run(context.Background(), st, enc, Query{Text: "applyRateLimit", Rerank: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("no hits")
	}
}

// TestSearchRun_RerankNilEncoderFallsBackToBM25 verifies that when Rerank=true
// but enc is nil (e.g. stub build), Run does not return an error and still
// returns BM25 results. The warning is emitted via slog (not testable here
// without a custom handler, but the important thing is no error + results returned).
func TestSearchRun_RerankNilEncoderFallsBackToBM25(t *testing.T) {
	st := setupIndex(t)
	// Pass Rerank: true with a nil encoder — simulates the stub-build path where
	// encoder.Default() returns (nil, ErrUnsupported) and the CLI passes nil.
	hits, err := Run(context.Background(), st, nil, Query{Text: "applyRateLimit", Rerank: true})
	if err != nil {
		t.Fatalf("Run with nil encoder and Rerank=true must not error; got: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("expected BM25 fallback results; got none")
	}
	// Should still produce relevant results (top hit should match the query).
	if hits[0].Qualname != "a.applyRateLimit" {
		t.Errorf("BM25 fallback top hit = %s; want a.applyRateLimit", hits[0].Qualname)
	}
}

// TestSearchRun_RerankCosineReorders verifies that a fake encoder whose vectors
// express a clear preference reorders the BM25 candidate list. We set up a
// situation where BM25 would rank "applyRateLimit" first, but the fake encoder
// gives "MyClass" a cosine score of 1.0 and everything else 0.0 — so the
// reranked top hit should be "MyClass".
func TestSearchRun_RerankCosineReorders(t *testing.T) {
	st := setupIndex(t)
	// preferClassEncoder always returns a unit vector along dim-0 for the
	// query, but returns a unit vector along dim-0 only for snippets that
	// contain "class" — yielding cosine=1 for MyClass and cosine=0 otherwise.
	enc := &preferClassEncoder{}
	hits, err := Run(context.Background(), st, enc, Query{
		Text:   "applyRateLimit MyClass doOther",
		TopN:   3,
		Rerank: true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("no hits")
	}
	if hits[0].Qualname != "a.MyClass" {
		t.Errorf("reranked top hit = %s; want a.MyClass (encoder prefers class snippets)", hits[0].Qualname)
	}
}

// preferClassEncoder is a test double that assigns a unit vector along dim-0
// to the query and to any snippet containing "class", and the zero vector to
// all others — producing cosine=1 for class snippets and cosine=0 elsewhere.
type preferClassEncoder struct{}

func (e *preferClassEncoder) Name() string { return "prefer-class-test" }
func (e *preferClassEncoder) EncodeQuery(q string) ([]float32, error) {
	return []float32{1, 0}, nil // unit vector along dim-0
}
func (e *preferClassEncoder) EncodeBatch(snips []string) ([][]float32, error) {
	out := make([][]float32, len(snips))
	for i, s := range snips {
		// Snippets for MyClass contain "class"; give them cosine=1 with query.
		if len(s) > 0 && containsClass(s) {
			out[i] = []float32{1, 0} // cosine(query, this) = 1.0
		} else {
			out[i] = []float32{0, 1} // cosine(query, this) = 0.0
		}
	}
	return out, nil
}

func containsClass(s string) bool {
	for i := 0; i+4 < len(s); i++ {
		if s[i] == 'c' && s[i+1] == 'l' && s[i+2] == 'a' && s[i+3] == 's' && s[i+4] == 's' {
			return true
		}
	}
	return false
}

func TestShow_ByQualname(t *testing.T) {
	st := setupIndex(t)
	sym, err := Show(context.Background(), st, "a.applyRateLimit")
	if err != nil {
		t.Fatalf("Show: %v", err)
	}
	if sym.Qualname != "a.applyRateLimit" {
		t.Errorf("got %s", sym.Qualname)
	}
}

func TestShow_ByFileLine(t *testing.T) {
	st := setupIndex(t)
	sym, err := Show(context.Background(), st, "a.py:3")
	if err != nil {
		t.Fatalf("Show: %v", err)
	}
	if sym.Qualname != "a.applyRateLimit" {
		t.Errorf("got %s; want a.applyRateLimit (line 3 is within 1-5)", sym.Qualname)
	}
}

func TestShow_NotFound(t *testing.T) {
	st := setupIndex(t)
	if _, err := Show(context.Background(), st, "no.such.symbol"); err == nil {
		t.Error("expected error for unknown qualname")
	}
}

// stubEncoder returns fixed-length all-zero vectors.
type stubEncoder struct{}

func (s *stubEncoder) Name() string { return "stub" }
func (s *stubEncoder) EncodeQuery(q string) ([]float32, error) {
	return []float32{1, 0, 0, 0}, nil
}
func (s *stubEncoder) EncodeBatch(snips []string) ([][]float32, error) {
	out := make([][]float32, len(snips))
	for i := range snips {
		out[i] = []float32{1, 0, 0, 0}
	}
	return out, nil
}
