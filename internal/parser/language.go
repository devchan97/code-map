// Package parser defines the tree-sitter parser registry for codemap's language adapters.
package parser

import "path/filepath"

// DetectLanguage maps a file path to a language tag using its extension.
// Returns "" for unknown extensions; the caller may use the fallback parser.
func DetectLanguage(path string) string {
	ext := filepath.Ext(path)
	switch ext {
	case ".py":
		return "python"
	case ".js", ".jsx", ".mjs", ".cjs":
		return "js"
	case ".ts":
		return "ts"
	case ".tsx":
		return "tsx"
	case ".java":
		return "java"
	case ".cs":
		return "csharp"
	case ".cpp", ".cc", ".cxx", ".c++", ".hpp", ".hxx":
		return "cpp"
	case ".go":
		return "go"
	case ".rs":
		return "rust"
	default:
		return ""
	}
}
