//go:build cgo

package python

import (
	"strings"
	"testing"

	"github.com/devchan97/code-map/internal/core"
)

// findSymbol returns the first symbol with the given qualname suffix, or nil.
func findSymbol(syms []core.Symbol, qualnameSuffix string) *core.Symbol {
	for i := range syms {
		if strings.HasSuffix(syms[i].Qualname, qualnameSuffix) {
			return &syms[i]
		}
	}
	return nil
}

func TestParse_ModuleFunctionAndClass(t *testing.T) {
	src := []byte(`
def top_level():
    pass

class MyClass:
    def method(self, x):
        return x

GLOBAL_CONST = 42
`)
	p := New()
	syms, _, err := p.Parse("app/mod.py", src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	fn := findSymbol(syms, "top_level")
	if fn == nil || fn.Kind != core.SymbolFunction || fn.Scope != core.ScopeGlobal {
		t.Errorf("module-top function not detected: %v", fn)
	}

	cls := findSymbol(syms, ".MyClass")
	if cls == nil || cls.Kind != core.SymbolClass || cls.Scope != core.ScopeGlobal {
		t.Errorf("class not detected: %v", cls)
	}

	method := findSymbol(syms, ".MyClass.method")
	if method == nil || method.Kind != core.SymbolMethod || method.Scope != core.ScopeClass {
		t.Errorf("method not detected: %v", method)
	}

	// ALL_CAPS module-top assignment → constant.
	cnst := findSymbol(syms, "GLOBAL_CONST")
	if cnst == nil {
		t.Errorf("GLOBAL_CONST not detected")
	} else if cnst.Kind != core.SymbolConstant {
		t.Errorf("GLOBAL_CONST kind = %s; want constant", cnst.Kind)
	}
}

func TestParse_FunctionParametersAndLocals(t *testing.T) {
	src := []byte(`
def fn(a, b):
    local_var = a + b
    return local_var
`)
	p := New()
	syms, _, err := p.Parse("m.py", src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	hasParam := false
	hasLocal := false
	for _, s := range syms {
		if s.Scope == core.ScopeParam && (s.Name == "a" || s.Name == "b") {
			hasParam = true
		}
		if s.Scope == core.ScopeLocal && s.Name == "local_var" {
			hasLocal = true
		}
	}
	if !hasParam {
		t.Errorf("expected param symbols a/b")
	}
	if !hasLocal {
		t.Errorf("expected local_var symbol")
	}
}

func TestParse_Imports(t *testing.T) {
	src := []byte(`
import os
import json as j
from collections import deque
`)
	p := New()
	syms, _, err := p.Parse("m.py", src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	importCount := 0
	for _, s := range syms {
		if s.Kind == core.SymbolImport {
			importCount++
		}
	}
	if importCount < 3 {
		t.Errorf("expected ≥ 3 import symbols; got %d", importCount)
	}
}

func TestParse_ModuleQualnameTrimsInit(t *testing.T) {
	p := New()
	syms, _, err := p.Parse("pkg/sub/__init__.py", []byte("def foo():\n    pass\n"))
	if err != nil {
		t.Fatal(err)
	}
	fn := findSymbol(syms, "foo")
	if fn == nil {
		t.Fatal("foo not detected")
	}
	if !strings.HasPrefix(fn.Qualname, "pkg.sub.foo") {
		t.Errorf("qualname = %q; want pkg.sub.foo (no .__init__ suffix)", fn.Qualname)
	}
}

func TestLanguage(t *testing.T) {
	if New().Language() != "python" {
		t.Errorf("Language() = %q", New().Language())
	}
}
