package skill

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/devchan97/code-map/internal/platform"
)

// Install writes (or prints) the rendered SKILL.md for the target.
// If print is true, the rendered body is returned and no disk writes occur.
// Otherwise the file is written atomically via platform.AtomicWrite and the
// absolute path of the written file is returned.
// BinaryName defaults to "codemap" when t.BinaryName is empty.
func Install(t Target, print bool) (string, error) {
	name := t.BinaryName
	if name == "" {
		name = "codemap"
	}

	body, err := Render(name, t.Version)
	if err != nil {
		return "", fmt.Errorf("skill: install: %w", err)
	}

	if print {
		return body, nil
	}

	path, err := ResolvePath(t)
	if err != nil {
		return "", fmt.Errorf("skill: install: %w", err)
	}

	dir := platform.ToSlash(filepath.Dir(platform.FromSlash(path)))
	if err := platform.EnsureDir(platform.FromSlash(dir)); err != nil {
		return "", fmt.Errorf("skill: install: ensure dir: %w", err)
	}

	if err := platform.AtomicWrite(platform.FromSlash(path), []byte(body), 0o644); err != nil {
		return "", fmt.Errorf("skill: install: write: %w", err)
	}

	return path, nil
}

// Uninstall removes the SKILL.md file for the target. Returns nil if the
// file does not exist.
func Uninstall(t Target) error {
	path, err := ResolvePath(t)
	if err != nil {
		return fmt.Errorf("skill: uninstall: %w", err)
	}

	if err := os.Remove(platform.FromSlash(path)); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("skill: uninstall: %w", err)
	}

	return nil
}
