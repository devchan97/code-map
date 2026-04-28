//go:build cgo

package cpp

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

// findEdge returns the first edge matching from/to/kind, or nil.
func findEdge(edges []core.Edge, from, to string, kind core.EdgeKind) *core.Edge {
	for i := range edges {
		if edges[i].Kind == kind &&
			strings.Contains(edges[i].FromQualname, from) &&
			strings.Contains(edges[i].ToQualname, to) {
			return &edges[i]
		}
	}
	return nil
}

const cppFixture = `
#include <iostream>
#include <string>

const int MAX_SIZE = 256;

namespace shapes {

class Animal {
public:
    std::string name;
    int age;

    Animal(std::string n, int a) : name(n), age(a) {}

    std::string getName() {
        std::string result = name;
        return result;
    }
};

class Dog : public Animal {
public:
    Dog(std::string n) : Animal(n, 0) {}

    void bark() {
        std::cout << getName() << std::endl;
    }
};

void topLevelFunc(int x, int y) {
    int sum = x + y;
    std::cout << sum << std::endl;
}

} // namespace shapes
`

func TestParse_Cpp_ClassAndMethod(t *testing.T) {
	a := &adapter{}
	syms, edges, err := a.Parse("src/shapes.cpp", []byte(cppFixture))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	// Class.
	animal := findSymbol(syms, ".Animal")
	if animal == nil || animal.Kind != core.SymbolClass {
		t.Errorf("Animal class not detected: %v", animal)
	}

	// Inline method.
	getName := findSymbol(syms, ".Animal.getName")
	if getName == nil || getName.Kind != core.SymbolMethod {
		t.Errorf("Animal.getName method not detected: %v", getName)
	}

	// Inheritance: Dog extends Animal.
	dog := findSymbol(syms, ".Dog")
	if dog == nil || dog.Kind != core.SymbolClass {
		t.Errorf("Dog class not detected: %v", dog)
	}

	inheritEdge := findEdge(edges, "Dog", "Animal", core.EdgeInherit)
	if inheritEdge == nil {
		t.Errorf("Dog→Animal inherit edge not detected")
	}

	// Top-level function.
	fn := findSymbol(syms, ".topLevelFunc")
	if fn == nil || fn.Kind != core.SymbolFunction {
		t.Errorf("topLevelFunc not detected: %v", fn)
	}
}

func TestParse_Cpp_Imports(t *testing.T) {
	a := &adapter{}
	syms, edges, err := a.Parse("src/shapes.cpp", []byte(cppFixture))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	importCount := 0
	for _, s := range syms {
		if s.Kind == core.SymbolImport {
			importCount++
		}
	}
	if importCount < 2 {
		t.Errorf("expected ≥2 import symbols; got %d", importCount)
	}

	importEdge := findEdge(edges, "", "iostream", core.EdgeImport)
	if importEdge == nil {
		t.Errorf("import edge for iostream not detected")
	}
}

func TestParse_Cpp_LocalAndParam(t *testing.T) {
	a := &adapter{}
	syms, _, err := a.Parse("src/shapes.cpp", []byte(cppFixture))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	hasParam := false
	hasLocal := false
	for _, s := range syms {
		if s.Scope == core.ScopeParam && (s.Name == "x" || s.Name == "y") {
			hasParam = true
		}
		if s.Scope == core.ScopeLocal && s.Name == "sum" {
			hasLocal = true
		}
	}
	if !hasParam {
		t.Errorf("expected param symbols x/y")
	}
	if !hasLocal {
		t.Errorf("expected local symbol 'sum'")
	}
}

func TestParse_Cpp_GlobalConstant(t *testing.T) {
	a := &adapter{}
	syms, _, err := a.Parse("src/shapes.cpp", []byte(cppFixture))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	maxSize := findSymbol(syms, ".MAX_SIZE")
	if maxSize == nil || maxSize.Kind != core.SymbolConstant {
		t.Errorf("MAX_SIZE constant not detected: %v", maxSize)
	}
}

func TestLanguage_Cpp(t *testing.T) {
	a := &adapter{}
	if a.Language() != "cpp" {
		t.Errorf("Language() = %q; want cpp", a.Language())
	}
}
