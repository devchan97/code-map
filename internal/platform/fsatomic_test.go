package platform

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicWrite_NewFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "out.txt")
	want := []byte("hello")
	if err := AtomicWrite(target, want, 0o644); err != nil {
		t.Fatalf("AtomicWrite: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAtomicWrite_Overwrite(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "out.txt")
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWrite(target, []byte("new"), 0o644); err != nil {
		t.Fatalf("AtomicWrite: %v", err)
	}
	got, _ := os.ReadFile(target)
	if string(got) != "new" {
		t.Errorf("got %q, want new", got)
	}
	// No leftover .tmp file.
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		names := []string{}
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("expected 1 file in dir, found %d: %v", len(entries), names)
	}
}

func TestEnsureDir(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "a", "b", "c")
	if err := EnsureDir(target); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	info, err := os.Stat(target)
	if err != nil || !info.IsDir() {
		t.Errorf("EnsureDir did not create directory %q (err=%v)", target, err)
	}
	// Idempotent.
	if err := EnsureDir(target); err != nil {
		t.Errorf("EnsureDir idempotent: %v", err)
	}
}
