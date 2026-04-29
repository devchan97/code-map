// Package pipeline orchestrates the indexing pipeline (walker → parser → store) for codemap.
package pipeline

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/devchan97/code-map/internal/core"
	"github.com/devchan97/code-map/internal/encoder"
	"github.com/devchan97/code-map/internal/lexical"
	"github.com/devchan97/code-map/internal/parser"
	"github.com/devchan97/code-map/internal/parser/fallback"
	"github.com/devchan97/code-map/internal/store"
	"github.com/devchan97/code-map/internal/walker"
)

// IndexOptions controls an indexing run.
type IndexOptions struct {
	// Force causes all files to be re-indexed even if their SHA-1 is unchanged.
	Force bool
	// Concurrency sets the number of parallel parser workers.
	// Zero means runtime.NumCPU(), capped at 16.
	Concurrency int
	// Encoder is an optional dense-vector encoder used for symbol embedding.
	// When nil, only lexical indexing is performed.
	Encoder encoder.Encoder
	// Progress is an optional writer for human-readable progress output (e.g. os.Stderr).
	Progress io.Writer
}

// Summary describes the result of an indexing run.
type Summary struct {
	// Parsed is the number of files that were (re-)parsed in this run.
	Parsed int
	// Skipped is the number of files skipped due to parse errors or unchanged SHA-1.
	Skipped int
	// Removed is the number of files deleted from the index because they no
	// longer exist on disk.
	Removed int
	// Symbols is the total number of symbols inserted in this run.
	Symbols int
	// Edges is the total number of edges inserted in this run.
	Edges int
	// TotalSymbols is the cumulative symbol count in the index after this run
	// (matches the value written to meta.symbol_count). Use this — not
	// Symbols — when mirroring counts into the registry so that `status`
	// (reads SQLite meta) and `list` (reads registry) agree.
	TotalSymbols int
	// TotalFiles is the cumulative file count in the index after this run
	// (matches meta.file_count). Same rationale as TotalSymbols.
	TotalFiles int
	// Duration is the wall-clock time for the full indexing pass.
	Duration time.Duration
	// IndexedAt is the UTC timestamp written to the meta table.
	IndexedAt time.Time
}

// parseResult holds the output of parsing a single file.
type parseResult struct {
	Path    string
	Lang    string
	SHA1    string
	Size    int64
	Symbols []core.Symbol
	Edges   []core.Edge
	Err     error
}

