//go:build cgo

package csharp

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

const csharpFixture = `
using System;
using System.Collections.Generic;

namespace MyApp.Domain
{
    public interface IShape
    {
        double Area();
    }

    public class Shape : IShape
    {
        public const double PI = 3.14159;
        private string _color;

        public Shape(string color)
        {
            _color = color;
        }

        public double Area()
        {
            double result = PI * 2.0;
            return result;
        }

        public string GetColor()
        {
            return _color;
        }
    }

    public class Circle : Shape
    {
        private readonly double _radius;

        public Circle(double radius) : base("red")
        {
            _radius = radius;
        }

        public override double Area()
        {
            return PI * _radius * _radius;
        }
    }
}
`

func TestParse_CSharp_ClassAndMethod(t *testing.T) {
	a := &adapter{}
	syms, edges, err := a.Parse("MyApp/Domain/Shape.cs", []byte(csharpFixture))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	// Interface.
	iface := findSymbol(syms, ".IShape")
	if iface == nil || iface.Kind != core.SymbolClass {
		t.Errorf("IShape interface not detected: %v", iface)
	}

	// Class.
	shape := findSymbol(syms, ".Shape")
	if shape == nil || shape.Kind != core.SymbolClass || shape.Scope != core.ScopeGlobal {
		t.Errorf("Shape class not detected: %v", shape)
	}

	// Method.
	area := findSymbol(syms, ".Shape.Area")
	if area == nil || area.Kind != core.SymbolMethod || area.Scope != core.ScopeClass {
		t.Errorf("Shape.Area method not detected: %v", area)
	}

	// Constructor.
	ctor := findSymbol(syms, ".Shape.Shape")
	if ctor == nil || ctor.Kind != core.SymbolMethod {
		t.Errorf("Shape constructor not detected: %v", ctor)
	}

	// Inheritance: Circle extends Shape.
	circle := findSymbol(syms, ".Circle")
	if circle == nil {
		t.Fatalf("Circle class not detected")
	}

	inheritEdge := findEdge(edges, "Circle", "Shape", core.EdgeInherit)
	if inheritEdge == nil {
		t.Errorf("Circle→Shape inherit edge not detected")
	}
}

func TestParse_CSharp_Imports(t *testing.T) {
	a := &adapter{}
	syms, edges, err := a.Parse("MyApp/Domain/Shape.cs", []byte(csharpFixture))
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

	importEdge := findEdge(edges, "", "System", core.EdgeImport)
	if importEdge == nil {
		t.Errorf("import edge for System not detected")
	}
}

func TestParse_CSharp_LocalAndParam(t *testing.T) {
	a := &adapter{}
	syms, _, err := a.Parse("MyApp/Domain/Shape.cs", []byte(csharpFixture))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	hasParam := false
	hasLocal := false
	for _, s := range syms {
		if s.Scope == core.ScopeParam && s.Name == "color" {
			hasParam = true
		}
		if s.Scope == core.ScopeLocal && s.Name == "result" {
			hasLocal = true
		}
	}
	if !hasParam {
		t.Errorf("expected param symbol 'color'")
	}
	if !hasLocal {
		t.Errorf("expected local symbol 'result'")
	}
}

func TestLanguage_CSharp(t *testing.T) {
	a := &adapter{}
	if a.Language() != "csharp" {
		t.Errorf("Language() = %q; want csharp", a.Language())
	}
}
