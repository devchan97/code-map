package fallback

import (
	"strings"
	"testing"

	"github.com/devchan97/code-map/internal/core"
)

func TestParser_LanguageAndBasic(t *testing.T) {
	p := New()
	if p.Language() != "fallback" {
		t.Errorf("Language = %q", p.Language())
	}

	src := []byte("line1\nline2\nline3\n")
	syms, edges, err := p.Parse("path/to/x.weird", src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(edges) != 0 {
		t.Errorf("expected no edges; got %v", edges)
	}
	if len(syms) != 1 {
		t.Fatalf("expected 1 symbol; got %d", len(syms))
	}
	s := syms[0]
	if s.Name != "x.weird" {
		t.Errorf("Name = %q; want x.weird", s.Name)
	}
	if s.Kind != core.SymbolVariable {
		t.Errorf("Kind = %q", s.Kind)
	}
	if s.Scope != core.ScopeGlobal {
		t.Errorf("Scope = %q", s.Scope)
	}
	if s.LineStart != 1 || s.LineEnd != 3 {
		t.Errorf("Lines = %d-%d; want 1-3", s.LineStart, s.LineEnd)
	}
}

func TestParser_TruncatesTo10Lines(t *testing.T) {
	p := New()
	var sb strings.Builder
	for i := 0; i < 20; i++ {
		sb.WriteString("line\n")
	}
	syms, _, err := p.Parse("big.txt", []byte(sb.String()))
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Count(syms[0].Snippet, "\n")
	if got > 10 {
		t.Errorf("snippet has %d newlines; expected ≤ 10", got)
	}
}

func TestParser_EmptyFile(t *testing.T) {
	p := New()
	syms, _, err := p.Parse("empty.txt", []byte(""))
	if err != nil {
		t.Fatal(err)
	}
	if syms[0].LineEnd < 1 {
		t.Errorf("LineEnd should be at least 1; got %d", syms[0].LineEnd)
	}
}