// Index runs an incremental indexing pass over repoRoot using the given store.
// Files whose SHA-1 has not changed since the previous run are skipped.
// All writes happen inside a single transaction; a failure rolls back completely.
func Index(ctx context.Context, st *store.Store, repoRoot string, opts IndexOptions) (Summary, error) {
	start := time.Now()

	// --- 1. Load previous file records (short-lived read tx). ---
	var prev []core.File
	if err := st.WithTx(ctx, func(tx store.Tx) error {
		var err error
		prev, err = tx.ListFiles()
		return err
	}); err != nil {
		return Summary{}, fmt.Errorf("pipeline.Index: list previous files: %w", err)
	}

	// --- 2. Walk the repo. ---
	var current []walker.Entry
	if err := walker.Walk(ctx, repoRoot, walker.Options{}, func(e walker.Entry) error {
		current = append(current, e)
		return nil
	}); err != nil {
		return Summary{}, fmt.Errorf("pipeline.Index: walk: %w", err)
	}

	// --- 3. Compute diff. ---
	changed, removed := ChangedFiles(prev, current)

	// If force mode, treat all current files as changed.
	if opts.Force && len(current) > 0 {
		changed = current
		// removed stays as-is (files truly deleted from disk)
	}

	// --- 4. Fast path: nothing to do. ---
	if len(changed) == 0 && len(removed) == 0 && !opts.Force {
		now := time.Now().UTC()
		var fastSymbols, fastFiles int
		// Still write updated indexed_at.
		if err := st.WithTx(ctx, func(tx store.Tx) error {
			meta, err := tx.ReadMeta()
			if err != nil {
				return err
			}
			meta.IndexedAt = now
			fastSymbols = meta.SymbolCount
			fastFiles = meta.FileCount
			return tx.WriteMeta(meta)
		}); err != nil {
			return Summary{}, fmt.Errorf("pipeline.Index: update meta (no-op): %w", err)
		}
		return Summary{
			Skipped:      len(prev),
			TotalSymbols: fastSymbols,
			TotalFiles:   fastFiles,
			Duration:     time.Since(start),
			IndexedAt:    now,
		}, nil
	}

	// --- 5. Concurrency setup. ---
	concurrency := opts.Concurrency
	if concurrency <= 0 {
		concurrency = runtime.NumCPU()
	}
	if concurrency > 16 {
		concurrency = 16
	}

	// --- 6. Set up progress reporter. ---
	progressWriter := opts.Progress
	if progressWriter == nil {
		progressWriter = os.Stderr
	}
	prog := NewProgress(progressWriter, len(changed))

	// --- 7. Parse all changed files concurrently. ---
	inCh := make(chan walker.Entry, len(changed))
	outCh := make(chan parseResult, len(changed))

	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case e, ok := <-inCh:
					if !ok {
						return
					}
					res := parseFile(ctx, repoRoot, e)
					if res.Err != nil {
						prog.IncSkipped()
					} else {
						prog.IncParsed()
					}
					outCh <- res
				}
			}
		}()
	}

	// Feed input channel.
	for _, e := range changed {
		inCh <- e
	}
	close(inCh)

	// Wait for workers then close output channel.
	go func() {
		wg.Wait()
		close(outCh)
	}()

	// Drain output into a map, then reorder by changed-order for determinism.
	resultMap := make(map[string]parseResult, len(changed))
	for res := range outCh {
		resultMap[res.Path] = res
	}

	prog.Finish()

	// --- 8. Single transaction: delete removed, upsert changed. ---
	now := time.Now().UTC()
	var totalSymbols, totalEdges, parsedCount, skippedCount int
	var totalSymbolsAfter, totalFilesAfter int

	if err := st.WithTx(ctx, func(tx store.Tx) error {
		// 8a. Delete removed files.
		for _, path := range removed {
			if err := tx.DeleteFileArtifacts(path); err != nil {
				return fmt.Errorf("delete artifacts %q: %w", path, err)
			}
			if err := tx.DeleteTokensForFile(path); err != nil {
				return fmt.Errorf("delete tokens %q: %w", path, err)
			}
		}

		// 8b. Process each changed file in input order.
		for _, e := range changed {
			res := resultMap[e.Path]

			if res.Err != nil {
				slog.Warn("pipeline: parse error; file will be retried next run",
					"path", e.Path, "err", res.Err)
				skippedCount++
				continue
			}

			// Clean up previous artifacts for this file.
			if err := tx.DeleteFileArtifacts(e.Path); err != nil {
				return fmt.Errorf("delete artifacts %q: %w", e.Path, err)
			}
			if err := tx.DeleteTokensForFile(e.Path); err != nil {
				return fmt.Errorf("delete tokens %q: %w", e.Path, err)
			}

			// Upsert file row.
			if err := tx.UpsertFile(core.File{
				Path:      e.Path,
				SHA1:      e.SHA1,
				IndexedAt: now,
				Language:  e.Lang,
				SizeBytes: e.Size,
			}); err != nil {
				return fmt.Errorf("upsert file %q: %w", e.Path, err)
			}

			// Ensure all symbols reference the correct file path.
			for i := range res.Symbols {
				res.Symbols[i].File = e.Path
			}

			// Insert symbols and collect IDs.
			ids, err := tx.InsertSymbols(res.Symbols)
			if err != nil {
				return fmt.Errorf("insert symbols %q: %w", e.Path, err)
			}
			totalSymbols += len(ids)

			// Insert edges.
			if err := tx.InsertEdges(res.Edges, e.Path); err != nil {
				return fmt.Errorf("insert edges %q: %w", e.Path, err)
			}
			totalEdges += len(res.Edges)

			// Insert lexical tokens for each symbol.
			for j, sym := range res.Symbols {
				toks := lexical.Tokenize(sym)
				if err := tx.InsertTokens(ids[j], toks); err != nil {
					return fmt.Errorf("insert tokens symbol %d: %w", ids[j], err)
				}
			}

			// Encoder vector storage: not yet supported — Tx interface does not
			// expose UpsertSymbolVectors. Log a warning and continue.
			// TODO(M5): extend Tx with UpsertSymbolVectors and wire up encoder here.
			if opts.Encoder != nil {
				slog.Warn("pipeline: encoder support requires Tx extension (TODO M5); vector storage skipped",
					"path", e.Path)
			}

			parsedCount++
		}

		// 8c. Resolve cross-file edges.
		if err := tx.ResolveEdges(); err != nil {
			return fmt.Errorf("resolve edges: %w", err)
		}

		// 8d. Build and write updated meta.
		files, err := tx.ListFiles()
		if err != nil {
			return fmt.Errorf("list files for meta: %w", err)
		}
		n, _, err := tx.Stats(ctx)
		if err != nil {
			return fmt.Errorf("stats for meta: %w", err)
		}

		embedder := "lexical"
		if opts.Encoder != nil {
			embedder = "lexical+" + opts.Encoder.Name()
		}

		meta := core.Meta{
			SchemaVer:   store.SchemaVer,
			RepoRoot:    repoRoot,
			IndexedAt:   now,
			Embedder:    embedder,
			SymbolCount: n,
			FileCount:   len(files),
		}
		totalSymbolsAfter = n
		totalFilesAfter = len(files)
		return tx.WriteMeta(meta)
	}); err != nil {
		return Summary{}, fmt.Errorf("pipeline.Index: transaction: %w", err)
	}

	return Summary{
		Parsed:       parsedCount,
		Skipped:      skippedCount,
		Removed:      len(removed),
		Symbols:      totalSymbols,
		Edges:        totalEdges,
		TotalSymbols: totalSymbolsAfter,
		TotalFiles:   totalFilesAfter,
		Duration:     time.Since(start),
		IndexedAt:    now,
	}, nil
}

