package lexical

import (
	"testing"

	"github.com/devchan97/code-map/internal/core"
)

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

func TestSplitIdentifier_CamelCase(t *testing.T) {
	got := SplitIdentifier("getUserById")
	for _, want := range []string{"getuserbyid", "get", "user", "by", "id"} {
		if !contains(got, want) {
			t.Errorf("SplitIdentifier(getUserById) missing %q; got %v", want, got)
		}
	}
}

func TestSplitIdentifier_SnakeCase(t *testing.T) {
	got := SplitIdentifier("snake_case_var")
	for _, want := range []string{"snake_case_var", "snake", "case", "var"} {
		if !contains(got, want) {
			t.Errorf("SplitIdentifier(snake_case_var) missing %q; got %v", want, got)
		}
	}
}

func TestSplitIdentifier_AcronymRun(t *testing.T) {
	got := SplitIdentifier("HTMLParser")
	for _, want := range []string{"htmlparser", "html", "parser"} {
		if !contains(got, want) {
			t.Errorf("SplitIdentifier(HTMLParser) missing %q; got %v", want, got)
		}
	}
}

func TestSplitIdentifier_LetterDigit(t *testing.T) {
	got := SplitIdentifier("func1Bar")
	for _, want := range []string{"func", "bar"} {
		if !contains(got, want) {
			t.Errorf("SplitIdentifier(func1Bar) missing %q; got %v", want, got)
		}
	}
}

func TestSplitIdentifier_DropsLengthOne(t *testing.T) {
	// `aB` produces [a, b] both length 1; spec says drop length<2 unless whole id is a single letter.
	got := SplitIdentifier("aB")
	for _, w := range got {
		if len(w) == 1 && w != "ab" { // "ab" is the lowercased full id; length 2, kept
			t.Errorf("SplitIdentifier(aB) yielded length-1 token %q; got %v", w, got)
		}
	}
	if !contains(got, "ab") {
		t.Errorf("SplitIdentifier(aB) should keep full lowered identifier 'ab'; got %v", got)
	}
}

func TestSplitIdentifier_SingleLetter(t *testing.T) {
	got := SplitIdentifier("x")
	if !contains(got, "x") {
		t.Errorf("SplitIdentifier(x) should keep 'x' (single-letter exception); got %v", got)
	}
}

func TestSplitIdentifier_Empty(t *testing.T) {
	if got := SplitIdentifier(""); got != nil {
		t.Errorf("SplitIdentifier(\"\") = %v; want nil", got)
	}
}

func TestTokenize_Symbol(t *testing.T) {
	s := core.Symbol{
		Name:      "applyRateLimit",
		Qualname:  "api.middleware.applyRateLimit",
		Docstring: "Applies the rate limiter",
		Snippet:   "def applyRateLimit(req): pass",
	}
	toks := Tokenize(s)
	fields := map[string]bool{}
	texts := []string{}
	for _, tk := range toks {
		fields[tk.Field] = true
		texts = append(texts, tk.Text)
	}
	for _, f := range []string{"name", "qualname", "docstring", "snippet"} {
		if !fields[f] {
			t.Errorf("Tokenize did not produce any tokens with field=%q", f)
		}
	}
	for _, want := range []string{"apply", "rate", "limit", "applyratelimit"} {
		if !contains(texts, want) {
			t.Errorf("Tokenize missing expected subword %q; texts=%v", want, texts)
		}
	}
}

func TestTokenize_FieldWeights(t *testing.T) {
	s := core.Symbol{Name: "foo", Qualname: "pkg.foo", Docstring: "abc def", Snippet: "x = 1"}
	toks := Tokenize(s)
	for _, tk := range toks {
		want := FieldWeights[tk.Field]
		if tk.Weight != want {
			t.Errorf("token %+v has weight %v; want %v", tk, tk.Weight, want)
		}
	}
}

func TestTokenizeQuery(t *testing.T) {
	got := TokenizeQuery("getUserById and other words")
	for _, want := range []string{"get", "user", "by", "id", "other", "words"} {
		if !contains(got, want) {
			t.Errorf("TokenizeQuery missing %q; got %v", want, got)
		}
	}
}
