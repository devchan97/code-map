package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/devchan97/code-map/internal/core"
	"github.com/devchan97/code-map/internal/encoder"
	searchpkg "github.com/devchan97/code-map/internal/search"
)

func newSearchCmd() *cobra.Command {
	var (
		flagRepo string
		jsonOut  bool
		top      int
		kindStr  string
		scopeStr string
		fileGlob string
		rerank   bool
	)

	cmd := &cobra.Command{
		Use:   "search <QUERY>",
		Short: "Search the index for symbols matching QUERY",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := args[0]
			if query == "" {
				return fmt.Errorf("search: %w: QUERY must not be empty", core.ErrUsage)
			}

			entry, err := resolveRepo(flagRepo)
			if err != nil {
				return err
			}

			st, err := openStore(entry.Path)
			if err != nil {
				return err
			}
			defer st.Close()

			// Parse --kind (comma-separated).
			var kinds []core.SymbolKind
			if kindStr != "" {
				for _, part := range strings.Split(kindStr, ",") {
					part = strings.TrimSpace(part)
					if part == "" {
						continue
					}
					k, err := core.ParseSymbolKind(part)
					if err != nil {
						return fmt.Errorf("search: invalid --kind %q: %w", part, core.ErrUsage)
					}
					kinds = append(kinds, k)
				}
			}

			// Parse --scope (comma-separated).
			var scopes []core.Scope
			if scopeStr != "" {
				for _, part := range strings.Split(scopeStr, ",") {
					part = strings.TrimSpace(part)
					if part == "" {
						continue
					}
					s, err := core.ParseScope(part)
					if err != nil {
						return fmt.Errorf("search: invalid --scope %q: %w", part, core.ErrUsage)
					}
					scopes = append(scopes, s)
				}
			}

			// Resolve encoder for rerank.
			var enc encoder.Encoder
			if rerank {
				e, encErr := encoder.Default()
				if encErr != nil {
					slog.Warn("search: encoder not available; rerank disabled", "err", encErr)
				} else {
					enc = e
				}
			}

			topN := top
			if topN <= 0 {
				topN = 10
			}

			q := searchpkg.Query{
				Text:     query,
				TopN:     topN,
				Kinds:    kinds,
				Scopes:   scopes,
				FileGlob: fileGlob,
				Rerank:   rerank && enc != nil,
			}

			ctx := context.Background()
			hits, err := searchpkg.Run(ctx, st, enc, q)
			if err != nil {
				return fmt.Errorf("search: %w", err)
			}

			if jsonOut {
				return WriteJSON(hits)
			}

			if len(hits) == 0 {
				fmt.Fprintln(os.Stderr, "no results")
				return nil
			}
			return WriteHuman(hits)
		},
	}

	addRepoFlag(cmd, &flagRepo)
	addJSONFlag(cmd, &jsonOut)
	cmd.Flags().IntVar(&top, "top", 10, "maximum number of results")
	cmd.Flags().StringVar(&kindStr, "kind", "", "comma-separated symbol kinds (function,method,class,...)")
	cmd.Flags().StringVar(&scopeStr, "scope", "", "comma-separated scopes (global,class,local,param)")
	cmd.Flags().StringVar(&fileGlob, "file", "", "file glob filter (doublestar)")
	cmd.Flags().BoolVar(&rerank, "rerank", false, "rerank results with encoder (requires -tags encoder build)")
	return cmd
}
