package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/devchan97/code-map/internal/registry"
)

func newForgetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "forget <PATH|NAME>",
		Short: "Remove a repository from the global registry",
		Long: `Remove the registry entry for the given NAME or PATH. The .codemap/
directory inside the repository is not touched.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := args[0]

			reg := registry.NewFileRegistry("")
			if err := reg.Remove(target); err != nil {
				return fmt.Errorf("forget: %w", err)
			}

			fmt.Printf("removed %q from registry\n", target)
			return nil
		},
	}

	return cmd
}
