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

func newCallsCmd() *cobra.Command {
	var (
		flagRepo string
		jsonOut  bool
	)

	cmd := &cobra.Command{
		Use:   "calls <QUALNAME>",
		Short: "Show outgoing calls (callees) of a symbol",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			qualname := args[0]
			if qualname == "" {
				return fmt.Errorf("calls: %w: QUALNAME must not be empty", core.ErrUsage)
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
			views, err := graph.Calls(ctx, st, qualname)
			if err != nil {
				return fmt.Errorf("calls: %w", err)
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
