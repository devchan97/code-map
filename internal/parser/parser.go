// Package parser defines the tree-sitter parser registry for codemap's language adapters.
package parser

import (
	"sort"
	"sync"

	"github.com/devchan97/code-map/internal/core"
)

// Parser is the language-specific adapter contract.
// Each language adapter implements this interface and registers itself via Register.
type Parser interface {
	// Language returns the canonical language tag (e.g. "python", "go", "ts", "rust").
	Language() string
	// Parse extracts symbols and edges from the given source file.
	// path is repo-relative and slash-separated. src is the raw file bytes.
	Parse(path string, src []byte) (symbols []core.Symbol, edges []core.Edge, err error)
}

var (
	mu       sync.RWMutex
	registry = map[string]Parser{}
)

// Register adds a parser to the global registry.
// If a parser for the same language is already registered it is replaced (idempotent).
func Register(p Parser) {
	mu.Lock()
	defer mu.Unlock()
	registry[p.Language()] = p
}

// For returns the registered parser for the given language tag, or (nil, false)
// if no parser is registered for that language.
func For(language string) (Parser, bool) {
	mu.RLock()
	defer mu.RUnlock()
	p, ok := registry[language]
	return p, ok
}

// Languages returns a sorted slice of language tags for all registered parsers.
func Languages() []string {
	mu.RLock()
	defer mu.RUnlock()
	tags := make([]string, 0, len(registry))
	for lang := range registry {
		tags = append(tags, lang)
	}
	sort.Strings(tags)
	return tags
}
