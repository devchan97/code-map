// Package rust provides a tree-sitter based Rust parser for codemap.
package rust

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	tsrust "github.com/smacker/go-tree-sitter/rust"

	"github.com/devchan97/code-map/internal/core"
)

// Parser implements parser.Parser for Rust source files.
type Parser struct{}

// New returns a Rust Parser.
func New() *Parser { return &Parser{} }

// Language returns "rust".
func (p *Parser) Language() string { return "rust" }

// Parse extracts symbols and edges from a Rust source file.
func (p *Parser) Parse(path string, src []byte) ([]core.Symbol, []core.Edge, error) {
	sp := sitter.NewParser()
	sp.SetLanguage(tsrust.GetLanguage())

	tree, parseErr := sp.ParseCtx(context.Background(), nil, src)
	if parseErr != nil {
		return nil, nil, fmt.Errorf("rust parse %q: %w", path, parseErr)
	}

	root := tree.RootNode()
	v := &visitor{
		src:           src,
		path:          path,
		modulePath:    moduleQualname(path),
		importedNames: make(map[string]bool),
	}
	v.walkSourceFile(root, v.modulePath)
	return v.symbols, v.edges, nil
}

type visitor struct {
	src           []byte
	path          string
	modulePath    string
	symbols       []core.Symbol
	edges         []core.Edge
	importedNames map[string]bool
}

// moduleQualname converts a file path to a Rust-flavoured module qualname.
// src/parser/lexer.rs    → src.parser.lexer
// src/parser/mod.rs      → src.parser
// src/main.rs / lib.rs   → src
func moduleQualname(path string) string {
	path = strings.ReplaceAll(path, "\\", "/")
	path = strings.TrimPrefix(path, "./")

	ext := filepath.Ext(path)
	noExt := strings.TrimSuffix(path, ext)
	dotted := strings.ReplaceAll(noExt, "/", ".")
	dotted = strings.TrimSuffix(dotted, ".mod")
	dotted = strings.TrimSuffix(dotted, ".main")
	dotted = strings.TrimSuffix(dotted, ".lib")
	if dotted == "" {
		return "crate"
	}
	return dotted
}

func (v *visitor) nodeText(n *sitter.Node) string {
	if n == nil {
		return ""
	}
	return string(v.src[n.StartByte():n.EndByte()])
}

func (v *visitor) snippet(n *sitter.Node) string {
	text := string(v.src[n.StartByte():n.EndByte()])
	lines := strings.Split(text, "\n")
	if len(lines) > 10 {
		lines = lines[:10]
		lines = append(lines, "…")
	}
	return strings.Join(lines, "\n")
}

func lineNumber(row uint32) int { return int(row) + 1 }

