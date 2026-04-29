//go:build !windows

package install

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// markerBegin/markerEnd wrap the block install-self appends to the
// user's shell rc file. The marker lets uninstall-self remove the
// block precisely without touching anything else the user added.
const (
	markerBegin = "# >>> codemap install-self >>>"
	markerEnd   = "# <<< codemap install-self <<<"
)

// addToPath appends an export line to the appropriate shell rc file
// if and only if the marker block is not already present. Returns
// (added, rcPath, err).
func addToPath(dir string) (bool, string, error) {
	rc, err := detectShellRC()
	if err != nil {
		return false, "", err
	}
	body, _ := os.ReadFile(rc)
	if strings.Contains(string(body), markerBegin) {
		return false, rc, nil
	}
	if err := os.MkdirAll(filepath.Dir(rc), 0o755); err != nil {
		return false, rc, err
	}
	block := fmt.Sprintf("\n%s\nexport PATH=\"%s:$PATH\"\n%s\n", markerBegin, dir, markerEnd)
	f, err := os.OpenFile(rc, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return false, rc, fmt.Errorf("open %s: %w", rc, err)
	}
	defer f.Close()
	if _, err := f.WriteString(block); err != nil {
		return false, rc, fmt.Errorf("write %s: %w", rc, err)
	}
	return true, rc, nil
}

// removeFromPath strips the marker block (and only that block) from
// the user's shell rc file.
func removeFromPath(dir string) (bool, string, error) {
	_ = dir // dir not needed: we identify by marker, not by literal path
	rc, err := detectShellRC()
	if err != nil {
		return false, "", err
	}
	body, err := os.ReadFile(rc)
	if errors.Is(err, os.ErrNotExist) {
		return false, rc, nil
	}
	if err != nil {
		return false, rc, fmt.Errorf("read %s: %w", rc, err)
	}
	stripped, removed := stripMarkerBlock(string(body))
	if !removed {
		return false, rc, nil
	}
	if err := os.WriteFile(rc, []byte(stripped), 0o644); err != nil {
		return false, rc, fmt.Errorf("write %s: %w", rc, err)
	}
	return true, rc, nil
}

func stripMarkerBlock(in string) (string, bool) {
	bi := strings.Index(in, markerBegin)
	if bi < 0 {
		return in, false
	}
	ei := strings.Index(in[bi:], markerEnd)
	if ei < 0 {
		return in, false
	}
	end := bi + ei + len(markerEnd)
	// Eat the trailing newline that the block was written with.
	if end < len(in) && in[end] == '\n' {
		end++
	}
	// Eat one preceding blank line for symmetry with how we wrote it.
	begin := bi
	if begin > 0 && in[begin-1] == '\n' {
		begin--
	}
	return in[:begin] + in[end:], true
}

// detectShellRC picks the rc file to modify based on $SHELL.
// Falls back to ~/.profile when the shell is unknown — that file is
// sourced by both bash and sh on most systems.
func detectShellRC() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home: %w", err)
	}
	shell := os.Getenv("SHELL")
	switch filepath.Base(shell) {
	case "bash":
		return filepath.Join(home, ".bashrc"), nil
	case "zsh":
		return filepath.Join(home, ".zshrc"), nil
	case "fish":
		return filepath.Join(home, ".config", "fish", "config.fish"), nil
	}
	return filepath.Join(home, ".profile"), nil
}

// pathContains reports whether dir is already present in a colon
// separated PATH string. Case-sensitive (Unix).
func pathContains(path, dir string) bool {
	for _, p := range strings.Split(path, ":") {
		if p == dir {
			return true
		}
	}
	return false
}
