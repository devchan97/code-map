package parser

import (
	"reflect"
	"testing"

	"github.com/devchan97/code-map/internal/core"
)

// fakeParser is a no-op Parser for registry testing.
type fakeParser struct{ lang string }

func (f *fakeParser) Language() string { return f.lang }
func (f *fakeParser) Parse(path string, src []byte) ([]core.Symbol, []core.Edge, error) {
	return nil, nil, nil
}

func TestRegisterFor(t *testing.T) {
	// Save original state and restore after test to avoid bleed-through.
	mu.Lock()
	saved := make(map[string]Parser, len(registry))
	for k, v := range registry {
		saved[k] = v
	}
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		registry = saved
		mu.Unlock()
	})

	p := &fakeParser{lang: "fake"}
	Register(p)
	got, ok := For("fake")
	if !ok || got != p {
		t.Fatalf("For(fake) = (%v, %v); want (%v, true)", got, ok, p)
	}

	// Replace by re-registering the same language.
	p2 := &fakeParser{lang: "fake"}
	Register(p2)
	got, _ = For("fake")
	if got != p2 {
		t.Errorf("Register should replace existing parser")
	}

	if _, ok := For("missing"); ok {
		t.Errorf("For(missing) should be false")
	}
}

func TestLanguages_Sorted(t *testing.T) {
	mu.Lock()
	saved := make(map[string]Parser, len(registry))
	for k, v := range registry {
		saved[k] = v
	}
	registry = map[string]Parser{}
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		registry = saved
		mu.Unlock()
	})

	for _, l := range []string{"zeta", "alpha", "mu"} {
		Register(&fakeParser{lang: l})
	}
	got := Languages()
	want := []string{"alpha", "mu", "zeta"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Languages() = %v; want %v", got, want)
	}
}

func TestDetectLanguage(t *testing.T) {
	cases := map[string]string{
		"foo.py":    "python",
		"foo.ts":    "ts",
		"foo.tsx":   "tsx",
		"foo.js":    "js",
		"foo.jsx":   "js",
		"foo.mjs":   "js",
		"foo.cjs":   "js",
		"Foo.java":  "java",
		"Foo.cs":    "csharp",
		"foo.cpp":   "cpp",
		"foo.cc":    "cpp",
		"foo.hpp":   "cpp",
		"main.go":   "go",
		"lib.rs":    "rust",
		"README.md": "",
		"":          "",
	}
	for path, want := range cases {
		got := DetectLanguage(path)
		if got != want {
			t.Errorf("DetectLanguage(%q) = %q; want %q", path, got, want)
		}
	}
}
