// Package fallback is a placeholder for the unknown-language whole-file adapter (M1 fallback).
// It emits a single whole-file Symbol for files whose language is not recognized by any registered parser.
package fallback

// TODO(M3): implement full fallback parser. Tracked in task.md §4.

import (
	"bytes"
	"path/filepath"
	"strings"

	"github.com/devchan97/code-map/internal/core"
)

// Parser emits a single whole-file Symbol for unknown languages.
// It is NOT registered at init — the pipeline picks it explicitly when parser.For(lang) returns false.
type Parser struct{}

// New returns a fallback Parser.
func New() *Parser { return &Parser{} }

// Language returns "fallback".
func (p *Parser) Language() string { return "fallback" }

// Parse emits one Symbol covering the entire file.
// The symbol has kind=variable, scope=global, name=filepath.Base(path),
// qualname=filepath.Base(path), and snippet=first 10 lines of src.
func (p *Parser) Parse(path string, src []byte) ([]core.Symbol, []core.Edge, error) {
	name := filepath.Base(path)
	lineCount := bytes.Count(src, []byte("\n"))
	if len(src) > 0 && src[len(src)-1] != '\n' {
		lineCount++
	}
	if lineCount == 0 {
		lineCount = 1
	}

	snippet := firstNLines(string(src), 10)

	sym := core.Symbol{
		Name:      name,
		Qualname:  name,
		Kind:      core.SymbolVariable,
		Scope:     core.ScopeGlobal,
		File:      path,
		LineStart: 1,
		LineEnd:   lineCount,
		Snippet:   snippet,
	}
	return []core.Symbol{sym}, nil, nil
}

// firstNLines returns the first n lines of s, joined by newlines.
func firstNLines(s string, n int) string {
	lines := strings.SplitN(s, "\n", n+1)
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}
