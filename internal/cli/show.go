package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/devchan97/code-map/internal/core"
	searchpkg "github.com/devchan97/code-map/internal/search"
)

func newShowCmd() *cobra.Command {
	var (
		flagRepo string
		jsonOut  bool
	)

	cmd := &cobra.Command{
		Use:   "show <QUALNAME|FILE:LINE>",
		Short: "Show the definition and snippet for a symbol",
		Long: `Look up a symbol by its fully-qualified name (e.g. app.module.func) or
by a FILE:LINE reference (e.g. src/foo.py:42).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := args[0]
			if target == "" {
				return fmt.Errorf("show: %w: target must not be empty", core.ErrUsage)
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
			sym, err := searchpkg.Show(ctx, st, target)
			if err != nil {
				return fmt.Errorf("show: %w", err)
			}

			if jsonOut {
				return WriteJSON(sym)
			}
			return WriteHuman(sym)
		},
	}

	addRepoFlag(cmd, &flagRepo)
	addJSONFlag(cmd, &jsonOut)
	return cmd
}
