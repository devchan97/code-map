package search

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/devchan97/code-map/internal/core"
	"github.com/devchan97/code-map/internal/store"
)

// Show returns the symbol identified by target.
//
// Target may be:
//   - a fully-qualified name (e.g. "app.module.func"), looked up by exact
//     qualname match — on ambiguous results the first by file:line is returned;
//   - a "FILE:LINE" pair (e.g. "app/module.py:42"), looked up by file path +
//     the symbol whose line range contains LINE.
func Show(ctx context.Context, st *store.Store, target string) (core.Symbol, error) {
	var result core.Symbol
	var found bool

	err := st.WithTx(ctx, func(tx store.Tx) error {
		// Detect FILE:LINE form: split by last ':', try to parse the right side
		// as an integer line number.
		if idx := strings.LastIndex(target, ":"); idx > 0 {
			maybeFile := target[:idx]
			maybeLineStr := target[idx+1:]
			if line, err := strconv.Atoi(maybeLineStr); err == nil && line > 0 {
				// Normalise path to forward slashes.
				normFile := filepath.ToSlash(maybeFile)
				syms, err := tx.SymbolsByFile(normFile)
				if err != nil {
					return fmt.Errorf("search.Show SymbolsByFile %q: %w", normFile, err)
				}

				// Find the narrowest containing range.
				bestWidth := -1
				for _, s := range syms {
					if s.LineStart <= line && line <= s.LineEnd {
						width := s.LineEnd - s.LineStart
						if !found || width < bestWidth {
							result = s
							found = true
							bestWidth = width
						}
					}
				}
				if !found {
					return fmt.Errorf("search.Show: no symbol at %s:%d: %w",
						normFile, line, core.ErrUsage)
				}
				return nil
			}
		}

		// Qualname form.
		syms, err := tx.SymbolsByQualname(target)
		if err != nil {
			return fmt.Errorf("search.Show SymbolsByQualname %q: %w", target, err)
		}
		if len(syms) == 0 {
			return fmt.Errorf("search.Show: symbol not found: %s: %w", target, core.ErrUsage)
		}
		result = syms[0]
		found = true
		return nil
	})
	if err != nil {
		return core.Symbol{}, err
	}
	return result, nil
}
