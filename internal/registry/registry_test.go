package registry

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/devchan97/code-map/internal/core"
)

func newTempRegistry(t *testing.T) *FileRegistry {
	t.Helper()
	dir := t.TempDir()
	return NewFileRegistry(filepath.Join(dir, "registry.toml"))
}

func TestFileRegistry_LoadEmpty(t *testing.T) {
	reg := newTempRegistry(t)
	got, err := reg.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got == nil {
		t.Error("Load on missing file should return non-nil empty slice")
	}
	if len(got) != 0 {
		t.Errorf("expected 0 entries, got %d", len(got))
	}
}

func TestFileRegistry_UpsertGetRemove(t *testing.T) {
	reg := newTempRegistry(t)
	repoDir := t.TempDir()

	e := core.RegistryEntry{
		Name:        "p1",
		Path:        repoDir,
		CreatedAt:   time.Now().UTC().Truncate(time.Second),
		LastIndexed: time.Now().UTC().Truncate(time.Second),
		FileCount:   3,
		SymbolCount: 12,
		Embedder:    "lexical",
		SchemaVer:   1,
	}
	if err := reg.Upsert(e); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, found, err := reg.Get("p1")
	if err != nil || !found {
		t.Fatalf("Get: found=%v err=%v", found, err)
	}
	if got.Name != "p1" || got.FileCount != 3 {
		t.Errorf("got = %+v", got)
	}

	// Upsert with same path replaces (not duplicates).
	e2 := e
	e2.FileCount = 99
	if err := reg.Upsert(e2); err != nil {
		t.Fatal(err)
	}
	all, _ := reg.Load()
	if len(all) != 1 {
		t.Errorf("expected 1 entry after upsert-by-path, got %d (%+v)", len(all), all)
	}
	if all[0].FileCount != 99 {
		t.Errorf("FileCount = %d; want 99", all[0].FileCount)
	}

	// Remove by name.
	if err := reg.Remove("p1"); err != nil {
		t.Fatal(err)
	}
	all, _ = reg.Load()
	if len(all) != 0 {
		t.Errorf("expected empty after Remove; got %d", len(all))
	}

	// Remove missing is not an error.
	if err := reg.Remove("nope"); err != nil {
		t.Errorf("Remove missing should not error; got %v", err)
	}
}

func TestFileRegistry_SortedByName(t *testing.T) {
	reg := newTempRegistry(t)
	for _, name := range []string{"zeta", "alpha", "mu"} {
		dir := t.TempDir()
		_ = reg.Upsert(core.RegistryEntry{Name: name, Path: dir, Embedder: "lexical", SchemaVer: 1})
	}
	got, err := reg.Load()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"alpha", "mu", "zeta"}
	for i, e := range got {
		if e.Name != want[i] {
			t.Errorf("idx %d: got %q want %q (full=%v)", i, e.Name, want[i], got)
		}
	}
}

func TestFileRegistry_RemoveByPath(t *testing.T) {
	reg := newTempRegistry(t)
	dir := t.TempDir()
	_ = reg.Upsert(core.RegistryEntry{Name: "n", Path: dir, Embedder: "lexical", SchemaVer: 1})
	if err := reg.Remove(dir); err != nil {
		t.Fatal(err)
	}
	all, _ := reg.Load()
	if len(all) != 0 {
		t.Errorf("expected removed by path; got %v", all)
	}
}
