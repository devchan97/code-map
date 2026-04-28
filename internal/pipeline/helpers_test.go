package pipeline

import "github.com/devchan97/code-map/internal/walker"

// makeEntries constructs walker.Entry slices from path/sha1 pairs for table-driven tests.
func makeEntries(in []struct{ Path, SHA1 string }) []walker.Entry {
	out := make([]walker.Entry, 0, len(in))
	for _, e := range in {
		out = append(out, walker.Entry{Path: e.Path, SHA1: e.SHA1})
	}
	return out
}
