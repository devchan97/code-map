// Package cli wires cobra subcommand handlers to codemap's domain packages.
package cli

import "github.com/spf13/cobra"

// NewRoot returns the root cobra command with all subcommands registered.
func NewRoot() *cobra.Command {
	root := &cobra.Command{
		Use:           "codemap",
		Short:         "codemap — local code-retrieval CLI for coding agents",
		SilenceUsage:  true,
		SilenceErrors: false,
	}

	// Disable the auto-generated completion subcommand.
	root.CompletionOptions.DisableDefaultCmd = true

	root.AddCommand(
		newInitCmd(),
		newIndexCmd(),
		newReindexCmd(),
		newListCmd(),
		newStatusCmd(),
		newForgetCmd(),
		newSearchCmd(),
		newShowCmd(),
		newRefsCmd(),
		newCallsCmd(),
		newVisualizeCmd(),
		newInstallSkillCmd(),
		newUninstallSkillCmd(),
		newInstallSelfCmd(),
		newUninstallSelfCmd(),
		newVersionCmd(),
	)

	return root
}
