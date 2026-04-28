//go:build cgo

package java

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

const javaFixture = `
package com.example;

import java.util.List;
import java.io.IOException;

public class Animal {
    private static final int MAX_AGE = 100;
    protected String name;

    public Animal(String name) {
        this.name = name;
    }

    public String getName() {
        String result = name.toUpperCase();
        return result;
    }
}

public class Dog extends Animal implements Runnable {
    public Dog(String name) {
        super(name);
    }

    public void run() {
        System.out.println(getName());
    }
}
`

func TestParse_Java_ClassAndMethod(t *testing.T) {
	a := &adapter{}
	syms, edges, err := a.Parse("com/example/Animal.java", []byte(javaFixture))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	// Class.
	animal := findSymbol(syms, ".Animal")
	if animal == nil || animal.Kind != core.SymbolClass || animal.Scope != core.ScopeGlobal {
		t.Errorf("Animal class not detected: %v", animal)
	}

	// Method.
	getName := findSymbol(syms, ".Animal.getName")
	if getName == nil || getName.Kind != core.SymbolMethod || getName.Scope != core.ScopeClass {
		t.Errorf("Animal.getName method not detected: %v", getName)
	}

	// Constructor.
	ctor := findSymbol(syms, ".Animal.Animal")
	if ctor == nil || ctor.Kind != core.SymbolMethod {
		t.Errorf("Animal constructor not detected: %v", ctor)
	}

	// Constant field (final + ALL_CAPS).
	maxAge := findSymbol(syms, ".Animal.MAX_AGE")
	if maxAge == nil || maxAge.Kind != core.SymbolConstant {
		t.Errorf("MAX_AGE constant not detected: %v", maxAge)
	}

	// Instance field.
	nameField := findSymbol(syms, ".Animal.name")
	if nameField == nil || nameField.Kind != core.SymbolVariable || nameField.Scope != core.ScopeClass {
		t.Errorf("Animal.name field not detected: %v", nameField)
	}

	// Inheritance edge: Dog extends Animal.
	dog := findSymbol(syms, ".Dog")
	if dog == nil || dog.Kind != core.SymbolClass {
		t.Errorf("Dog class not detected: %v", dog)
	}

	inheritEdge := findEdge(edges, "Dog", "Animal", core.EdgeInherit)
	if inheritEdge == nil {
		t.Errorf("Dog→Animal inherit edge not detected")
	}
}

func TestParse_Java_Imports(t *testing.T) {
	a := &adapter{}
	syms, edges, err := a.Parse("com/example/Animal.java", []byte(javaFixture))
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

	importEdge := findEdge(edges, "com.example", "java.util.List", core.EdgeImport)
	if importEdge == nil {
		t.Errorf("import edge for java.util.List not detected")
	}
}

func TestParse_Java_LocalVarAndParam(t *testing.T) {
	a := &adapter{}
	syms, _, err := a.Parse("com/example/Animal.java", []byte(javaFixture))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	hasParam := false
	hasLocal := false
	for _, s := range syms {
		if s.Scope == core.ScopeParam && s.Name == "name" {
			hasParam = true
		}
		if s.Scope == core.ScopeLocal && s.Name == "result" {
			hasLocal = true
		}
	}
	if !hasParam {
		t.Errorf("expected param symbol 'name'")
	}
	if !hasLocal {
		t.Errorf("expected local symbol 'result'")
	}
}

func TestParse_Java_CallEdge(t *testing.T) {
	a := &adapter{}
	_, edges, err := a.Parse("com/example/Animal.java", []byte(javaFixture))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	callEdge := findEdge(edges, "Dog.run", "println", core.EdgeCall)
	if callEdge == nil {
		t.Errorf("call edge for System.out.println not detected")
	}
}

func TestLanguage_Java(t *testing.T) {
	a := &adapter{}
	if a.Language() != "java" {
		t.Errorf("Language() = %q; want java", a.Language())
	}
}
