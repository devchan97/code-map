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
	IndexerVer  int       `json:"indexer_ver"`
	// Stale is true when the index data was produced by a different
	// IndexerVer than the running binary. The DB still reads, but
	// search results may not reflect the current parser/tokenizer
	// rules; user should run `codemap reindex`.
	Stale       bool   `json:"stale"`
	StaleReason string `json:"stale_reason,omitempty"`
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

			// Surface indexer_ver skew as a stale flag rather than a
			// hard error: the data is still readable, just produced by
			// an older parser/tokenizer that may emit different
			// qualnames or tokens than the running binary.
			stale := false
			reason := ""
			if meta.IndexerVer != 0 && meta.IndexerVer != store.IndexerVer {
				stale = true
				reason = fmt.Sprintf(
					"indexer_ver %d on disk, %d in this binary — run `codemap reindex` to refresh",
					meta.IndexerVer, store.IndexerVer,
				)
			} else if meta.IndexerVer == 0 {
				// Pre-IndexerVer index. Treat as stale once the binary
				// understands the field; user reindex is cheap.
				stale = true
				reason = fmt.Sprintf(
					"index predates indexer_ver tracking — run `codemap reindex` to upgrade to indexer_ver %d",
					store.IndexerVer,
				)
			}

			out := statusOutput{
				Name:        entry.Name,
				Path:        entry.Path,
				LastIndexed: meta.IndexedAt,
				FileCount:   meta.FileCount,
				SymbolCount: meta.SymbolCount,
				Embedder:    meta.Embedder,
				SchemaVer:   meta.SchemaVer,
				IndexerVer:  meta.IndexerVer,
				Stale:       stale,
				StaleReason: reason,
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
			fmt.Printf("indexer_ver:  %d\n", out.IndexerVer)
			fmt.Printf("stale:        %v\n", out.Stale)
			if out.StaleReason != "" {
				fmt.Printf("              %s\n", out.StaleReason)
			}
			return nil
		},
	}

	addRepoFlag(cmd, &flagRepo)
	addJSONFlag(cmd, &jsonOut)
	return cmd
}
