package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/devchan97/code-map/internal/core"
	"github.com/devchan97/code-map/internal/graph"
)

// edgeEndpoint is the JSON shape for one end of an edge (architecture §6.2).
type edgeEndpoint struct {
	Qualname  string `json:"qualname"`
	File      string `json:"file"`
	LineStart int    `json:"line_start"`
	LineEnd   int    `json:"line_end"`
}

// edgeJSON is the JSON shape for a graph edge (architecture §6.2 refs/calls).
type edgeJSON struct {
	From     edgeEndpoint  `json:"from"`
	To       edgeEndpoint  `json:"to"`
	Kind     core.EdgeKind `json:"kind"`
	Resolved bool          `json:"resolved"`
}

// edgeViewsToJSON converts []graph.EdgeView to []edgeJSON for stable JSON output.
func edgeViewsToJSON(views []graph.EdgeView) []edgeJSON {
	out := make([]edgeJSON, 0, len(views))
	for _, ev := range views {
		from := edgeEndpoint{Qualname: ev.FromName}
		if ev.From != nil {
			from.File = ev.From.File
			from.LineStart = ev.From.LineStart
			from.LineEnd = ev.From.LineEnd
		}

		to := edgeEndpoint{Qualname: ev.ToName}
		if ev.To != nil {
			to.File = ev.To.File
			to.LineStart = ev.To.LineStart
			to.LineEnd = ev.To.LineEnd
		}

		out = append(out, edgeJSON{
			From:     from,
			To:       to,
			Kind:     ev.Kind,
			Resolved: ev.Resolved,
		})
	}
	return out
}

func newRefsCmd() *cobra.Command {
	var (
		flagRepo string
		jsonOut  bool
	)

	cmd := &cobra.Command{
		Use:   "refs <QUALNAME>",
		Short: "Show incoming references (callers) of a symbol",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			qualname := args[0]
			if qualname == "" {
				return fmt.Errorf("refs: %w: QUALNAME must not be empty", core.ErrUsage)
			}

			entry, err := resolveRepo(flagRepo)
			if err != nil {
				return err
			}

			st, err := openStore(entry.Path)
			if err != nil {
				return err
			}
			defer st.Close()

			ctx := context.Background()
			views, err := graph.Refs(ctx, st, qualname)
			if err != nil {
				return fmt.Errorf("refs: %w", err)
			}

			if jsonOut {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(edgeViewsToJSON(views))
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "FROM\tTO\tKIND\tRESOLVED")
			for _, ev := range views {
				fmt.Fprintf(w, "%s\t%s\t%s\t%v\n", ev.FromName, ev.ToName, ev.Kind, ev.Resolved)
			}
			return w.Flush()
		},
	}

	addRepoFlag(cmd, &flagRepo)
	addJSONFlag(cmd, &jsonOut)
	return cmd
}
