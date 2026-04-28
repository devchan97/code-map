// Package platform abstracts OS-specific filesystem and terminal operations.
package platform

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// UserHome returns the current user's home directory.
func UserHome() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("UserHome: %w", err)
	}
	return home, nil
}

// CodemapHome returns the path to the global ~/.codemap directory,
// forward-slash normalized. Returns empty string if the home directory
// cannot be determined.
func CodemapHome() string {
	home, err := UserHome()
	if err != nil {
		return ""
	}
	return ToSlash(filepath.Join(home, ".codemap"))
}

// RegistryPath returns the path to the global registry TOML file,
// i.e. <CodemapHome>/registry.toml.
func RegistryPath() string {
	return CodemapHome() + "/registry.toml"
}

// StatePath returns the path to the global state TOML file,
// i.e. <CodemapHome>/state.toml.
func StatePath() string {
	return CodemapHome() + "/state.toml"
}

// RepoCodemapDir returns the path to the per-repo .codemap directory,
// i.e. <repoRoot>/.codemap, forward-slash normalized.
func RepoCodemapDir(repoRoot string) string {
	return ToSlash(filepath.Join(repoRoot, ".codemap"))
}

// ToSlash converts a path to use forward slashes. Unlike filepath.ToSlash —
// which is a no-op on POSIX hosts — this implementation always rewrites
// backslashes so callers can hand it OS-native paths from either platform
// and get a forward-slash result.
func ToSlash(p string) string {
	return strings.ReplaceAll(p, "\\", "/")
}

// FromSlash converts a forward-slash path to the OS-native separator.
func FromSlash(p string) string {
	return filepath.FromSlash(p)
}

// RelFromRoot returns a repo-relative path with forward slashes for abs,
// relative to repoRoot. Returns an error if abs is not under repoRoot.
func RelFromRoot(repoRoot, abs string) (string, error) {
	// Normalize both paths to clean, absolute form using OS separators
	// before computing the relative path.
	cleanRoot := filepath.Clean(repoRoot)
	cleanAbs := filepath.Clean(abs)

	rel, err := filepath.Rel(cleanRoot, cleanAbs)
	if err != nil {
		return "", fmt.Errorf("RelFromRoot: %w", err)
	}

	// Ensure the relative path does not escape the root.
	if strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("RelFromRoot: %q is not under %q", abs, repoRoot)
	}

	return ToSlash(rel), nil
}
