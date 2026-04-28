package platform

import (
	"fmt"
	"os"
	"path/filepath"
)

// AtomicWrite writes data to path atomically by first writing to a
// temporary file alongside path, syncing to disk, then renaming it to
// the final destination. On any error the temporary file is removed.
func AtomicWrite(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := EnsureDir(dir); err != nil {
		return fmt.Errorf("AtomicWrite: ensure dir: %w", err)
	}

	tmp := path + ".tmp"

	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return fmt.Errorf("AtomicWrite: open tmp: %w", err)
	}

	// Write and sync; clean up tmp on any failure.
	if _, err = f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("AtomicWrite: write: %w", err)
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("AtomicWrite: sync: %w", err)
	}
	if err = f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("AtomicWrite: close: %w", err)
	}

	if err = os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("AtomicWrite: rename: %w", err)
	}
	return nil
}

// EnsureDir creates path and any necessary parents with permission 0o755.
func EnsureDir(path string) error {
	if err := os.MkdirAll(path, 0o755); err != nil {
		return fmt.Errorf("EnsureDir %q: %w", path, err)
	}
	return nil
}
