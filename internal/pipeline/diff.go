package pipeline

import (
	"sort"

	"github.com/devchan97/code-map/internal/core"
	"github.com/devchan97/code-map/internal/walker"
)

// ChangedFiles returns the subset of current whose SHA-1 differs from prev,
// plus the list of paths in prev that are absent from current (removed files).
//
// prev is the previous-snapshot rows from the files table.
// current is the freshly walked entries.
// Comparison is by Path (forward-slash normalised).
//
// Result order: changed follows the input order of current; removed is sorted
// lexicographically for determinism.
func ChangedFiles(prev []core.File, current []walker.Entry) (changed []walker.Entry, removed []string) {
	// Build lookup from path → previous File.
	prevByPath := make(map[string]core.File, len(prev))
	for _, f := range prev {
		prevByPath[f.Path] = f
	}

	// Walk current entries: collect changed (new or modified).
	seen := make(map[string]struct{}, len(current))
	for _, e := range current {
		seen[e.Path] = struct{}{}
		if prevByPath[e.Path].SHA1 != e.SHA1 {
			changed = append(changed, e)
		}
	}

	// Collect paths from prev that are no longer present.
	for _, f := range prev {
		if _, ok := seen[f.Path]; !ok {
			removed = append(removed, f.Path)
		}
	}
	sort.Strings(removed)

	return changed, removed
}
