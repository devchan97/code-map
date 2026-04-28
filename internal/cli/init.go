package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/devchan97/code-map/internal/core"
	"github.com/devchan97/code-map/internal/pipeline"
	"github.com/devchan97/code-map/internal/platform"
	"github.com/devchan97/code-map/internal/registry"
	"github.com/devchan97/code-map/internal/store"
)

func newInitCmd() *cobra.Command {
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "init [PATH]",
		Short: "Idempotently initialise and index a repository",
		Long: `Create (or reuse) <PATH>/.codemap/, register the repo in the global
registry, and run the first incremental index. Idempotent: safe to run again
on an already-initialised repository.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Resolve PATH argument.
			rawPath := "."
			if len(args) == 1 {
				rawPath = args[0]
			}

			abs, err := filepath.Abs(rawPath)
			if err != nil {
				return fmt.Errorf("init: resolve path: %w", err)
			}
			repoRoot := platform.ToSlash(filepath.Clean(abs))

			// Open (and init-schema if necessary) the store.
			st, err := store.Open(repoRoot)
			if err != nil {
				return fmt.Errorf("init: open store: %w", err)
			}
			defer st.Close()

			// Register in global registry (upsert with zero LastIndexed).
			reg := registry.NewFileRegistry("")
			entry := core.RegistryEntry{
				Name:        filepath.Base(platform.FromSlash(repoRoot)),
				Path:        repoRoot,
				CreatedAt:   time.Now().UTC(),
				LastIndexed: time.Time{},
				Embedder:    "lexical",
				SchemaVer:   store.SchemaVer,
			}
			if err := reg.Upsert(entry); err != nil {
				return fmt.Errorf("init: upsert registry: %w", err)
			}

			// Run first incremental index.
			ctx := context.Background()
			sum, err := pipeline.Index(ctx, st, repoRoot, pipeline.IndexOptions{
				Progress: os.Stderr,
			})
			if err != nil {
				return fmt.Errorf("init: pipeline.Index: %w", err)
			}

			// Update registry mirror with results.
			entry.LastIndexed = sum.IndexedAt
			entry.FileCount = sum.Parsed + sum.Skipped
			entry.SymbolCount = sum.Symbols
			if err := reg.Upsert(entry); err != nil {
				return fmt.Errorf("init: update registry: %w", err)
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

	addJSONFlag(cmd, &jsonOut)
	return cmd
}