// Reindex truncates the index and rebuilds it from scratch.
// It deletes all existing artifacts for every file, then runs a full Index pass
// with Force=true. All operations are performed inside transactions so any
// failure leaves the index in a clean prior state.
func Reindex(ctx context.Context, st *store.Store, repoRoot string, opts IndexOptions) (Summary, error) {
	// Delete all existing data by iterating files and removing their artifacts.
	// This avoids extending the Tx interface with a bulk-truncate method.
	if err := st.WithTx(ctx, func(tx store.Tx) error {
		files, err := tx.ListFiles()
		if err != nil {
			return fmt.Errorf("list files: %w", err)
		}
		for _, f := range files {
			if err := tx.DeleteFileArtifacts(f.Path); err != nil {
				return fmt.Errorf("delete artifacts %q: %w", f.Path, err)
			}
			if err := tx.DeleteTokensForFile(f.Path); err != nil {
				return fmt.Errorf("delete tokens %q: %w", f.Path, err)
			}
		}
		return nil
	}); err != nil {
		return Summary{}, fmt.Errorf("pipeline.Reindex: truncate: %w", err)
	}

	// Run a full index pass with Force=true so all files are re-parsed.
	opts.Force = true
	return Index(ctx, st, repoRoot, opts)
}

// parseFile reads and parses a single walker.Entry, returning a parseResult.
// It never returns a fatal error; parse failures are captured in parseResult.Err.
func parseFile(ctx context.Context, repoRoot string, e walker.Entry) parseResult {
	res := parseResult{
		Path: e.Path,
		Lang: e.Lang,
		SHA1: e.SHA1,
		Size: e.Size,
	}

	// Build absolute path. Walker paths are repo-relative forward-slash strings.
	absPath := filepath.Join(filepath.FromSlash(repoRoot), filepath.FromSlash(e.Path))

	// Check context before performing IO.
	select {
	case <-ctx.Done():
		res.Err = ctx.Err()
		return res
	default:
	}

	src, err := os.ReadFile(absPath)
	if err != nil {
		res.Err = fmt.Errorf("read file: %w", err)
		return res
	}

	// Lookup language-specific parser; fall back to the generic whole-file adapter.
	var p interface {
		Parse(path string, src []byte) ([]core.Symbol, []core.Edge, error)
	}
	if lp, ok := parser.For(e.Lang); ok {
		p = lp
	} else {
		p = fallback.New()
	}

	syms, edges, err := p.Parse(e.Path, src)
	if err != nil {
		res.Err = fmt.Errorf("parse: %w", err)
		return res
	}

	res.Symbols = syms
	res.Edges = edges
	return res
}
