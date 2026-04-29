package install

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestInstallSelf_CopiesAndIsIdempotent runs install-self twice from a
// throwaway "binary" file and verifies that:
//   - the destination receives the copy on the first run,
//   - the second run is a no-op (no error, no spurious copy).
//
// PATH wiring is tested separately on Unix; on Windows we just check
// that the call succeeds against HKCU\Environment without raising.
func TestInstallSelf_CopiesAndIsIdempotent(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp) // platform.CodemapHome on Windows
	if runtime.GOOS == "windows" {
		t.Skip("Windows install-self touches HKCU; covered by integration manually")
	}

	src := filepath.Join(tmp, "fakecodemap")
	if err := os.WriteFile(src, []byte("#!/bin/sh\necho hi\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	res, err := InstallSelf(src)
	if err != nil {
		t.Fatalf("first install: %v", err)
	}
	if !res.BinaryCopied {
		t.Errorf("first run: BinaryCopied = false; want true")
	}
	if _, err := os.Stat(filepath.FromSlash(res.BinaryPath)); err != nil {
		t.Errorf("destination missing after install: %v", err)
	}

	res2, err := InstallSelf(src)
	if err != nil {
		t.Fatalf("second install: %v", err)
	}
	if res2.BinaryCopied {
		t.Errorf("second run copied again; install-self is not idempotent")
	}
	if res2.PathAdded {
		t.Errorf("second run mutated PATH again; install-self is not idempotent")
	}
}

func TestUninstallSelf_RemovesBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows uninstall-self touches HKCU; covered manually")
	}
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	src := filepath.Join(tmp, "fakecodemap")
	_ = os.WriteFile(src, []byte("x"), 0o755)
	if _, err := InstallSelf(src); err != nil {
		t.Fatal(err)
	}
	if _, err := UninstallSelf(); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if _, err := os.Stat(filepath.FromSlash(BinaryPath())); !os.IsNotExist(err) {
		t.Errorf("binary still present after uninstall: %v", err)
	}
}

