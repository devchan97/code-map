// Package visualize renders a static graph.html for a codemap index.
package visualize

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"path/filepath"
	"strings"
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
// Locations, Snippet, and Outgoing are non-vis fields read by the aside
// panel script when a node is selected; vis-network ignores unknown keys.
type graphNode struct {
	ID        string         `json:"id"`
	Label     string         `json:"label"`
	Group     string         `json:"group"`
	Title     string         `json:"title"`
	Locations []nodeLocation `json:"locations"`
	Snippet   string         `json:"snippet"`
	// Outgoing lists every edge whose FromQualname matches this node's id,
	// regardless of whether the target was resolved to an in-graph node.
	// Lets the aside surface external references (stdlib calls, unresolved
	// callees) that the canvas necessarily filters out.
	Outgoing []nodeOutgoing `json:"outgoing"`
}

// nodeOutgoing is one outgoing edge from a node, kept whether or not the
// target qualname resolves to a known symbol in this index.
type nodeOutgoing struct {
	To       string `json:"to"`
	Kind     string `json:"kind"`
	Resolved bool   `json:"resolved"`
}

// nodeLocation is one occurrence of a (possibly merged) qualname.
type nodeLocation struct {
	File      string `json:"file"`
	LineStart int    `json:"line_start"`
	LineEnd   int    `json:"line_end"`
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

	// 5a. Group every edge by from-qualname so the aside can show
	// outgoing references (resolved or not) for any node, even when the
	// canvas filters out unresolved targets.
	outByFrom := make(map[string][]nodeOutgoing, len(allEdges))
	for _, e := range allEdges {
		outByFrom[e.FromQualname] = append(outByFrom[e.FromQualname], nodeOutgoing{
			To: e.ToQualname, Kind: string(e.Kind), Resolved: e.Resolved,
		})
	}

	// 5. Build nodes; index qualnames for endpoint filtering.
	//
	// vis-network's DataSet rejects duplicate ids with
	//   "Cannot add item: item with id <x> already exists"
	// and stops loading the remaining nodes — leaving the canvas blank.
	// Edges are keyed by qualname (see core.Edge), so the node id must also
	// be the qualname; when two symbols share a qualname (common when a
	// parser is in scaffold state and emits one file-level symbol per file
	// using the basename) we collapse them into a single node and append
	// the additional locations to the tooltip rather than producing a
	// duplicate id. Once parsers emit fully-qualified names (pkg.Func),
	// collisions disappear naturally.
	nodeSet := make(map[string]int, len(symbols))
	nodes := make([]graphNode, 0, len(symbols))
	for _, s := range symbols {
		loc := nodeLocation{File: s.File, LineStart: s.LineStart, LineEnd: s.LineEnd}
		locStr := fmt.Sprintf("%s:%d-%d", s.File, s.LineStart, s.LineEnd)
		if idx, dup := nodeSet[s.Qualname]; dup {
			nodes[idx].Title += "\n" + locStr
			nodes[idx].Locations = append(nodes[idx].Locations, loc)
			continue
		}
		nodeSet[s.Qualname] = len(nodes)
		nodes = append(nodes, graphNode{
			ID:        s.Qualname,
			Label:     s.Name,
			Group:     string(s.Kind),
			Title:     locStr,
			Locations: []nodeLocation{loc},
			Snippet:   s.Snippet,
			Outgoing:  outByFrom[s.Qualname],
		})
	}

	// 6. Build edges.
	//
	// Policy: an edge whose `from` is in the node set is always rendered.
	// If the `to` endpoint is not in the node set (the common case for
	// language stdlib calls and parser-unresolved targets) we synthesize
	// a placeholder external node so the connection is still visible.
	// This keeps the canvas honest about real call relationships even
	// when the parser/resolver cannot map a target back to a known
	// internal symbol; the dashed style flags the link as unresolved.
	//
	// Edges with `from` outside the node set are dropped — without an
	// origin to anchor on they would only add noise.
	edges := make([]graphEdge, 0, len(allEdges))
	externals := make(map[string]int)
	for _, e := range allEdges {
		if _, ok := nodeSet[e.FromQualname]; !ok {
			continue
		}
		if _, ok := nodeSet[e.ToQualname]; !ok {
			if _, seen := externals[e.ToQualname]; !seen {
				externals[e.ToQualname] = len(nodes)
				label := e.ToQualname
				if i := strings.LastIndex(label, "."); i >= 0 {
					label = label[i+1:]
				}
				nodes = append(nodes, graphNode{
					ID:    e.ToQualname,
					Label: label,
					Group: "external",
					Title: e.ToQualname + "\n(external — not in this index)",
				})
				nodeSet[e.ToQualname] = externals[e.ToQualname]
			}
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
