package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestInstall_PrintMode verifies that print=true returns the rendered body
// without writing any file.
func TestInstall_PrintMode(t *testing.T) {
	out, err := Install(Target{
		Agent:      "claude-code",
		Scope:      "user",
		BinaryName: "codemap",
		Version:    "v0.0.0-test",
	}, true)
	if err != nil {
		t.Fatalf("Install print: %v", err)
	}
	if !strings.Contains(out, "v0.0.0-test") {
		t.Errorf("print output missing version; got: %.200s", out)
	}
	if !strings.Contains(out, "codemap") {
		t.Errorf("print output missing 'codemap'; got: %.200s", out)
	}
}

// TestInstall_WritesFileAndMatchesExpected installs into a temp repo and
// verifies: file exists, body contains version, no backslashes in returned path.
func TestInstall_WritesFileAndMatchesExpected(t *testing.T) {
	repo := t.TempDir()
	target := Target{
		Agent:      "claude-code",
		Scope:      "project",
		Repo:       repo,
		BinaryName: "codemap",
		Version:    "v0.1.0",
	}

	path, err := Install(target, false)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if strings.Contains(path, "\\") {
		t.Errorf("returned path not forward-slash normalized: %q", path)
	}

	got, err := os.ReadFile(filepath.FromSlash(path))
	if err != nil {
		t.Fatalf("read written file: %v", err)
	}
	if !strings.Contains(string(got), "v0.1.0") {
		t.Error("written file missing version")
	}
	if !strings.Contains(string(got), "codemap") {
		t.Error("written file missing 'codemap'")
	}
}

// TestInstall_AtomicWrite verifies that the atomic write leaves no .tmp file
// after a successful install.
func TestInstall_AtomicWrite(t *testing.T) {
	repo := t.TempDir()
	target := Target{
		Agent:      "claude-code",
		Scope:      "project",
		Repo:       repo,
		BinaryName: "codemap",
		Version:    "v0.0.1",
	}

	path, err := Install(target, false)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	nativePath := filepath.FromSlash(path)
	tmpPath := nativePath + ".tmp"
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Errorf("atomic write left behind .tmp file: %s", tmpPath)
	}
}

// TestUninstall_RemovesFile verifies the file is removed after uninstall.
func TestUninstall_RemovesFile(t *testing.T) {
	repo := t.TempDir()
	target := Target{
		Agent:      "claude-code",
		Scope:      "project",
		Repo:       repo,
		BinaryName: "codemap",
		Version:    "v0.1",
	}

	path, err := Install(target, false)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	if err := Uninstall(target); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if _, err := os.Stat(filepath.FromSlash(path)); !os.IsNotExist(err) {
		t.Errorf("expected file removed; stat err = %v", err)
	}
}

// TestUninstall_IdempotentWhenAbsent verifies uninstall on a missing file is a no-op.
func TestUninstall_IdempotentWhenAbsent(t *testing.T) {
	repo := t.TempDir()
	target := Target{
		Agent:      "claude-code",
		Scope:      "project",
		Repo:       repo,
		BinaryName: "codemap",
		Version:    "v0.1",
	}
	// Never installed — should not error.
	if err := Uninstall(target); err != nil {
		t.Errorf("Uninstall on absent file should be no-op; got: %v", err)
	}
}

// TestInstall_DefaultBinaryName verifies that empty BinaryName defaults to "codemap".
func TestInstall_DefaultBinaryName(t *testing.T) {
	repo := t.TempDir()
	target := Target{
		Agent:      "claude-code",
		Scope:      "project",
		Repo:       repo,
		BinaryName: "", // intentionally empty
		Version:    "v0.0.1",
	}
	path, err := Install(target, false)
	if err != nil {
		t.Fatalf("Install with empty BinaryName: %v", err)
	}
	got, err := os.ReadFile(filepath.FromSlash(path))
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if !strings.Contains(string(got), "codemap") {
		t.Error("body should contain default binary name 'codemap'")
	}
}
