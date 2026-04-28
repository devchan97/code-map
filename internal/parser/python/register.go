// Package python registers the Python tree-sitter adapter with the codemap parser registry.
package python

import "github.com/devchan97/code-map/internal/parser"

func init() { parser.Register(New()) }
