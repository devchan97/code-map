package graph

import (
	"context"
	"testing"
	"time"

	"github.com/devchan97/code-map/internal/core"
	"github.com/devchan97/code-map/internal/store"
)

func setupGraphIndex(t *testing.T) *store.Store {
	t.Helper()
	root := t.TempDir()
	st, err := store.Open(root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	ctx := context.Background()
	now := time.Now().UTC()

	err = st.WithTx(ctx, func(tx store.Tx) error {
		if err := tx.UpsertFile(core.File{Path: "a.py", SHA1: "h", IndexedAt: now, Language: "python", SizeBytes: 1}); err != nil {
			return err
		}
		_, err := tx.InsertSymbols([]core.Symbol{
			{Name: "main", Qualname: "a.main", Kind: core.SymbolFunction, Scope: core.ScopeGlobal, File: "a.py", LineStart: 1, LineEnd: 5, Snippet: "main"},
			{Name: "helper", Qualname: "a.helper", Kind: core.SymbolFunction, Scope: core.ScopeGlobal, File: "a.py", LineStart: 10, LineEnd: 12, Snippet: "helper"},
		})
		if err != nil {
			return err
		}
		return tx.InsertEdges([]core.Edge{
			{FromQualname: "a.main", ToQualname: "a.helper", Kind: core.EdgeCall, Resolved: false},
			{FromQualname: "a.main", ToQualname: "external.unknown", Kind: core.EdgeCall, Resolved: false},
		}, "a.py")
	})
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestRefs_FindsCallers(t *testing.T) {
	st := setupGraphIndex(t)
	views, err := Refs(context.Background(), st, "a.helper")
	if err != nil {
		t.Fatalf("Refs: %v", err)
	}
	if len(views) != 1 {
		t.Fatalf("expected 1 incoming edge to a.helper; got %d", len(views))
	}
	if views[0].FromName != "a.main" || views[0].ToName != "a.helper" {
		t.Errorf("edge endpoints = %s → %s", views[0].FromName, views[0].ToName)
	}
	if views[0].Kind != core.EdgeCall {
		t.Errorf("kind = %s", views[0].Kind)
	}
	if views[0].From == nil {
		t.Errorf("From should be hydrated for resolved-counterpart")
	}
}

func TestCalls_FindsCallees(t *testing.T) {
	st := setupGraphIndex(t)
	views, err := Calls(context.Background(), st, "a.main")
	if err != nil {
		t.Fatalf("Calls: %v", err)
	}
	if len(views) != 2 {
		t.Fatalf("expected 2 outgoing edges from a.main; got %d", len(views))
	}
	var resolved, unresolved int
	for _, v := range views {
		if v.To != nil {
			resolved++
		} else {
			unresolved++
		}
	}
	if resolved != 1 || unresolved != 1 {
		t.Errorf("resolved=%d unresolved=%d; want 1+1", resolved, unresolved)
	}
}

func TestCalls_NoEdges(t *testing.T) {
	st := setupGraphIndex(t)
	views, err := Calls(context.Background(), st, "a.helper")
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 0 {
		t.Errorf("a.helper has no outgoing edges; got %v", views)
	}
}
