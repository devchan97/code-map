package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/devchan97/code-map/internal/core"
	"github.com/devchan97/code-map/internal/store"
)

// statusOutput is the JSON shape for codemap status --json (architecture §6.2).
type statusOutput struct {
	Name        string    `json:"name"`
	Path        string    `json:"path"`
	LastIndexed time.Time `json:"last_indexed"`
	FileCount   int       `json:"file_count"`
	SymbolCount int       `json:"symbol_count"`
	Embedder    string    `json:"embedder"`
	SchemaVer   int       `json:"schema_ver"`
	Stale       bool      `json:"stale"`
}

// readMeta reads the meta table from st inside a short read-only transaction.
func readMeta(ctx context.Context, st *store.Store) (core.Meta, error) {
	var m core.Meta
	if err := st.WithTx(ctx, func(tx store.Tx) error {
		var err error
		m, err = tx.ReadMeta()
		return err
	}); err != nil {
		return core.Meta{}, fmt.Errorf("readMeta: %w", err)
	}
	return m, nil
}

func newStatusCmd() *cobra.Command {
	var (
		flagRepo string
		jsonOut  bool
	)

	cmd := &cobra.Command{
		Use:   "status [PATH|NAME]",
		Short: "Show index statistics and last-indexed timestamp",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Positional arg overrides --repo.
			repo := flagRepo
			if len(args) == 1 {
				repo = args[0]
			}

			entry, err := resolveRepo(repo)
			if err != nil {
				return err
			}

			st, err := openStore(entry.Path)
			if err != nil {
				return err
			}
			defer st.Close()

			ctx := context.Background()
			meta, err := readMeta(ctx, st)
			if err != nil {
				return fmt.Errorf("status: %w", err)
			}

			out := statusOutput{
				Name:        entry.Name,
				Path:        entry.Path,
				LastIndexed: meta.IndexedAt,
				FileCount:   meta.FileCount,
				SymbolCount: meta.SymbolCount,
				Embedder:    meta.Embedder,
				SchemaVer:   meta.SchemaVer,
				Stale:       false, // v1: always false
			}

			if jsonOut {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(out)
			}

			fmt.Printf("name:         %s\n", out.Name)
			fmt.Printf("path:         %s\n", out.Path)
			fmt.Printf("last_indexed: %s\n", out.LastIndexed.UTC().Format(time.RFC3339))
			fmt.Printf("file_count:   %d\n", out.FileCount)
			fmt.Printf("symbol_count: %d\n", out.SymbolCount)
			fmt.Printf("embedder:     %s\n", out.Embedder)
			fmt.Printf("schema_ver:   %d\n", out.SchemaVer)
			fmt.Printf("stale:        %v\n", out.Stale)
			return nil
		},
	}

	addRepoFlag(cmd, &flagRepo)
	addJSONFlag(cmd, &jsonOut)
	return cmd
}
