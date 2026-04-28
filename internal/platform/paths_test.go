package platform

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestRepoCodemapDir_ForwardSlash(t *testing.T) {
	got := RepoCodemapDir("C:\\foo\\bar")
	if strings.Contains(got, "\\") {
		t.Errorf("RepoCodemapDir contains backslash: %q", got)
	}
	if !strings.HasSuffix(got, "/.codemap") {
		t.Errorf("RepoCodemapDir = %q; want suffix /.codemap", got)
	}
}

func TestToFromSlash(t *testing.T) {
	if ToSlash("a/b") != "a/b" {
		t.Errorf("ToSlash idempotent failed")
	}
	got := ToSlash("a\\b\\c")
	if got != "a/b/c" {
		t.Errorf("ToSlash = %q; want a/b/c", got)
	}
}

func TestRelFromRoot(t *testing.T) {
	root := t.TempDir()
	abs := filepath.Join(root, "sub", "file.go")
	rel, err := RelFromRoot(root, abs)
	if err != nil {
		t.Fatalf("RelFromRoot err: %v", err)
	}
	if rel != "sub/file.go" {
		t.Errorf("RelFromRoot = %q; want sub/file.go", rel)
	}
}

func TestRelFromRoot_Escape(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Dir(root)
	if _, err := RelFromRoot(root, filepath.Join(parent, "outside.go")); err == nil {
		t.Error("RelFromRoot expected error for path escaping the root")
	}
}

func TestRegistryPath_UnderCodemapHome(t *testing.T) {
	home := CodemapHome()
	if home == "" {
		t.Skip("user home not available")
	}
	if !strings.HasPrefix(RegistryPath(), home+"/") {
		t.Errorf("RegistryPath %q not under %q", RegistryPath(), home)
	}
	if !strings.HasSuffix(RegistryPath(), "/registry.toml") {
		t.Errorf("RegistryPath %q lacks expected suffix", RegistryPath())
	}
	if !strings.HasSuffix(StatePath(), "/state.toml") {
		t.Errorf("StatePath %q lacks expected suffix", StatePath())
	}
}
