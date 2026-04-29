package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/devchan97/code-map/internal/skill"
)

func newInstallSkillCmd() *cobra.Command {
	var (
		scope     string
		agent     string
		printOnly bool
		flagRepo  string
	)

	cmd := &cobra.Command{
		Use:   "install-skill",
		Short: "Install the SKILL.md file for the specified agent",
		Long: `Write (or print) the codemap SKILL.md at the path expected by the
target agent. Use --scope project to place it inside the current repo.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			var repoPath string
			if scope == "project" {
				// Resolve repo root for project scope.
				entry, err := resolveRepo(flagRepo)
				if err != nil {
					return fmt.Errorf("install-skill: %w", err)
				}
				repoPath = entry.Path
			}

			t := skill.Target{
				Agent:      agent,
				Scope:      scope,
				Repo:       repoPath,
				BinaryName: "codemap",
				Version:    Version,
			}

			result, err := skill.Install(t, printOnly)
			if err != nil {
				// codex spec is still upstream-pending; turn the long
				// wrap chain (`install-skill: skill: install: skill:
				// resolve path: ...`) into a single user-readable line.
				if errors.Is(err, skill.ErrCodexPending) {
					return fmt.Errorf(
						"agent %q is not yet supported (codex skill spec is still being finalised upstream).\n"+
							"Use --print to dump the SKILL.md body and place it manually, or use --agent claude-code",
						agent)
				}
				return fmt.Errorf("install-skill: %w", err)
			}

			if printOnly {
				fmt.Fprint(cmd.OutOrStdout(), result)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "installed: %s\n", result)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&scope, "scope", "user", "scope: user or project")
	cmd.Flags().StringVar(&agent, "agent", "claude-code", "agent: claude-code or codex")
	cmd.Flags().BoolVar(&printOnly, "print", false, "print the SKILL.md body to stdout instead of writing to disk")
	addRepoFlag(cmd, &flagRepo)
	return cmd
}

func newUninstallSkillCmd() *cobra.Command {
	var (
		scope    string
		agent    string
		flagRepo string
	)

	cmd := &cobra.Command{
		Use:   "uninstall-skill",
		Short: "Remove the SKILL.md file for the specified agent",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			var repoPath string
			if scope == "project" {
				entry, err := resolveRepo(flagRepo)
				if err != nil {
					return fmt.Errorf("uninstall-skill: %w", err)
				}
				repoPath = entry.Path
			}

			t := skill.Target{
				Agent: agent,
				Scope: scope,
				Repo:  repoPath,
			}

			if err := skill.Uninstall(t); err != nil {
				return fmt.Errorf("uninstall-skill: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "uninstalled skill for agent=%s scope=%s\n", agent, scope)
			return nil
		},
	}

	cmd.Flags().StringVar(&scope, "scope", "user", "scope: user or project")
	cmd.Flags().StringVar(&agent, "agent", "claude-code", "agent: claude-code or codex")
	addRepoFlag(cmd, &flagRepo)
	return cmd
}
