package main

// This file exists so that `go test ./cmd/codemap/...` exercises the package's
// compilation. The actual binary is exercised end-to-end via the pipeline and
// search/graph integration tests in their respective packages.

import (
	"testing"

	"github.com/devchan97/code-map/internal/parser"
)

func TestPackageCompiles(t *testing.T) {
	// If this test runs at all, the package compiled.
}

// TestRequiredParsersRegistered guards against the silent-fallback regression where
// a parser package is dropped from main.go's blank-import list (or its CGO build
// breaks). Without this check, a missing parser falls through to the whole-file
// adapter and produces 1 symbol per file with 0 edges — looks like the parser
// "works poorly" but is actually not loaded at all.
func TestRequiredParsersRegistered(t *testing.T) {
	required := []string{"python", "ts", "java", "csharp", "cpp", "go", "rust"}
	for _, lang := range required {
		if _, ok := parser.For(lang); !ok {
			t.Errorf("parser for %q not registered — check cmd/codemap/main.go blank imports and CGO build", lang)
		}
	}
}
