// Package visualize renders a static graph.html for a codemap index.
package visualize

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"path/filepath"
	"time"

	"github.com/devchan97/code-map/internal/core"
	"github.com/devchan97/code-map/internal/platform"
)

// Options controls graph.html rendering.
type Options struct {
	// OutPath is the destination file path. Defaults to <repoRoot>/.codemap/graph.html.
	OutPath string
	// Open, when true, opens the rendered file in the OS default browser.
	Open bool
	// FileGlob is an optional forward-slash doublestar glob to filter symbols by file.
	// An empty string means all files.
	FileGlob string
	// Kinds filters symbols by kind. An empty slice means all kinds.
	Kinds []core.SymbolKind
}

// DataSource is the read-only contract visualize needs from the index.
// It is implemented by store.Tx or a small adapter over Store.
// visualize intentionally does not import internal/store; the call site
// provides an adapter.
type DataSource interface {
	AllSymbols(ctx context.Context, fileGlob string, kinds []core.SymbolKind) ([]core.Symbol, error)
	AllEdges(ctx context.Context) ([]core.Edge, error)
	ReadMeta() (core.Meta, error)
}

// graphNode is the vis-network node shape injected into the template.
type graphNode struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Group string `json:"group"`
	Title string `json:"title"`
}

// graphEdge is the vis-network edge shape injected into the template.
type graphEdge struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Arrows string `json:"arrows"`
	Label  string `json:"label"`
	Dashes bool   `json:"dashes"`
}

// templateData holds the values passed to graph.html.tmpl.
type templateData struct {
	RepoName    string
	RepoPath    string
	LastIndexed string
	FileCount   int
	SymbolCount int
	EdgeCount   int
	// NodesJSON and EdgesJSON are pre-marshaled JSON; using template.JS
	// prevents HTML escaping of the JSON literals.
	NodesJSON template.JS
	EdgesJSON template.JS
}

// Render writes graph.html using ds as the data source and returns the
// absolute path of the written file. If opts.Open is true it also opens the
// file in the default OS browser.
func Render(ctx context.Context, ds DataSource, repoRoot string, opts Options) (string, error) {
	// 1. Determine output path.
	outPath := opts.OutPath
	if outPath == "" {
		outPath = platform.RepoCodemapDir(repoRoot) + "/graph.html"
	}
	// Make absolute so the returned value is always unambiguous.
	absPath, err := filepath.Abs(platform.FromSlash(outPath))
	if err != nil {
		return "", fmt.Errorf("visualize: resolve output path: %w", err)
	}

	// 2. Read meta.
	meta, err := ds.ReadMeta()
	if err != nil {
		return "", fmt.Errorf("visualize: read meta: %w", err)
	}

	// 3. Read symbols.
	symbols, err := ds.AllSymbols(ctx, opts.FileGlob, opts.Kinds)
	if err != nil {
		return "", fmt.Errorf("visualize: read symbols: %w", err)
	}

	// 4. Read edges.
	allEdges, err := ds.AllEdges(ctx)
	if err != nil {
		return "", fmt.Errorf("visualize: read edges: %w", err)
	}

	// 5. Build nodes; index qualnames for endpoint filtering.
	nodeSet := make(map[string]struct{}, len(symbols))
	nodes := make([]graphNode, 0, len(symbols))
	for _, s := range symbols {
		nodeSet[s.Qualname] = struct{}{}
		nodes = append(nodes, graphNode{
			ID:    s.Qualname,
			Label: s.Name,
			Group: string(s.Kind),
			Title: fmt.Sprintf("%s:%d-%d", s.File, s.LineStart, s.LineEnd),
		})
	}

	// 6. Build edges; skip any whose endpoints are not in the node set.
	edges := make([]graphEdge, 0, len(allEdges))
	for _, e := range allEdges {
		if _, ok := nodeSet[e.FromQualname]; !ok {
			continue
		}
		if _, ok := nodeSet[e.ToQualname]; !ok {
			continue
		}
		edges = append(edges, graphEdge{
			From:   e.FromQualname,
			To:     e.ToQualname,
			Arrows: "to",
			Label:  string(e.Kind),
			Dashes: !e.Resolved,
		})
	}

	// 7. Marshal nodes and edges to JSON strings.
	nodesBytes, err := json.Marshal(nodes)
	if err != nil {
		return "", fmt.Errorf("visualize: marshal nodes: %w", err)
	}
	edgesBytes, err := json.Marshal(edges)
	if err != nil {
		return "", fmt.Errorf("visualize: marshal edges: %w", err)
	}

	// 8. Execute template.
	repoName := filepath.Base(repoRoot)
	repoPathSlash := platform.ToSlash(repoRoot)

	tmpl, err := template.New("graph").Parse(graphTemplate)
	if err != nil {
		return "", fmt.Errorf("visualize: parse template: %w", err)
	}

	var buf bytes.Buffer
	data := templateData{
		RepoName:    repoName,
		RepoPath:    repoPathSlash,
		LastIndexed: meta.IndexedAt.Format(time.RFC3339),
		FileCount:   meta.FileCount,
		SymbolCount: len(nodes),
		EdgeCount:   len(edges),
		NodesJSON:   template.JS(nodesBytes),
		EdgesJSON:   template.JS(edgesBytes),
	}
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("visualize: execute template: %w", err)
	}

	// 9. Ensure parent directory exists and atomically write the HTML.
	absDir := filepath.Dir(absPath)
	if err := platform.EnsureDir(absDir); err != nil {
		return "", fmt.Errorf("visualize: ensure dir: %w", err)
	}
	if err := platform.AtomicWrite(absPath, buf.Bytes(), 0o644); err != nil {
		return "", fmt.Errorf("visualize: write html: %w", err)
	}

	// 10. Optionally open in browser.
	if opts.Open {
		if err := platform.OpenInBrowser(absPath); err != nil {
			return absPath, fmt.Errorf("visualize: open in browser: %w", err)
		}
	}

	return platform.ToSlash(absPath), nil
}
