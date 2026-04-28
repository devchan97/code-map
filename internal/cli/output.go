package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/devchan97/code-map/internal/core"
)

// WriteJSON marshals v to stdout as indented JSON.
func WriteJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("WriteJSON: %w", err)
	}
	return nil
}

// WriteHuman writes a human-readable representation of v to stdout.
// Supports core.Meta, core.SearchHit, core.Symbol, core.Edge,
// []core.RegistryEntry, and string. Pipeline/graph types are rendered
// inline in their respective command files.
func WriteHuman(v any) error {
	switch val := v.(type) {
	case core.Meta:
		fmt.Printf("repo_root:    %s\n", val.RepoRoot)
		fmt.Printf("schema_ver:   %d\n", val.SchemaVer)
		fmt.Printf("embedder:     %s\n", val.Embedder)
		fmt.Printf("indexed_at:   %s\n", val.IndexedAt.UTC().Format(time.RFC3339))
		fmt.Printf("file_count:   %d\n", val.FileCount)
		fmt.Printf("symbol_count: %d\n", val.SymbolCount)

	case []core.SearchHit:
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "SCORE\tFILE\tLINES\tQUALNAME")
		for _, h := range val {
			fmt.Fprintf(w, "%.4f\t%s\t%d-%d\t%s\n",
				h.Score, h.File, h.LineStart, h.LineEnd, h.Qualname)
		}
		return w.Flush()

	case core.SearchHit:
		fmt.Printf("score:    %.4f\n", val.Score)
		fmt.Printf("qualname: %s\n", val.Qualname)
		fmt.Printf("file:     %s:%d-%d\n", val.File, val.LineStart, val.LineEnd)
		fmt.Printf("snippet:\n%s\n", val.Snippet)

	case core.Symbol:
		fmt.Printf("qualname: %s\n", val.Qualname)
		fmt.Printf("kind:     %s\n", val.Kind)
		fmt.Printf("scope:    %s\n", val.Scope)
		fmt.Printf("file:     %s:%d-%d\n", val.File, val.LineStart, val.LineEnd)
		if val.Docstring != "" {
			fmt.Printf("doc:      %s\n", val.Docstring)
		}
		fmt.Printf("snippet:\n%s\n", val.Snippet)

	case core.Edge:
		fmt.Printf("%s -[%s]-> %s (resolved=%v)\n",
			val.FromQualname, val.Kind, val.ToQualname, val.Resolved)

	case []core.RegistryEntry:
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tPATH\tLAST_INDEXED\tFILES\tSYMBOLS")
		for _, e := range val {
			lastIdx := e.LastIndexed.UTC().Format(time.RFC3339)
			if e.LastIndexed.IsZero() {
				lastIdx = "(never)"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%d\n",
				e.Name, e.Path, lastIdx, e.FileCount, e.SymbolCount)
		}
		return w.Flush()

	default:
		fmt.Printf("%v\n", v)
	}
	return nil
}
