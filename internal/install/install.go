// Package install implements `codemap install-self` and its inverse.
//
// install-self copies the running codemap binary to a stable location
// inside the user's home directory and ensures that location is on PATH
// so the user can run `codemap` from any shell without manually managing
// downloads or environment variables.
//
// The destination is <CodemapHome>/bin (i.e. ~/.codemap/bin), keeping
// codemap's footprint in a single tree the user already knows about.
//
// PATH wiring is platform-specific:
//   - Windows: HKCU\Environment\Path is updated and a WM_SETTINGCHANGE
//     broadcast notifies running processes. No admin rights required.
//   - Unix: a marker block is appended to the user's shell rc file
//     (~/.bashrc, ~/.zshrc, etc.). The marker is preserved so
//     uninstall-self can remove the block cleanly.
//
// Both operations are idempotent: re-running install-self is a no-op
// once the binary is in place and PATH already contains the target
// directory.
package install

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/devchan97/code-map/internal/platform"
)

// BinDir returns the absolute path of the directory codemap installs
// itself into: <CodemapHome>/bin (forward-slash normalised).
func BinDir() string {
	return platform.CodemapHome() + "/bin"
}

// BinaryName is the platform-appropriate basename of the installed
// binary ("codemap" on Unix, "codemap.exe" on Windows).
func BinaryName() string {
	if runtime.GOOS == "windows" {
		return "codemap.exe"
	}
	return "codemap"
}

// BinaryPath returns the full path to the installed binary.
func BinaryPath() string {
	return BinDir() + "/" + BinaryName()
}

// Result describes what InstallSelf did. It is returned even on no-op
// runs so callers can print an honest summary.
type Result struct {
	// BinaryPath is the destination path that now holds the codemap
	// binary (whether or not a copy actually happened this run).
	BinaryPath string
	// BinaryCopied is true when the binary was newly written or
	// overwritten because the running process and the destination
	// were not the same file.
	BinaryCopied bool
	// PathAdded is true when this run made a change to the PATH
	// environment (registry entry on Windows, rc-file block on Unix).
	PathAdded bool
	// ShellRCPath is the path of the rc file modified on Unix, or
	// empty on Windows / when no rc file change was needed.
	ShellRCPath string
	// PathAlreadyEffective is true when BinDir() is already present
	// in the running process's PATH; in that case the user does not
	// need to open a new shell to use the installed binary.
	PathAlreadyEffective bool
}

// InstallSelf copies the running binary to BinaryPath() and ensures
// BinDir() is on the user's persistent PATH. selfPath should be the
// absolute path to the currently running executable (os.Executable).
func InstallSelf(selfPath string) (Result, error) {
	res := Result{BinaryPath: BinaryPath()}

	if err := platform.EnsureDir(BinDir()); err != nil {
		return res, fmt.Errorf("install: ensure bin dir: %w", err)
	}

	dest := platform.FromSlash(BinaryPath())
	src := platform.FromSlash(selfPath)
	srcAbs, err := filepath.Abs(src)
	if err != nil {
		return res, fmt.Errorf("install: resolve self path: %w", err)
	}
	destAbs, _ := filepath.Abs(dest)

	// Skip the copy when src and dest are the same inode (re-run from
	// the installed location) OR when an existing dest already matches
	// src in size and mtime. The latter keeps `install-self` idempotent
	// when the user runs it twice from the same downloaded zip without
	// reading the entire binary back to compare hashes. A real version
	// upgrade rewrites the file with a new mtime/size, which triggers
	// a fresh copy.
	if !sameFile(srcAbs, destAbs) && !destMatchesSrc(srcAbs, destAbs) {
		if err := copyExecutable(srcAbs, destAbs); err != nil {
			return res, fmt.Errorf("install: copy binary: %w", err)
		}
		res.BinaryCopied = true
	}

	added, rcPath, err := addToPath(BinDir())
	if err != nil {
		return res, fmt.Errorf("install: add to PATH: %w", err)
	}
	res.PathAdded = added
	res.ShellRCPath = rcPath
	res.PathAlreadyEffective = pathContains(os.Getenv("PATH"), BinDir())

	return res, nil
}

// UninstallSelf removes the installed binary, the bin directory if
// empty, and any PATH entries that install-self created.
func UninstallSelf() (Result, error) {
	res := Result{BinaryPath: BinaryPath()}

	dest := platform.FromSlash(BinaryPath())
	if _, err := os.Stat(dest); err == nil {
		if err := os.Remove(dest); err != nil {
			return res, fmt.Errorf("uninstall: remove binary: %w", err)
		}
		res.BinaryCopied = true // reuse the field as "binary touched"
	}
	// Best-effort: remove bin dir if it's empty after.
	_ = os.Remove(platform.FromSlash(BinDir()))

	removed, rcPath, err := removeFromPath(BinDir())
	if err != nil {
		return res, fmt.Errorf("uninstall: remove from PATH: %w", err)
	}
	res.PathAdded = removed // "PATH was changed"
	res.ShellRCPath = rcPath
	return res, nil
}

// sameFile is true when both paths resolve to the same file on disk.
func sameFile(a, b string) bool {
	ai, errA := os.Stat(a)
	bi, errB := os.Stat(b)
	if errA != nil || errB != nil {
		return false
	}
	return os.SameFile(ai, bi)
}

// destMatchesSrc reports whether dest exists and has the same Size and
// ModTime as src — a cheap heuristic that keeps install-self idempotent
// without re-reading the entire binary. A real version upgrade rewrites
// the file with a new ModTime, so the comparison stays correct in
// practice.
func destMatchesSrc(src, dest string) bool {
	si, errS := os.Stat(src)
	di, errD := os.Stat(dest)
	if errS != nil || errD != nil {
		return false
	}
	return si.Size() == di.Size() && si.ModTime().Equal(di.ModTime())
}

// copyExecutable atomically replaces dest with a copy of src and
// preserves the executable bit and source mtime on Unix. Carrying the
// mtime over makes destMatchesSrc cheap: a second install-self from
// the same source file is a no-op without hashing the binary.
func copyExecutable(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	srcInfo, err := in.Stat()
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(dest), ".codemap-install-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }

	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		cleanup()
		return err
	}
	if err := os.Chtimes(tmpPath, srcInfo.ModTime(), srcInfo.ModTime()); err != nil {
		cleanup()
		return err
	}
	// Windows can't rename onto an existing executable that is currently
	// running; we tolerate that by removing the destination first when
	// possible. Most of the time install-self runs from a downloaded zip
	// (not from BinaryPath) so the destination is either absent or stale.
	if runtime.GOOS == "windows" {
		_ = os.Remove(dest)
	}
	if err := os.Rename(tmpPath, dest); err != nil {
		cleanup()
		return err
	}
	return nil
}
