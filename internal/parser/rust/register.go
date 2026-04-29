// Package rust registers the Rust tree-sitter adapter with the codemap parser registry.
package rust

import "github.com/devchan97/code-map/internal/parser"

func init() { parser.Register(New()) }
