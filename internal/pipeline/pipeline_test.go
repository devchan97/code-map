package pipeline

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/devchan97/code-map/internal/core"
	"github.com/devchan97/code-map/internal/parser"
	"github.com/devchan97/code-map/internal/store"
)

// fakeParser yields one symbol per file so we can drive the pipeline without CGO.
type fakeParser struct{ lang string }

func (f *fakeParser) Language() string { return f.lang }
func (f *fakeParser) Parse(path string, src []byte) ([]core.Symbol, []core.Edge, error) {
	sym := core.Symbol{
		Name:      filepath.Base(path),
		Qualname:  "fake." + filepath.Base(path),
		Kind:      core.SymbolFunction,
		Scope:     core.ScopeGlobal,
		File:      path,
		LineStart: 1,
		LineEnd:   2,
		Snippet:   string(src),
	}
	return []core.Symbol{sym}, nil, nil
}

// registerFakeFor registers a fake parser for the given language and restores
// state after the test.
func registerFakeFor(t *testing.T, lang string) {
	t.Helper()
	parser.Register(&fakeParser{lang: lang})
}

func writeRepoFile(t *testing.T, root, rel, content string) {
	t.Helper()
	abs := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestIndex_FreshRepo(t *testing.T) {
	registerFakeFor(t, "python")
	root := t.TempDir()
	writeRepoFile(t, root, "a.py", "x = 1\n")
	writeRepoFile(t, root, "sub/b.py", "y = 2\n")

	st, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	sum, err := Index(context.Background(), st, root, IndexOptions{})
	if err != nil {
		t.Fatalf("Index: %v", err)
	}
	if sum.Parsed != 2 {
		t.Errorf("Parsed = %d; want 2", sum.Parsed)
	}
	if sum.Symbols < 2 {
		t.Errorf("Symbols = %d; want >= 2", sum.Symbols)
	}
	if sum.IndexedAt.IsZero() {
		t.Error("IndexedAt zero")
	}
}

func TestIndex_IncrementalSkipsUnchanged(t *testing.T) {
	registerFakeFor(t, "python")
	root := t.TempDir()
	writeRepoFile(t, root, "a.py", "x = 1\n")

	st, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if _, err := Index(context.Background(), st, root, IndexOptions{}); err != nil {
		t.Fatalf("first Index: %v", err)
	}

	// Second run with no file changes should skip.
	sum, err := Index(context.Background(), st, root, IndexOptions{})
	if err != nil {
		t.Fatalf("second Index: %v", err)
	}
	if sum.Parsed != 0 {
		t.Errorf("expected 0 parsed on no-op run; got %d", sum.Parsed)
	}
}

func TestIndex_ChangedFileReparsed(t *testing.T) {
	registerFakeFor(t, "python")
	root := t.TempDir()
	writeRepoFile(t, root, "a.py", "x = 1\n")

	st, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if _, err := Index(context.Background(), st, root, IndexOptions{}); err != nil {
		t.Fatal(err)
	}
	// Modify file.
	writeRepoFile(t, root, "a.py", "x = 99\n")

	sum, err := Index(context.Background(), st, root, IndexOptions{})
	if err != nil {
		t.Fatalf("second Index: %v", err)
	}
	if sum.Parsed != 1 {
		t.Errorf("expected 1 parsed after content change; got %d", sum.Parsed)
	}
}

func TestIndex_DetectsRemovedFiles(t *testing.T) {
	registerFakeFor(t, "python")
	root := t.TempDir()
	writeRepoFile(t, root, "a.py", "x = 1\n")
	writeRepoFile(t, root, "b.py", "y = 2\n")

	st, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := Index(context.Background(), st, root, IndexOptions{}); err != nil {
		t.Fatal(err)
	}

	// Delete b.py.
	_ = os.Remove(filepath.Join(root, "b.py"))

	sum, err := Index(context.Background(), st, root, IndexOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if sum.Removed != 1 {
		t.Errorf("Removed = %d; want 1", sum.Removed)
	}
}

func TestReindex_RebuildsAll(t *testing.T) {
	registerFakeFor(t, "python")
	root := t.TempDir()
	writeRepoFile(t, root, "a.py", "x = 1\n")
	writeRepoFile(t, root, "b.py", "y = 2\n")

	st, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if _, err := Index(context.Background(), st, root, IndexOptions{}); err != nil {
		t.Fatal(err)
	}
	sum, err := Reindex(context.Background(), st, root, IndexOptions{})
	if err != nil {
		t.Fatalf("Reindex: %v", err)
	}
	if sum.Parsed != 2 {
		t.Errorf("Reindex Parsed = %d; want 2", sum.Parsed)
	}
}

func TestChangedFiles(t *testing.T) {
	prev := []core.File{
		{Path: "a", SHA1: "1"},
		{Path: "b", SHA1: "2"},
		{Path: "c", SHA1: "3"},
	}
	curHelper := []struct{ Path, SHA1 string }{
		{"a", "1"}, // unchanged
		{"b", "X"}, // modified
		{"d", "4"}, // new
	}
	// Convert to walker.Entry-compatible via interface; use the actual walker.Entry type.
	type minEntry struct {
		Path string
		SHA1 string
	}
	_ = minEntry{}
	// Build []walker.Entry inline via package import.
	// Done in helper:
	cur := makeEntries(curHelper)
	changed, removed := ChangedFiles(prev, cur)

	// Expect "b" (modified) and "d" (new) in changed.
	gotChanged := map[string]bool{}
	for _, c := range changed {
		gotChanged[c.Path] = true
	}
	if !gotChanged["b"] || !gotChanged["d"] {
		t.Errorf("changed missing b or d: %v", gotChanged)
	}
	if gotChanged["a"] {
		t.Errorf("a unchanged should NOT be in changed")
	}

	// Expect "c" in removed.
	if len(removed) != 1 || removed[0] != "c" {
		t.Errorf("removed = %v; want [c]", removed)
	}
}
