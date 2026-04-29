package visualize

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/devchan97/code-map/internal/core"
)

// fakeDS is an in-memory DataSource for testing.
type fakeDS struct {
	syms []core.Symbol
	eds  []core.Edge
	meta core.Meta
}

func (f *fakeDS) AllSymbols(ctx context.Context, fileGlob string, kinds []core.SymbolKind) ([]core.Symbol, error) {
	return f.syms, nil
}
func (f *fakeDS) AllEdges(ctx context.Context) ([]core.Edge, error) { return f.eds, nil }
func (f *fakeDS) ReadMeta() (core.Meta, error)                      { return f.meta, nil }

func TestRender_WritesHTMLWithMeta(t *testing.T) {
	repo := t.TempDir()
	ds := &fakeDS{
		syms: []core.Symbol{
			{Name: "foo", Qualname: "a.foo", Kind: core.SymbolFunction, Scope: core.ScopeGlobal,
				File: "a.py", LineStart: 1, LineEnd: 5, Snippet: "def foo()"},
			{Name: "Bar", Qualname: "a.Bar", Kind: core.SymbolClass, Scope: core.ScopeGlobal,
				File: "a.py", LineStart: 10, LineEnd: 20, Snippet: "class Bar"},
		},
		eds: []core.Edge{
			{FromQualname: "a.foo", ToQualname: "a.Bar", Kind: core.EdgeReference, Resolved: true},
		},
		meta: core.Meta{
			SchemaVer: 1, RepoRoot: repo, IndexedAt: time.Date(2026, 4, 28, 5, 30, 0, 0, time.UTC),
			Embedder: "lexical", FileCount: 1, SymbolCount: 2,
		},
	}
	outPath, err := Render(context.Background(), ds, repo, Options{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.HasSuffix(outPath, "/graph.html") {
		t.Errorf("outPath = %q; want forward-slash suffix /graph.html", outPath)
	}
	if strings.Contains(outPath, "\\") {
		t.Errorf("outPath contains backslash: %q", outPath)
	}

	body, err := os.ReadFile(filepath.FromSlash(outPath))
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	html := string(body)

	checks := []string{
		"<!DOCTYPE html",
		"vis-network",
		"a.foo",
		"a.Bar",
		"2026-04-28",
		filepath.ToSlash(repo),
		`>2<`,
		`>1<`,
	}
	for _, c := range checks {
		if !strings.Contains(html, c) {
			t.Errorf("HTML missing fragment %q", c)
		}
	}
}

// TestRender_DedupesNodesByQualname guards against a vis-network DataSet error
// ("Cannot add item: item with id <x> already exists") that aborts rendering
// when two symbols share a qualname — happens whenever a parser emits one
// file-level symbol per file using the basename (e.g. multiple `index.go`
// files in different packages). The render must produce one node per qualname
// and append the extra locations to the tooltip so the graph still loads.
func TestRender_DedupesNodesByQualname(t *testing.T) {
	repo := t.TempDir()
	ds := &fakeDS{
		syms: []core.Symbol{
			{Name: "index.go", Qualname: "index.go", Kind: core.SymbolVariable, Scope: core.ScopeGlobal,
				File: "internal/cli/index.go", LineStart: 1, LineEnd: 10},
			{Name: "index.go", Qualname: "index.go", Kind: core.SymbolVariable, Scope: core.ScopeGlobal,
				File: "internal/lexical/index.go", LineStart: 1, LineEnd: 39},
			{Name: "index.go", Qualname: "index.go", Kind: core.SymbolVariable, Scope: core.ScopeGlobal,
				File: "internal/store/index.go", LineStart: 1, LineEnd: 25},
		},
		meta: core.Meta{SchemaVer: 1, IndexedAt: time.Now().UTC()},
	}
	outPath, err := Render(context.Background(), ds, repo, Options{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	body, err := os.ReadFile(filepath.FromSlash(outPath))
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	html := string(body)

	// Exactly one node with id "index.go" must appear in the embedded JSON.
	if got := strings.Count(html, `"id":"index.go"`); got != 1 {
		t.Errorf("got %d nodes with id=index.go; want 1 (rest must be merged)", got)
	}
	// All three locations must show up in the tooltip text.
	for _, loc := range []string{
		"internal/cli/index.go:1-10",
		"internal/lexical/index.go:1-39",
		"internal/store/index.go:1-25",
	} {
		if !strings.Contains(html, loc) {
			t.Errorf("HTML missing location %q in tooltip", loc)
		}
	}
}

func TestRender_CustomOutPath(t *testing.T) {
	repo := t.TempDir()
	custom := filepath.Join(t.TempDir(), "custom.html")
	ds := &fakeDS{meta: core.Meta{SchemaVer: 1, IndexedAt: time.Now().UTC()}}
	outPath, err := Render(context.Background(), ds, repo, Options{OutPath: custom})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if filepath.FromSlash(outPath) != custom {
		t.Errorf("outPath = %q; want %q", outPath, custom)
	}
	if _, err := os.Stat(custom); err != nil {
		t.Errorf("custom path not written: %v", err)
	}
}
