package main

// This file exists so that `go test ./cmd/codemap/...` exercises the package's
// compilation. The actual binary is exercised end-to-end via the pipeline and
// search/graph integration tests in their respective packages.

import "testing"

func TestPackageCompiles(t *testing.T) {
	// If this test runs at all, the package compiled.
}
