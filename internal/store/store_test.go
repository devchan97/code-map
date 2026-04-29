package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/devchan97/code-map/internal/core"
	"github.com/devchan97/code-map/internal/lexical"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	root := t.TempDir()
	st, err := Open(root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestOpen_FreshAndReopen(t *testing.T) {
	root := t.TempDir()
	st1, err := Open(root)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	if err := st1.Close(); err != nil {
		t.Fatal(err)
	}
	// Reopen — should succeed without applying schema again.
	st2, err := Open(root)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer st2.Close()
	if st2.RepoRoot() == "" {
		t.Error("RepoRoot should be populated")
	}
}

func TestStore_FilesAndMeta(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	err := st.WithTx(ctx, func(tx Tx) error {
		if err := tx.UpsertFile(core.File{
			Path: "main.py", SHA1: "abc", IndexedAt: now, Language: "python", SizeBytes: 100,
		}); err != nil {
			return err
		}
		if err := tx.WriteMeta(core.Meta{
			SchemaVer: SchemaVer, RepoRoot: "/r", IndexedAt: now,
			Embedder: "lexical", SymbolCount: 0, FileCount: 1,
		}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatalf("tx1: %v", err)
	}

	err = st.WithTx(ctx, func(tx Tx) error {
		files, err := tx.ListFiles()
		if err != nil {
			return err
		}
		if len(files) != 1 || files[0].Path != "main.py" || files[0].SHA1 != "abc" {
			t.Errorf("files = %v", files)
		}
		meta, err := tx.ReadMeta()
		if err != nil {
			return err
		}
		if meta.SchemaVer != SchemaVer || meta.Embedder != "lexical" || meta.FileCount != 1 {
			t.Errorf("meta = %+v", meta)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("tx2: %v", err)
	}
}

func TestStore_SymbolsCascadeDelete(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	var ids []int64
	err := st.WithTx(ctx, func(tx Tx) error {
		if err := tx.UpsertFile(core.File{
			Path: "a.py", SHA1: "h", IndexedAt: now, Language: "python", SizeBytes: 1,
		}); err != nil {
			return err
		}
		got, err := tx.InsertSymbols([]core.Symbol{
			{Name: "foo", Qualname: "a.foo", Kind: core.SymbolFunction, Scope: core.ScopeGlobal,
				File: "a.py", LineStart: 1, LineEnd: 5, Snippet: "def foo(): pass"},
			{Name: "bar", Qualname: "a.bar", Kind: core.SymbolFunction, Scope: core.ScopeGlobal,
				File: "a.py", LineStart: 10, LineEnd: 12, Snippet: "def bar(): pass"},
		})
		if err != nil {
			return err
		}
		ids = got
		if err := tx.InsertEdges([]core.Edge{
			{FromQualname: "a.foo", ToQualname: "a.bar", Kind: core.EdgeCall, Resolved: false},
		}, "a.py"); err != nil {
			return err
		}
		if err := tx.InsertTokens(ids[0], []lexical.Token{{Text: "foo", Field: "name", Weight: 4}}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Delete artifacts for a.py.
	err = st.WithTx(ctx, func(tx Tx) error {
		return tx.DeleteFileArtifacts("a.py")
	})
	if err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Verify cascade: symbols, edges, tokens all gone.
	err = st.WithTx(ctx, func(tx Tx) error {
		syms, err := tx.SymbolsByFile("a.py")
		if err != nil {
			return err
		}
		if len(syms) != 0 {
			t.Errorf("symbols not cleaned: %v", syms)
		}
		ed, err := tx.EdgesFrom("a.foo")
		if err != nil {
			return err
		}
		if len(ed) != 0 {
			t.Errorf("edges not cleaned: %v", ed)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestStore_HydrateSymbols_PreservesOrder(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	var ids []int64

	err := st.WithTx(ctx, func(tx Tx) error {
		if err := tx.UpsertFile(core.File{
			Path: "x.py", SHA1: "1", IndexedAt: now, Language: "python", SizeBytes: 1,
		}); err != nil {
			return err
		}
		var err error
		ids, err = tx.InsertSymbols([]core.Symbol{
			{Name: "a", Qualname: "x.a", Kind: core.SymbolFunction, Scope: core.ScopeGlobal, File: "x.py", LineStart: 1, LineEnd: 2, Snippet: "a"},
			{Name: "b", Qualname: "x.b", Kind: core.SymbolFunction, Scope: core.ScopeGlobal, File: "x.py", LineStart: 3, LineEnd: 4, Snippet: "b"},
			{Name: "c", Qualname: "x.c", Kind: core.SymbolFunction, Scope: core.ScopeGlobal, File: "x.py", LineStart: 5, LineEnd: 6, Snippet: "c"},
		})
		return err
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Hydrate in reverse order.
	reversed := []int64{ids[2], ids[0], ids[1]}
	err = st.WithTx(ctx, func(tx Tx) error {
		got, err := tx.HydrateSymbols(reversed)
		if err != nil {
			return err
		}
		if len(got) != 3 {
			t.Fatalf("got %d", len(got))
		}
		if got[0].Name != "c" || got[1].Name != "a" || got[2].Name != "b" {
			t.Errorf("order = %s,%s,%s; want c,a,b", got[0].Name, got[1].Name, got[2].Name)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestStore_ResolveEdges(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	err := st.WithTx(ctx, func(tx Tx) error {
		if err := tx.UpsertFile(core.File{Path: "a.py", SHA1: "h", IndexedAt: now, Language: "python", SizeBytes: 1}); err != nil {
			return err
		}
		if _, err := tx.InsertSymbols([]core.Symbol{
			{Name: "bar", Qualname: "a.bar", Kind: core.SymbolFunction, Scope: core.ScopeGlobal, File: "a.py", LineStart: 1, LineEnd: 2, Snippet: ""},
		}); err != nil {
			return err
		}
		if err := tx.InsertEdges([]core.Edge{
			{FromQualname: "a.foo", ToQualname: "a.bar", Kind: core.EdgeCall, Resolved: false},
			{FromQualname: "a.foo", ToQualname: "external.thing", Kind: core.EdgeCall, Resolved: false},
		}, "a.py"); err != nil {
			return err
		}
		return tx.ResolveEdges()
	})
	if err != nil {
		t.Fatal(err)
	}

	err = st.WithTx(ctx, func(tx Tx) error {
		edges, err := tx.EdgesFrom("a.foo")
		if err != nil {
			return err
		}
		var resolved, unresolved int
		for _, e := range edges {
			if e.Resolved {
				resolved++
			} else {
				unresolved++
			}
		}
		if resolved != 1 || unresolved != 1 {
			t.Errorf("expected 1 resolved + 1 unresolved; got resolved=%d unresolved=%d", resolved, unresolved)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestStore_ResolveEdges_ShortRange covers §15 Q3: an edge whose
// to_qualname is a bare callee name (e.g. `_cleanup` from a Python
// parser that didn't track the same-module prefix) should be rewritten
// to `<module>._cleanup` and marked resolved when that qualname exists
// in the symbols table.
//
// External calls like `argparse.ArgumentParser` (which already contain
// a dot) must NOT be rewritten — the short-range pass only fires on
// dotless to_qualnames so dotted external chains stay unresolved.
func TestStore_ResolveEdges_ShortRange(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	err := st.WithTx(ctx, func(tx Tx) error {
		if err := tx.UpsertFile(core.File{Path: "main.py", SHA1: "h", IndexedAt: now, Language: "python", SizeBytes: 1}); err != nil {
			return err
		}
		if _, err := tx.InsertSymbols([]core.Symbol{
			{Name: "main", Qualname: "main.main", Kind: core.SymbolFunction, Scope: core.ScopeGlobal, File: "main.py", LineStart: 1, LineEnd: 5},
			{Name: "_cleanup", Qualname: "main._cleanup", Kind: core.SymbolFunction, Scope: core.ScopeGlobal, File: "main.py", LineStart: 10, LineEnd: 12},
		}); err != nil {
			return err
		}
		return tx.InsertEdges([]core.Edge{
			// bare same-module call — should resolve to main._cleanup
			{FromQualname: "main.main", ToQualname: "_cleanup", Kind: core.EdgeCall, Resolved: false},
			// dotted external — must stay unresolved
			{FromQualname: "main.main", ToQualname: "argparse.ArgumentParser", Kind: core.EdgeCall, Resolved: false},
			// already-resolved exact match — unchanged
			{FromQualname: "main.main", ToQualname: "main._cleanup", Kind: core.EdgeReference, Resolved: false},
		}, "main.py")
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := st.WithTx(ctx, func(tx Tx) error { return tx.ResolveEdges() }); err != nil {
		t.Fatal(err)
	}

	err = st.WithTx(ctx, func(tx Tx) error {
		edges, err := tx.EdgesFrom("main.main")
		if err != nil {
			return err
		}
		var resolvedCleanup, resolvedRef bool
		var argparseStillUnresolved bool
		for _, e := range edges {
			switch {
			case e.ToQualname == "main._cleanup" && e.Resolved && e.Kind == core.EdgeCall:
				resolvedCleanup = true
			case e.ToQualname == "main._cleanup" && e.Resolved && e.Kind == core.EdgeReference:
				resolvedRef = true
			case e.ToQualname == "argparse.ArgumentParser" && !e.Resolved:
				argparseStillUnresolved = true
			}
		}
		if !resolvedCleanup {
			t.Errorf("bare-name edge to `_cleanup` was not rewritten to `main._cleanup`/resolved")
		}
		if !resolvedRef {
			t.Errorf("exact-match edge to `main._cleanup` was not resolved")
		}
		if !argparseStillUnresolved {
			t.Errorf("dotted external `argparse.ArgumentParser` should stay unresolved")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestStore_SearchBM25_Basic(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	err := st.WithTx(ctx, func(tx Tx) error {
		if err := tx.UpsertFile(core.File{Path: "a.py", SHA1: "h", IndexedAt: now, Language: "python", SizeBytes: 1}); err != nil {
			return err
		}
		ids, err := tx.InsertSymbols([]core.Symbol{
			{Name: "applyRateLimit", Qualname: "a.applyRateLimit", Kind: core.SymbolFunction, Scope: core.ScopeGlobal, File: "a.py", LineStart: 1, LineEnd: 5, Snippet: "def applyRateLimit"},
			{Name: "doOther", Qualname: "a.doOther", Kind: core.SymbolFunction, Scope: core.ScopeGlobal, File: "a.py", LineStart: 10, LineEnd: 12, Snippet: "def doOther"},
		})
		if err != nil {
			return err
		}
		for i, s := range []core.Symbol{
			{Name: "applyRateLimit", Qualname: "a.applyRateLimit"},
			{Name: "doOther", Qualname: "a.doOther"},
		} {
			toks := lexical.Tokenize(s)
			if err := tx.InsertTokens(ids[i], toks); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	err = st.WithTx(ctx, func(tx Tx) error {
		results, err := tx.SearchBM25(ctx, "applyRateLimit", 10, lexical.Filters{})
		if err != nil {
			return err
		}
		if len(results) == 0 {
			t.Fatalf("no results")
		}
		// Top hit should match the qualname's symbol.
		top, err := tx.HydrateSymbols([]int64{results[0].SymbolID})
		if err != nil {
			return err
		}
		if top[0].Name != "applyRateLimit" {
			t.Errorf("top = %s; want applyRateLimit", top[0].Name)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestStore_Meta_RoundTripsIndexerVer guards that a write/read cycle on the
// meta table preserves both schema_ver and indexer_ver. Without this, a
// silently-dropped indexer_ver would defeat the staleness warning that
// `status` relies on after a parser/tokenizer change.
func TestStore_Meta_RoundTripsIndexerVer(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	want := core.Meta{
		SchemaVer: SchemaVer, IndexerVer: IndexerVer,
		RepoRoot: "/r", IndexedAt: now, Embedder: "lexical",
		FileCount: 3, SymbolCount: 9,
	}
	if err := st.WithTx(ctx, func(tx Tx) error { return tx.WriteMeta(want) }); err != nil {
		t.Fatal(err)
	}
	var got core.Meta
	if err := st.WithTx(ctx, func(tx Tx) error {
		var err error
		got, err = tx.ReadMeta()
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if got.IndexerVer != want.IndexerVer {
		t.Errorf("IndexerVer round-trip = %d; want %d", got.IndexerVer, want.IndexerVer)
	}
	if got.SchemaVer != want.SchemaVer {
		t.Errorf("SchemaVer round-trip = %d; want %d", got.SchemaVer, want.SchemaVer)
	}
}

// TestStore_SearchBM25_PrefixExpansion guards Issue #2: a query like "parse"
// must match symbols whose tokens begin with "parse" (e.g. "parser",
// "parsing"), since BM25 over IN(...) is exact-match and tokens aren't
// stemmed at index time.
func TestStore_SearchBM25_PrefixExpansion(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	err := st.WithTx(ctx, func(tx Tx) error {
		if err := tx.UpsertFile(core.File{Path: "a.py", SHA1: "h", IndexedAt: now, Language: "python", SizeBytes: 1}); err != nil {
			return err
		}
		ids, err := tx.InsertSymbols([]core.Symbol{
			{Name: "parser", Qualname: "a.parser", Kind: core.SymbolFunction, Scope: core.ScopeGlobal, File: "a.py", LineStart: 1, LineEnd: 2, Snippet: "def parser"},
			{Name: "unrelated", Qualname: "a.unrelated", Kind: core.SymbolFunction, Scope: core.ScopeGlobal, File: "a.py", LineStart: 3, LineEnd: 4, Snippet: "def unrelated"},
		})
		if err != nil {
			return err
		}
		for i, s := range []core.Symbol{
			{Name: "parser", Qualname: "a.parser"},
			{Name: "unrelated", Qualname: "a.unrelated"},
		} {
			if err := tx.InsertTokens(ids[i], lexical.Tokenize(s)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Query "parse" must hit "parser" via prefix expansion.
	err = st.WithTx(ctx, func(tx Tx) error {
		results, err := tx.SearchBM25(ctx, "parse", 10, lexical.Filters{})
		if err != nil {
			return err
		}
		if len(results) == 0 {
			t.Fatalf("query \"parse\" returned no results; expected to hit \"parser\" via prefix expansion")
		}
		hyd, _ := tx.HydrateSymbols([]int64{results[0].SymbolID})
		if hyd[0].Name != "parser" {
			t.Errorf("top hit = %q; want parser", hyd[0].Name)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestStore_SchemaMismatch(t *testing.T) {
	root := t.TempDir()
	st, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}

	// Manually corrupt schema_ver via direct exec.
	if _, err := st.db.Exec(`UPDATE meta SET value = '999' WHERE key = 'schema_ver'`); err != nil {
		// schema_ver may not have been written yet; insert it.
		if _, err := st.db.Exec(`INSERT OR REPLACE INTO meta(key, value) VALUES ('schema_ver', '999')`); err != nil {
			t.Fatal(err)
		}
	}
	st.Close()

	_, err = Open(root)
	if !errors.Is(err, core.ErrSchemaMismatch) {
		t.Errorf("Open should return ErrSchemaMismatch; got %v", err)
	}
}
