package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/devchan97/code-map/internal/core"
	"github.com/devchan97/code-map/internal/platform"
	"github.com/devchan97/code-map/internal/store"
	"github.com/devchan97/code-map/internal/visualize"
)

// viewData holds eagerly-fetched data used by the visualize DataSource adapter.
type viewData struct {
	symbols []core.Symbol
	edges   []core.Edge
	meta    core.Meta
}

// tableDataSource implements visualize.DataSource wrapping pre-fetched data.
type tableDataSource struct {
	sym  []core.Symbol
	eds  []core.Edge
	meta core.Meta
}

func (d *tableDataSource) AllSymbols(_ context.Context, _ string, _ []core.SymbolKind) ([]core.Symbol, error) {
	return d.sym, nil
}

func (d *tableDataSource) AllEdges(_ context.Context) ([]core.Edge, error) {
	return d.eds, nil
}

func (d *tableDataSource) ReadMeta() (core.Meta, error) {
	return d.meta, nil
}

func newVisualizeCmd() *cobra.Command {
	var (
		flagRepo string
		outPath  string
		open     bool
	)

	cmd := &cobra.Command{
		Use:   "visualize [PATH|NAME]",
		Short: "Generate a static graph.html visualization of the index",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
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

			// Eagerly gather all data inside one transaction.
			ctx := context.Background()
			vd, err := gatherViewData(ctx, st)
			if err != nil {
				return fmt.Errorf("visualize: gather data: %w", err)
			}

			ds := &tableDataSource{
				sym:  vd.symbols,
				eds:  vd.edges,
				meta: vd.meta,
			}

			opts := visualize.Options{
				OutPath: outPath,
				Open:    open,
			}

			repoRoot := platform.ToSlash(filepath.Clean(platform.FromSlash(entry.Path)))
			writtenPath, err := visualize.Render(ctx, ds, repoRoot, opts)
			if err != nil {
				return fmt.Errorf("visualize: render: %w", err)
			}

			fmt.Fprintln(os.Stdout, writtenPath)
			return nil
		},
	}

	addRepoFlag(cmd, &flagRepo)
	cmd.Flags().StringVar(&outPath, "out", "", "output file path (default: <repo>/.codemap/graph.html)")
	cmd.Flags().BoolVar(&open, "open", false, "open the rendered HTML in the default browser")
	return cmd
}

// gatherViewData runs a single WithTx to collect symbols, edges, and meta.
func gatherViewData(ctx context.Context, st *store.Store) (viewData, error) {
	var vd viewData
	if err := st.WithTx(ctx, func(tx store.Tx) error {
		var err error
		vd.meta, err = tx.ReadMeta()
		if err != nil {
			return fmt.Errorf("read meta: %w", err)
		}
		vd.symbols, err = tx.AllSymbols("", nil)
		if err != nil {
			return fmt.Errorf("all symbols: %w", err)
		}
		vd.edges, err = tx.AllEdges()
		if err != nil {
			return fmt.Errorf("all edges: %w", err)
		}
		return nil
	}); err != nil {
		return viewData{}, fmt.Errorf("gatherViewData: %w", err)
	}
	return vd, nil
}
