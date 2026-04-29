package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/devchan97/code-map/internal/core"
	searchpkg "github.com/devchan97/code-map/internal/search"
)

func newShowCmd() *cobra.Command {
	var (
		flagRepo string
		jsonOut  bool
		lines    int
		full     bool
	)

	cmd := &cobra.Command{
		Use:   "show <QUALNAME|FILE:LINE>",
		Short: "Show the definition and snippet for a symbol",
		Long: `Look up a symbol by its fully-qualified name (e.g. app.module.func) or
by a FILE:LINE reference (e.g. src/foo.py:42).

By default the snippet is capped at the first 10 lines (the indexed value).
Use --lines N to read up to N lines from disk for this symbol's range, or
--full to read the entire LineStart..LineEnd range. These flags require an
on-disk file at the indexed path.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := args[0]
			if target == "" {
				return fmt.Errorf("show: %w: target must not be empty", core.ErrUsage)
			}
			if lines < 0 {
				return fmt.Errorf("show: %w: --lines must be non-negative", core.ErrUsage)
			}
			if full && lines > 0 {
				return fmt.Errorf("show: %w: --lines and --full are mutually exclusive", core.ErrUsage)
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

			ctx := context.Background()
			sym, err := searchpkg.Show(ctx, st, target)
			if err != nil {
				return fmt.Errorf("show: %w", err)
			}

			// If the caller asked for an expanded body, splice it into
			// sym.Snippet before rendering. JSON output preserves the
			// expanded snippet so machine consumers see the same thing.
			if full || lines > 0 {
				body, readErr := readSymbolBody(entry.Path, sym, lines, full)
				if readErr != nil {
					// Soft-fail: keep the indexed snippet but print a notice
					// so the caller knows the on-disk read didn't happen.
					fmt.Fprintf(os.Stderr, "notice: --lines/--full read failed (%v); falling back to indexed snippet\n", readErr)
				} else {
					sym.Snippet = body
				}
			}

			if jsonOut {
				return WriteJSON(sym)
			}
			if err := WriteHuman(sym); err != nil {
				return err
			}
			// Always emit a partial-Read hint so callers can fetch the full
			// body in one round-trip without guessing line numbers. Hint goes
			// to stdout (after the snippet) so it lands in the agent's view.
			fmt.Println(readHintLine(sym))
			return nil
		},
	}

	addRepoFlag(cmd, &flagRepo)
	addJSONFlag(cmd, &jsonOut)
	cmd.Flags().IntVar(&lines, "lines", 0,
		"read up to N lines of the symbol's body from disk (0 = use indexed 10-line snippet)")
	cmd.Flags().BoolVar(&full, "full", false,
		"read the symbol's entire LineStart..LineEnd range from disk")
	return cmd
}

// readSymbolBody reads sym's source range from the on-disk file under
// repoRoot. With full=true it reads the whole LineStart..LineEnd range;
// otherwise it reads min(maxLines, range) lines starting at LineStart.
// Returns the joined text without a trailing newline, plus a "…" marker
// when the body is truncated below the symbol's full length.
func readSymbolBody(repoRoot string, sym core.Symbol, maxLines int, full bool) (string, error) {
	if sym.LineStart < 1 || sym.LineEnd < sym.LineStart {
		return "", fmt.Errorf("invalid line range %d-%d", sym.LineStart, sym.LineEnd)
	}
	abs := filepath.Join(filepath.FromSlash(repoRoot), filepath.FromSlash(sym.File))
	f, err := os.Open(abs)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", abs, err)
	}
	defer f.Close()

	totalRange := sym.LineEnd - sym.LineStart + 1
	wantLines := totalRange
	if !full {
		if maxLines < wantLines {
			wantLines = maxLines
		}
	}

	scanner := bufio.NewScanner(f)
	// Allow long lines (default Scanner limit is 64 KiB which is fine for
	// most code but trips on minified files); 1 MiB is generous.
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	var collected []string
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		if lineNo < sym.LineStart {
			continue
		}
		if lineNo > sym.LineEnd || len(collected) >= wantLines {
			break
		}
		collected = append(collected, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("read %s: %w", abs, err)
	}
	if len(collected) == 0 {
		return "", fmt.Errorf("no lines in range %d-%d (file has %d lines)", sym.LineStart, sym.LineEnd, lineNo)
	}

	out := strings.Join(collected, "\n")
	if !full && wantLines < totalRange {
		out += "\n…"
	}
	return out, nil
}

// readHintLine returns a single-line hint pointing to the partial-Read
// command that fetches the symbol's full body. It's emitted on every show
// invocation so agents always know the cheapest way to widen the view.
func readHintLine(sym core.Symbol) string {
	count := sym.LineEnd - sym.LineStart + 1
	if count < 1 {
		count = 1
	}
	return fmt.Sprintf("hint:     full body via Read %s offset=%d limit=%d (%d lines)",
		sym.File, sym.LineStart, count, count)
}
