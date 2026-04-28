// Package walker enumerates indexable files honoring ignore rules and computing SHA-1 + language hints.
package walker

import (
	"os"
	"path/filepath"

	gitignore "github.com/sabhiram/go-gitignore"
)

// DefaultExcludeDirs lists directory names that are always excluded during
// walking, matched against the basename of every directory encountered.
var DefaultExcludeDirs = []string{
	".git",
	"node_modules",
	".venv",
	"venv",
	"__pycache__",
	"dist",
	"build",
	".codemap",
	"target",
	".next",
	".mypy_cache",
	".pytest_cache",
	".ruff_cache",
}

// IgnoreSet combines compiled gitignore rules from .gitignore,
// .codemapignore and any caller-supplied extra patterns.
type IgnoreSet struct {
	compiled *gitignore.GitIgnore
}

// LoadIgnore reads .gitignore and .codemapignore from root, merges them
// with extra patterns and returns a ready-to-use IgnoreSet.
// Missing ignore files are silently skipped (not an error).
func LoadIgnore(root string, extra []string) (*IgnoreSet, error) {
	var lines []string

	for _, name := range []string{".gitignore", ".codemapignore"} {
		path := filepath.Join(root, name)
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		lines = appendLines(lines, string(data))
	}

	lines = append(lines, extra...)

	compiled := gitignore.CompileIgnoreLines(lines...)
	return &IgnoreSet{compiled: compiled}, nil
}

// Match reports whether relPath should be ignored.
// relPath must use forward slashes and be relative to the repo root.
// isDir should be true when the path refers to a directory.
func (ig *IgnoreSet) Match(relPath string, isDir bool) bool {
	if ig == nil || ig.compiled == nil {
		return false
	}
	return ig.compiled.MatchesPath(relPath)
}

// appendLines splits raw text into individual non-empty, non-comment lines
// and appends them to dst.
func appendLines(dst []string, raw string) []string {
	start := 0
	for i := 0; i <= len(raw); i++ {
		if i == len(raw) || raw[i] == '\n' {
			line := raw[start:i]
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
			if line != "" && line[0] != '#' {
				dst = append(dst, line)
			}
			start = i + 1
		}
	}
	return dst
}
