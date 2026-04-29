// Package golang registers the Go tree-sitter adapter with the codemap parser registry.
package golang

import "github.com/devchan97/code-map/internal/parser"

func init() { parser.Register(New()) }
