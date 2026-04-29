package cli

import (
	"fmt"
	"os"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/devchan97/code-map/internal/install"
)

func newInstallSelfCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "install-self",
		Short: "Copy the running binary to ~/.codemap/bin and add it to PATH",
		Long: `Copy the running codemap binary to a stable location inside the user's
home directory (~/.codemap/bin) and ensure that location is on PATH so
'codemap' can be invoked from any shell.

This is idempotent — running it again when codemap is already installed
prints what is already in place and exits cleanly.

Windows: updates HKCU\Environment\Path (no admin required) and
broadcasts WM_SETTINGCHANGE so new shells see the change immediately.

Unix: appends an idempotent marker block to the user's shell rc file
(~/.bashrc, ~/.zshrc, etc.). Open a new shell or source the file to
pick up the change.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			self, err := os.Executable()
			if err != nil {
				return fmt.Errorf("install-self: locate self: %w", err)
			}
			res, err := install.InstallSelf(self)
			if err != nil {
				return err
			}
			fmt.Printf("binary:  %s\n", res.BinaryPath)
			if res.BinaryCopied {
				fmt.Println("         (copied)")
			} else {
				fmt.Println("         (already in place)")
			}
			if res.PathAdded {
				if res.ShellRCPath != "" {
					fmt.Printf("PATH:    appended marker block to %s\n", res.ShellRCPath)
					fmt.Println("         open a new shell, or `source` the file, to pick it up.")
				} else {
					fmt.Printf("PATH:    %s added to user PATH (HKCU\\Environment)\n", install.BinDir())
					fmt.Println("         open a new terminal to pick it up.")
				}
			} else {
				fmt.Printf("PATH:    %s already on PATH; no change\n", install.BinDir())
			}
			if res.PathAlreadyEffective {
				fmt.Println("ready:   yes (this shell can already run `codemap`)")
			} else {
				fmt.Println("ready:   in new shells")
			}
			return nil
		},
	}
}

func newUninstallSelfCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall-self",
		Short: "Remove the installed binary and undo the PATH change",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := install.UninstallSelf()
			if err != nil {
				return err
			}
			if res.BinaryCopied {
				fmt.Printf("removed: %s\n", res.BinaryPath)
			} else {
				fmt.Printf("absent:  %s (nothing to remove)\n", res.BinaryPath)
			}
			if res.PathAdded {
				if res.ShellRCPath != "" {
					fmt.Printf("PATH:    stripped marker block from %s\n", res.ShellRCPath)
				} else {
					fmt.Printf("PATH:    removed %s from user PATH\n", install.BinDir())
				}
			} else {
				fmt.Println("PATH:    no codemap entry to remove")
			}
			if runtime.GOOS != "windows" && res.ShellRCPath != "" {
				fmt.Println("note:    the change applies to new shells only.")
			}
			return nil
		},
	}
}
