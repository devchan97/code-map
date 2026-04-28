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
