package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/devchan97/code-map/internal/pipeline"
	"github.com/devchan97/code-map/internal/platform"
	"github.com/devchan97/code-map/internal/registry"
)

func newIndexCmd() *cobra.Command {
	var (
		flagRepo string
		jsonOut  bool
	)

	cmd := &cobra.Command{
		Use:   "index [PATH]",
		Short: "Incremental index: re-parse only changed files",
		Long: `Open the index for the resolved repo and re-index only files whose
SHA-1 has changed since the previous run.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var repoRoot string

			if len(args) == 1 {
				abs, err := filepath.Abs(args[0])
				if err != nil {
					return fmt.Errorf("index: resolve path: %w", err)
				}
				repoRoot = platform.ToSlash(filepath.Clean(abs))
			} else {
				entry, err := resolveRepo(flagRepo)
				if err != nil {
					return err
				}
				repoRoot = entry.Path
			}

			st, err := openStore(repoRoot)
			if err != nil {
				return err
			}
			defer st.Close()

			ctx := context.Background()
			sum, err := pipeline.Index(ctx, st, repoRoot, pipeline.IndexOptions{
				Progress: os.Stderr,
			})
			if err != nil {
				return fmt.Errorf("index: pipeline.Index: %w", err)
			}

			// Update registry mirror.
			reg := registry.NewFileRegistry("")
			entries, _ := reg.Load()
			for _, e := range entries {
				canon, _ := canonicalPath(e.Path)
				repoCanon, _ := canonicalPath(repoRoot)
				if canon == repoCanon {
					e.LastIndexed = sum.IndexedAt
					e.FileCount = sum.TotalFiles
					e.SymbolCount = sum.TotalSymbols
					_ = reg.Upsert(e)
					break
				}
			}

			if jsonOut {
				return WriteJSON(sum)
			}
			fmt.Printf("indexed at:  %s\n", sum.IndexedAt.UTC().Format(time.RFC3339))
			fmt.Printf("files:       parsed=%d  skipped=%d  removed=%d\n",
				sum.Parsed, sum.Skipped, sum.Removed)
			fmt.Printf("symbols:     %d\n", sum.Symbols)
			fmt.Printf("edges:       %d\n", sum.Edges)
			fmt.Printf("duration:    %s\n", sum.Duration.Round(time.Millisecond))
			return nil
		},
	}

	addRepoFlag(cmd, &flagRepo)
	addJSONFlag(cmd, &jsonOut)
	return cmd
}

// canonicalPath returns a canonical forward-slash absolute path.
func canonicalPath(p string) (string, error) {
	abs, err := filepath.Abs(platform.FromSlash(p))
	if err != nil {
		return "", err
	}
	return platform.ToSlash(filepath.Clean(abs)), nil
}
