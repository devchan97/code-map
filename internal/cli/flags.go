package cli

import "github.com/spf13/cobra"

// commonFlags holds flags that appear on multiple subcommands.
type commonFlags struct {
	repo string
	json bool
}

// addRepoFlag adds the --repo flag to cmd, storing the value in dst.
func addRepoFlag(cmd *cobra.Command, dst *string) {
	cmd.Flags().StringVar(dst, "repo", "", "repository NAME or PATH to operate on")
}

// addJSONFlag adds the --json flag to cmd, storing the value in dst.
func addJSONFlag(cmd *cobra.Command, dst *bool) {
	cmd.Flags().BoolVar(dst, "json", false, "emit output as JSON")
}
