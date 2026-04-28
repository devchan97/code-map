package registry

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/devchan97/code-map/internal/core"
)

func writeIndexDB(t *testing.T, repoRoot string) {
	t.Helper()
	dir := filepath.Join(repoRoot, ".codemap")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.db"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResolve_Step1_FlagByName(t *testing.T) {
	reg := newTempRegistry(t)
	dir := t.TempDir()
	_ = reg.Upsert(core.RegistryEntry{Name: "myrepo", Path: dir, Embedder: "lexical", SchemaVer: 1})

	res, err := Resolve(ResolveInput{FlagRepo: "myrepo", Cwd: t.TempDir()}, reg, State{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Entry.Name != "myrepo" {
		t.Errorf("Entry = %+v", res.Entry)
	}
}

func TestResolve_Step1_FlagByPath(t *testing.T) {
	reg := newTempRegistry(t)
	dir := t.TempDir()
	_ = reg.Upsert(core.RegistryEntry{Name: "x", Path: dir, Embedder: "lexical", SchemaVer: 1})

	res, err := Resolve(ResolveInput{FlagRepo: dir, Cwd: t.TempDir()}, reg, State{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Entry.Name != "x" {
		t.Errorf("Entry = %+v", res.Entry)
	}
}

func TestResolve_Step1_FlagAutoRegister(t *testing.T) {
	reg := newTempRegistry(t)
	repo := t.TempDir()
	writeIndexDB(t, repo)

	res, err := Resolve(ResolveInput{FlagRepo: repo, Cwd: t.TempDir()}, reg, State{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Entry.Path == "" {
		t.Errorf("expected auto-registered entry; got %+v", res.Entry)
	}
	all, _ := reg.Load()
	if len(all) != 1 {
		t.Errorf("expected 1 registered entry; got %d", len(all))
	}
}

func TestResolve_Step1_FlagNotFound(t *testing.T) {
	reg := newTempRegistry(t)
	_, err := Resolve(ResolveInput{FlagRepo: "missing", Cwd: t.TempDir()}, reg, State{})
	if !errors.Is(err, core.ErrRepoNotFound) {
		t.Errorf("expected ErrRepoNotFound; got %v", err)
	}
}

func TestResolve_Step2_CwdInsideRegistered(t *testing.T) {
	reg := newTempRegistry(t)
	repo := t.TempDir()
	_ = reg.Upsert(core.RegistryEntry{Name: "r", Path: repo, Embedder: "lexical", SchemaVer: 1})

	sub := filepath.Join(repo, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	res, err := Resolve(ResolveInput{Cwd: sub}, reg, State{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Entry.Name != "r" {
		t.Errorf("Entry = %+v", res.Entry)
	}
}

func TestResolve_Step3_AutoRegisterFromCwd(t *testing.T) {
	reg := newTempRegistry(t)
	repo := t.TempDir()
	writeIndexDB(t, repo)
	res, err := Resolve(ResolveInput{Cwd: repo}, reg, State{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Entry.Path == "" {
		t.Errorf("expected auto-registered entry")
	}
}

func TestResolve_Step4_DefaultRepo(t *testing.T) {
	reg := newTempRegistry(t)
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	_ = reg.Upsert(core.RegistryEntry{Name: "a", Path: dir1, Embedder: "lexical", SchemaVer: 1})
	_ = reg.Upsert(core.RegistryEntry{Name: "b", Path: dir2, Embedder: "lexical", SchemaVer: 1})

	cwd := t.TempDir() // not under either repo
	res, err := Resolve(ResolveInput{Cwd: cwd}, reg, State{DefaultRepo: "b"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Entry.Name != "b" {
		t.Errorf("Entry = %+v", res.Entry)
	}
}

func TestResolve_Step5_SingleEntryNotice(t *testing.T) {
	reg := newTempRegistry(t)
	dir := t.TempDir()
	_ = reg.Upsert(core.RegistryEntry{Name: "only", Path: dir, Embedder: "lexical", SchemaVer: 1})

	cwd := t.TempDir()
	res, err := Resolve(ResolveInput{Cwd: cwd}, reg, State{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Entry.Name != "only" {
		t.Errorf("Entry = %+v", res.Entry)
	}
	if res.Notice != "using only registered repo: only" {
		t.Errorf("Notice = %q; want exact spec wording", res.Notice)
	}
}

func TestResolve_Step6_NotFound(t *testing.T) {
	reg := newTempRegistry(t)
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	_ = reg.Upsert(core.RegistryEntry{Name: "a", Path: dir1, Embedder: "lexical", SchemaVer: 1})
	_ = reg.Upsert(core.RegistryEntry{Name: "b", Path: dir2, Embedder: "lexical", SchemaVer: 1})

	_, err := Resolve(ResolveInput{Cwd: t.TempDir()}, reg, State{})
	if !errors.Is(err, core.ErrRepoNotFound) {
		t.Errorf("expected ErrRepoNotFound; got %v", err)
	}
}