var (
	wsCollapser = regexp.MustCompile(`\s+`)
	identRe     = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

func cleanDocAttr(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.TrimPrefix(s, "///")
	s = strings.TrimPrefix(s, "//!")
	s = strings.TrimPrefix(s, "//")
	return wsCollapser.ReplaceAllString(strings.TrimSpace(s), " ")
}

// docComment collects `///` doc lines and contiguous `//` comments preceding a node.
func (v *visitor) docComment(n *sitter.Node) string {
	prev := n.PrevSibling()
	var lines []string
	for prev != nil {
		t := prev.Type()
		if t == "line_comment" || t == "block_comment" {
			lines = append([]string{cleanDocAttr(v.nodeText(prev))}, lines...)
			prev = prev.PrevSibling()
			continue
		}
		break
	}
	return strings.Join(lines, " ")
}

// walkSourceFile iterates over top-level items.
// scope is the module qualname under which symbols are defined.
func (v *visitor) walkSourceFile(root *sitter.Node, scope string) {
	for i := 0; i < int(root.ChildCount()); i++ {
		child := root.Child(i)
		if child == nil {
			continue
		}
		v.handleItem(child, scope)
	}
}

// handleItem dispatches a single Rust item declaration to the appropriate handler.
func (v *visitor) handleItem(n *sitter.Node, scope string) {
	switch n.Type() {
	case "use_declaration":
		v.handleUse(n, scope)
	case "function_item":
		v.handleFunction(n, scope, "")
	case "struct_item":
		v.handleTypeItem(n, scope, core.SymbolClass)
	case "enum_item":
		v.handleTypeItem(n, scope, core.SymbolClass)
	case "union_item":
		v.handleTypeItem(n, scope, core.SymbolClass)
	case "trait_item":
		v.handleTrait(n, scope)
	case "type_item":
		v.handleTypeItem(n, scope, core.SymbolClass)
	case "impl_item":
		v.handleImpl(n, scope)
	case "const_item":
		v.handleValue(n, scope, core.SymbolConstant)
	case "static_item":
		v.handleValue(n, scope, core.SymbolVariable)
	case "mod_item":
		v.handleMod(n, scope)
	}
}

// handleUse processes a `use foo::bar::Baz;` statement.
func (v *visitor) handleUse(n *sitter.Node, scope string) {
	pathNode := n.ChildByFieldName("argument")
	if pathNode == nil {
		// fall back to scanning children for scoped identifier or use_list.
		for i := 0; i < int(n.ChildCount()); i++ {
			c := n.Child(i)
			if c == nil {
				continue
			}
			t := c.Type()
			if t == "scoped_identifier" || t == "scoped_use_list" || t == "use_list" || t == "identifier" || t == "use_as_clause" {
				pathNode = c
				break
			}
		}
	}
	if pathNode == nil {
		return
	}
	target := v.nodeText(pathNode)
	target = strings.TrimSuffix(strings.TrimSpace(target), ";")
	if target == "" {
		return
	}

	// Local binding: last path segment, or alias from use_as_clause.
	localName := lastPathSegment(target)
	if pathNode.Type() == "use_as_clause" {
		if alias := pathNode.ChildByFieldName("alias"); alias != nil {
			localName = v.nodeText(alias)
		}
	}

	if identRe.MatchString(localName) {
		v.symbols = append(v.symbols, core.Symbol{
			Name:      localName,
			Qualname:  scope + "." + localName,
			Kind:      core.SymbolImport,
			Scope:     core.ScopeGlobal,
			File:      v.path,
			LineStart: lineNumber(n.StartPoint().Row),
			LineEnd:   lineNumber(n.EndPoint().Row),
			Snippet:   v.nodeText(n),
		})
		v.importedNames[localName] = true
	}

	v.edges = append(v.edges, core.Edge{
		FromQualname: scope,
		ToQualname:   target,
		Kind:         core.EdgeImport,
		Resolved:     false,
	})
}

func lastPathSegment(p string) string {
	// Strip everything up to the last `::`. Also handle nested use lists by
	// taking the rightmost identifier-shaped chunk.
	if idx := strings.LastIndex(p, "::"); idx != -1 {
		p = p[idx+2:]
	}
	p = strings.TrimSpace(p)
	// In `{a, b}` style, just bail.
	if strings.ContainsAny(p, "{},") {
		return ""
	}
	return p
}

// handleFunction processes a function_item. parentQualname is non-empty for impl methods.
func (v *visitor) handleFunction(n *sitter.Node, scope, parentQualname string) {
	nameNode := n.ChildByFieldName("name")
	if nameNode == nil {
		return
	}
	name := v.nodeText(nameNode)
	qualname := scope + "." + name
	kind := core.SymbolFunction
	symScope := core.ScopeGlobal
	if parentQualname != "" {
		kind = core.SymbolMethod
		symScope = core.ScopeClass
	}

	v.symbols = append(v.symbols, core.Symbol{
		Name:           name,
		Qualname:       qualname,
		Kind:           kind,
		Scope:          symScope,
		File:           v.path,
		LineStart:      lineNumber(n.StartPoint().Row),
		LineEnd:        lineNumber(n.EndPoint().Row),
		ParentQualname: parentQualname,
		Docstring:      v.docComment(n),
		Snippet:        v.snippet(n),
	})

	v.handleParameters(n.ChildByFieldName("parameters"), qualname)
	if body := n.ChildByFieldName("body"); body != nil {
		v.walkBody(body, qualname)
	}
}

func (v *visitor) handleParameters(params *sitter.Node, funcQualname string) {
	if params == nil {
		return
	}
	for i := 0; i < int(params.ChildCount()); i++ {
		child := params.Child(i)
		if child == nil {
			continue
		}
		var paramName string
		switch child.Type() {
		case "parameter":
			pat := child.ChildByFieldName("pattern")
			if pat != nil && pat.Type() == "identifier" {
				paramName = v.nodeText(pat)
			}
		case "self_parameter":
			paramName = "self"
		}
		if paramName == "" || !identRe.MatchString(paramName) {
			continue
		}
		v.symbols = append(v.symbols, core.Symbol{
			Name:           paramName,
			Qualname:       funcQualname + "." + paramName,
			Kind:           core.SymbolVariable,
			Scope:          core.ScopeParam,
			File:           v.path,
			LineStart:      lineNumber(child.StartPoint().Row),
			LineEnd:        lineNumber(child.EndPoint().Row),
			ParentQualname: funcQualname,
			Snippet:        v.nodeText(child),
		})
	}
}

// handleTypeItem covers struct/enum/union/type alias.
func (v *visitor) handleTypeItem(n *sitter.Node, scope string, kind core.SymbolKind) {
	nameNode := n.ChildByFieldName("name")
	if nameNode == nil {
		return
	}
	name := v.nodeText(nameNode)
	qualname := scope + "." + name
	v.symbols = append(v.symbols, core.Symbol{
		Name:      name,
		Qualname:  qualname,
		Kind:      kind,
		Scope:     core.ScopeGlobal,
		File:      v.path,
		LineStart: lineNumber(n.StartPoint().Row),
		LineEnd:   lineNumber(n.EndPoint().Row),
		Docstring: v.docComment(n),
		Snippet:   v.snippet(n),
	})
}

// handleTrait emits a class-kind symbol for the trait and a method symbol for each
// signature it declares (no body — signatures only).
func (v *visitor) handleTrait(n *sitter.Node, scope string) {
	nameNode := n.ChildByFieldName("name")
	if nameNode == nil {
		return
	}
	name := v.nodeText(nameNode)
	traitQualname := scope + "." + name
	v.symbols = append(v.symbols, core.Symbol{
		Name:      name,
		Qualname:  traitQualname,
		Kind:      core.SymbolClass,
		Scope:     core.ScopeGlobal,
		File:      v.path,
		LineStart: lineNumber(n.StartPoint().Row),
		LineEnd:   lineNumber(n.EndPoint().Row),
		Docstring: v.docComment(n),
		Snippet:   v.snippet(n),
	})

	// Supertrait bounds: `trait Foo: Bar + Baz` → inherit edges.
	if bounds := n.ChildByFieldName("bounds"); bounds != nil {
		v.collectBoundEdges(bounds, traitQualname)
	}

	if body := n.ChildByFieldName("body"); body != nil {
		for i := 0; i < int(body.ChildCount()); i++ {
			c := body.Child(i)
			if c == nil {
				continue
			}
			if c.Type() == "function_item" || c.Type() == "function_signature_item" {
				if nm := c.ChildByFieldName("name"); nm != nil {
					methodName := v.nodeText(nm)
					v.symbols = append(v.symbols, core.Symbol{
						Name:           methodName,
						Qualname:       traitQualname + "." + methodName,
						Kind:           core.SymbolMethod,
						Scope:          core.ScopeClass,
						File:           v.path,
						LineStart:      lineNumber(c.StartPoint().Row),
						LineEnd:        lineNumber(c.EndPoint().Row),
						ParentQualname: traitQualname,
						Docstring:      v.docComment(c),
						Snippet:        v.snippet(c),
					})
				}
			}
		}
	}
}

// handleImpl walks `impl Foo { ... }` and `impl Trait for Foo { ... }`.
// Methods inside the impl are scoped under the receiving type, mirroring how callers
// reference them. An `impl Trait for Foo` block emits an inherit edge Foo→Trait.
func (v *visitor) handleImpl(n *sitter.Node, scope string) {
	typeNode := n.ChildByFieldName("type")
	if typeNode == nil {
		return
	}
	typeName := v.nodeText(typeNode)
	if typeName == "" {
		return
	}
	parentQualname := scope + "." + typeName

	// `impl Trait for Type` form.
	if trait := n.ChildByFieldName("trait"); trait != nil {
		traitName := v.nodeText(trait)
		if traitName != "" {
			v.edges = append(v.edges, core.Edge{
				FromQualname: parentQualname,
				ToQualname:   traitName,
				Kind:         core.EdgeInherit,
				Resolved:     false,
			})
		}
	}

	body := n.ChildByFieldName("body")
	if body == nil {
		return
	}
	for i := 0; i < int(body.ChildCount()); i++ {
		c := body.Child(i)
		if c == nil {
			continue
		}
		switch c.Type() {
		case "function_item":
			v.handleFunction(c, parentQualname, parentQualname)
		case "const_item":
			v.handleValue(c, parentQualname, core.SymbolConstant)
		case "type_item":
			v.handleTypeItem(c, parentQualname, core.SymbolClass)
		}
	}
}

// collectBoundEdges walks a trait_bounds node emitting inherit edges.
func (v *visitor) collectBoundEdges(n *sitter.Node, fromQualname string) {
	if n == nil {
		return
	}
	if n.Type() == "type_identifier" || n.Type() == "scoped_type_identifier" {
		v.edges = append(v.edges, core.Edge{
			FromQualname: fromQualname,
			ToQualname:   v.nodeText(n),
			Kind:         core.EdgeInherit,
			Resolved:     false,
		})
		return
	}
	for i := 0; i < int(n.ChildCount()); i++ {
		v.collectBoundEdges(n.Child(i), fromQualname)
	}
}

func (v *visitor) handleValue(n *sitter.Node, scope string, kind core.SymbolKind) {
	nameNode := n.ChildByFieldName("name")
	if nameNode == nil {
		return
	}
	name := v.nodeText(nameNode)
	if !identRe.MatchString(name) {
		return
	}
	v.symbols = append(v.symbols, core.Symbol{
		Name:      name,
		Qualname:  scope + "." + name,
		Kind:      kind,
		Scope:     core.ScopeGlobal,
		File:      v.path,
		LineStart: lineNumber(n.StartPoint().Row),
		LineEnd:   lineNumber(n.EndPoint().Row),
		Docstring: v.docComment(n),
		Snippet:   v.snippet(n),
	})
}

// handleMod handles inline `mod foo { ... }` (file-style `mod foo;` is just a stub
// with no body and contributes nothing useful here — the actual file is indexed
// separately by the walker).
func (v *visitor) handleMod(n *sitter.Node, scope string) {
	nameNode := n.ChildByFieldName("name")
	if nameNode == nil {
		return
	}
	name := v.nodeText(nameNode)
	modQualname := scope + "." + name
	v.symbols = append(v.symbols, core.Symbol{
		Name:      name,
		Qualname:  modQualname,
		Kind:      core.SymbolClass,
		Scope:     core.ScopeGlobal,
		File:      v.path,
		LineStart: lineNumber(n.StartPoint().Row),
		LineEnd:   lineNumber(n.EndPoint().Row),
		Docstring: v.docComment(n),
		Snippet:   v.snippet(n),
	})
	if body := n.ChildByFieldName("body"); body != nil {
		v.walkSourceFile(body, modQualname)
	}
}

// walkBody recursively walks a function body collecting call edges and reference
// edges for `use`-bound names.
func (v *visitor) walkBody(n *sitter.Node, enclosingQualname string) {
	if n == nil {
		return
	}
	if n.Type() == "call_expression" {
		funcNode := n.ChildByFieldName("function")
		if funcNode != nil {
			target := v.nodeText(funcNode)
			v.edges = append(v.edges, core.Edge{
				FromQualname: enclosingQualname,
				ToQualname:   target,
				Kind:         core.EdgeCall,
				Resolved:     false,
			})
		}
	}
	if n.Type() == "macro_invocation" {
		// e.g. `println!(...)` — emit as a call edge to the macro name.
		if macroName := n.ChildByFieldName("macro"); macroName != nil {
			v.edges = append(v.edges, core.Edge{
				FromQualname: enclosingQualname,
				ToQualname:   v.nodeText(macroName) + "!",
				Kind:         core.EdgeCall,
				Resolved:     false,
			})
		}
	}
	if n.Type() == "identifier" && n.Parent() != nil {
		parentType := n.Parent().Type()
		if parentType != "function_item" &&
			parentType != "let_declaration" &&
			parentType != "parameter" &&
			parentType != "use_declaration" &&
			parentType != "use_as_clause" {
			name := v.nodeText(n)
			if v.importedNames[name] {
				v.edges = append(v.edges, core.Edge{
					FromQualname: enclosingQualname,
					ToQualname:   name,
					Kind:         core.EdgeReference,
					Resolved:     false,
				})
			}
		}
	}
	for i := 0; i < int(n.ChildCount()); i++ {
		v.walkBody(n.Child(i), enclosingQualname)
	}
}
