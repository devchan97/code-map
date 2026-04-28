// Package lexical provides tokenization, BM25 scoring, and the TokenStore
// interface for codemap's lexical retrieval layer.
package lexical

import (
	"strings"
	"unicode"

	"github.com/devchan97/code-map/internal/core"
)

// Token is one inverted-index posting derived from a Symbol's text fields.
type Token struct {
	Text   string  // lowercased
	Field  string  // "name" | "qualname" | "docstring" | "snippet"
	Weight float32 // field-level weight applied at scoring time
}

// FieldWeights holds the default per-field BM25 weights.
var FieldWeights = map[string]float32{
	"name":      4.0,
	"qualname":  3.0,
	"docstring": 1.0,
	"snippet":   0.5,
}

// Tokenize produces inverted-index tokens for the given Symbol.
// Tokens include the original full identifier and all subwords from
// CamelCase / snake_case decomposition. All tokens are lowercased.
// Stop-word filtering is NOT applied (caller may filter at query time).
func Tokenize(s core.Symbol) []Token {
	var toks []Token

	// field="name"
	if s.Name != "" {
		for _, t := range SplitIdentifier(s.Name) {
			toks = append(toks, Token{Text: t, Field: "name", Weight: FieldWeights["name"]})
		}
	}

	// field="qualname" — split on dots, then SplitIdentifier each segment
	if s.Qualname != "" {
		seen := make(map[string]bool)
		for _, seg := range strings.Split(s.Qualname, ".") {
			if seg == "" {
				continue
			}
			for _, t := range SplitIdentifier(seg) {
				if !seen[t] {
					seen[t] = true
					toks = append(toks, Token{Text: t, Field: "qualname", Weight: FieldWeights["qualname"]})
				}
			}
		}
	}

	// field="docstring"
	if s.Docstring != "" {
		for _, t := range splitText(s.Docstring) {
			toks = append(toks, Token{Text: t, Field: "docstring", Weight: FieldWeights["docstring"]})
		}
	}

	// field="snippet"
	if s.Snippet != "" {
		for _, t := range splitText(s.Snippet) {
			toks = append(toks, Token{Text: t, Field: "snippet", Weight: FieldWeights["snippet"]})
		}
	}

	return toks
}

// SplitIdentifier returns the lowercased subwords of an identifier,
// splitting on snake_case and CamelCase boundaries. The original
// (lowercased) identifier is included if it differs from any subword.
//
//	"getUserById"  → ["getuserbyid", "get", "user", "by", "id"]
//	"snake_case_var" → ["snake_case_var", "snake", "case", "var"]
//	"HTMLParser"   → ["htmlparser", "html", "parser"]
func SplitIdentifier(id string) []string {
	if id == "" {
		return nil
	}

	lower := strings.ToLower(id)

	// Split on underscores first, then apply CamelCase splitting to each piece.
	snakeParts := strings.Split(id, "_")
	var subwords []string
	for _, part := range snakeParts {
		if part == "" {
			continue
		}
		for _, sub := range splitCamel(part) {
			if sub == "" {
				continue
			}
			subwords = append(subwords, strings.ToLower(sub))
		}
	}

	// Deduplicate subwords while preserving order.
	seen := make(map[string]bool)
	var deduped []string
	for _, w := range subwords {
		if !seen[w] {
			seen[w] = true
			deduped = append(deduped, w)
		}
	}

	// Filter: drop pieces of length < 2, EXCEPT when the entire identifier is a
	// single character (in that case keep it).
	var filtered []string
	isSingleChar := len([]rune(lower)) == 1
	for _, w := range deduped {
		if len([]rune(w)) >= 2 || isSingleChar {
			filtered = append(filtered, w)
		}
	}

	// Prepend the full lowercased identifier if it is not already present in the
	// filtered subword list.
	alreadyPresent := false
	for _, w := range filtered {
		if w == lower {
			alreadyPresent = true
			break
		}
	}
	if !alreadyPresent {
		// Only prepend when there is something meaningful to return.
		if len(filtered) > 0 || isSingleChar {
			filtered = append([]string{lower}, filtered...)
		} else if isSingleChar {
			filtered = []string{lower}
		} else {
			// identifier produced no subwords after filtering — return it alone if ≥ 2 chars.
			if len([]rune(lower)) >= 2 {
				filtered = []string{lower}
			} else {
				filtered = []string{lower} // single char: keep per the exception rule
			}
		}
	}

	return filtered
}

// TokenizeQuery splits a free-text query into the same tokens used at index
// time, preserving order. Used by BM25 search.
func TokenizeQuery(q string) []string {
	if q == "" {
		return nil
	}
	// Split on whitespace and punctuation that is not part of an identifier.
	raw := splitTextRaw(q)
	var result []string
	for _, tok := range raw {
		for _, sub := range SplitIdentifier(tok) {
			result = append(result, sub)
		}
	}
	return result
}

// splitCamel splits a single word (no underscores) on CamelCase boundaries.
// Boundary rules:
//   - [a-z] → [A-Z]           e.g. getUserById → get|User|By|Id
//   - [A-Z]+ → [A-Z][a-z]     e.g. HTMLParser → HTML|Parser
//   - letter → digit           e.g. func1 → func|1
//   - digit → letter           e.g. 1func → 1|func
func splitCamel(s string) []string {
	if s == "" {
		return nil
	}
	runes := []rune(s)
	n := len(runes)
	var parts []string
	start := 0

	for i := 1; i < n; i++ {
		cur := runes[i]
		prev := runes[i-1]

		// [a-z] → [A-Z]
		lowerToUpper := unicode.IsLower(prev) && unicode.IsUpper(cur)

		// [A-Z]+ → [A-Z][a-z]: e.g. in "HTMLParser" when prev='L', cur='P', next='a'
		// Actually the boundary is between the last uppercase of a run and the next uppercase
		// followed by lower, i.e. between i-2 and i-1 when runes[i-1] is upper and runes[i] is upper
		// and runes[i+1] is lower. We shift the boundary to sit before runes[i-1].
		upperRunBeforeLower := false
		if i+1 < n && unicode.IsUpper(prev) && unicode.IsUpper(cur) && unicode.IsLower(runes[i+1]) {
			// boundary goes between prev and cur
			upperRunBeforeLower = true
		}

		// letter → digit
		letterToDigit := unicode.IsLetter(prev) && unicode.IsDigit(cur)

		// digit → letter
		digitToLetter := unicode.IsDigit(prev) && unicode.IsLetter(cur)

		if lowerToUpper || upperRunBeforeLower || letterToDigit || digitToLetter {
			parts = append(parts, string(runes[start:i]))
			start = i
		}
	}
	parts = append(parts, string(runes[start:]))
	return parts
}

// splitText splits prose text (docstrings, snippets) on whitespace and
// punctuation, lowercases tokens, and drops tokens shorter than 2 runes.
func splitText(text string) []string {
	raw := splitTextRaw(text)
	var result []string
	for _, w := range raw {
		lw := strings.ToLower(w)
		if len([]rune(lw)) >= 2 {
			result = append(result, lw)
		}
	}
	return result
}

// splitTextRaw splits text on non-identifier characters (whitespace +
// punctuation that is not `_`). Returns raw (unmodified case) tokens.
func splitTextRaw(text string) []string {
	return strings.FieldsFunc(text, func(r rune) bool {
		// Keep letters, digits, and underscore as identifier characters.
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_'
	})
}
