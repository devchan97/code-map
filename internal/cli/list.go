package cli

import (
	"github.com/spf13/cobra"

	"github.com/devchan97/code-map/internal/registry"
)

func newListCmd() *cobra.Command {
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all registered repositories",
		Long:  `Print a table of all repos known to the global registry (NAME, PATH, LAST_INDEXED, FILES, SYMBOLS).`,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			reg := registry.NewFileRegistry("")
			entries, err := reg.Load()
			if err != nil {
				return err
			}

			if jsonOut {
				return WriteJSON(entries)
			}
			return WriteHuman(entries)
		},
	}

	addJSONFlag(cmd, &jsonOut)
	return cmd
}
