// Package golang provides a tree-sitter based Go parser for codemap.
package golang

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	tsgolang "github.com/smacker/go-tree-sitter/golang"

	"github.com/devchan97/code-map/internal/core"
)

// Parser implements parser.Parser for Go source files.
type Parser struct{}

// New returns a Go Parser.
func New() *Parser { return &Parser{} }

// Language returns "go".
func (p *Parser) Language() string { return "go" }

// Parse extracts symbols and edges from a Go source file.
func (p *Parser) Parse(path string, src []byte) ([]core.Symbol, []core.Edge, error) {
	sp := sitter.NewParser()
	sp.SetLanguage(tsgolang.GetLanguage())

	tree, parseErr := sp.ParseCtx(context.Background(), nil, src)
	if parseErr != nil {
		return nil, nil, fmt.Errorf("go parse %q: %w", path, parseErr)
	}

	root := tree.RootNode()
	v := &visitor{
		src:           src,
		path:          path,
		pkg:           packageQualname(path, root, src),
		importedNames: make(map[string]bool),
	}
	v.walkSourceFile(root)
	return v.symbols, v.edges, nil
}

// visitor holds parsing state while walking the tree-sitter AST.
type visitor struct {
	src           []byte
	path          string
	pkg           string
	symbols       []core.Symbol
	edges         []core.Edge
	importedNames map[string]bool
}

// packageQualname derives the package qualname. We prefer the directory path so that
// callers see "internal/parser/golang" rather than the package_clause "golang", which
// collides across directories.
func packageQualname(path string, root *sitter.Node, src []byte) string {
	dir := filepath.Dir(path)
	dir = strings.ReplaceAll(dir, "\\", "/")
	dir = strings.TrimPrefix(dir, "./")
	if dir == "." || dir == "" {
		// Top-level file — fall back to package_clause name.
		for i := 0; i < int(root.ChildCount()); i++ {
			c := root.Child(i)
			if c != nil && c.Type() == "package_clause" {
				for j := 0; j < int(c.ChildCount()); j++ {
					cc := c.Child(j)
					if cc != nil && cc.Type() == "package_identifier" {
						return string(src[cc.StartByte():cc.EndByte()])
					}
				}
			}
		}
		return "main"
	}
	return strings.ReplaceAll(dir, "/", ".")
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

func cleanComment(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.TrimPrefix(s, "//")
	s = strings.TrimPrefix(s, "/*")
	s = strings.TrimSuffix(s, "*/")
	return wsCollapser.ReplaceAllString(strings.TrimSpace(s), " ")
}

// docComment returns the comment block immediately preceding n at the same indentation,
// joined into a single string.
func (v *visitor) docComment(n *sitter.Node) string {
	prev := n.PrevSibling()
	var lines []string
	for prev != nil && (prev.Type() == "comment") {
		// Walk further back across consecutive comments.
		lines = append([]string{cleanComment(v.nodeText(prev))}, lines...)
		prev = prev.PrevSibling()
	}
	return strings.Join(lines, " ")
}

func isExported(name string) bool {
	if name == "" {
		return false
	}
	r := name[0]
	return r >= 'A' && r <= 'Z'
}

// walkSourceFile iterates over top-level declarations in a Go source file.
func (v *visitor) walkSourceFile(root *sitter.Node) {
	for i := 0; i < int(root.ChildCount()); i++ {
		child := root.Child(i)
		if child == nil {
			continue
		}
		switch child.Type() {
		case "import_declaration":
			v.handleImport(child)
		case "function_declaration":
			v.handleFunction(child)
		case "method_declaration":
			v.handleMethod(child)
		case "type_declaration":
			v.handleTypeDecl(child)
		case "const_declaration":
			v.handleValueDecl(child, core.SymbolConstant)
		case "var_declaration":
			v.handleValueDecl(child, core.SymbolVariable)
		}
	}
}

// handleImport processes both `import "x"` and grouped `import ( ... )` forms.
func (v *visitor) handleImport(n *sitter.Node) {
	// Walk all import_spec descendants (handles both single and grouped imports).
	var visit func(node *sitter.Node)
	visit = func(node *sitter.Node) {
		if node == nil {
			return
		}
		if node.Type() == "import_spec" {
			pathNode := node.ChildByFieldName("path")
			nameNode := node.ChildByFieldName("name")
			if pathNode == nil {
				return
			}
			rawPath := strings.Trim(v.nodeText(pathNode), `"`)
			if rawPath == "" {
				return
			}
			localName := ""
			if nameNode != nil {
				localName = v.nodeText(nameNode)
			} else {
				// Default to the last path segment.
				parts := strings.Split(rawPath, "/")
				localName = parts[len(parts)-1]
			}
			if localName == "" || localName == "_" || localName == "." {
				// Blank/dot imports still emit an edge but no usable local name.
				v.edges = append(v.edges, core.Edge{
					FromQualname: v.pkg,
					ToQualname:   rawPath,
					Kind:         core.EdgeImport,
					Resolved:     false,
				})
				return
			}
			sym := core.Symbol{
				Name:      localName,
				Qualname:  v.pkg + "." + localName,
				Kind:      core.SymbolImport,
				Scope:     core.ScopeGlobal,
				File:      v.path,
				LineStart: lineNumber(node.StartPoint().Row),
				LineEnd:   lineNumber(node.EndPoint().Row),
				Snippet:   v.nodeText(node),
			}
			v.symbols = append(v.symbols, sym)
			v.importedNames[localName] = true
			v.edges = append(v.edges, core.Edge{
				FromQualname: v.pkg,
				ToQualname:   rawPath,
				Kind:         core.EdgeImport,
				Resolved:     false,
			})
			return
		}
		for i := 0; i < int(node.ChildCount()); i++ {
			visit(node.Child(i))
		}
	}
	visit(n)
}

// handleFunction processes a top-level function_declaration.
func (v *visitor) handleFunction(n *sitter.Node) {
	nameNode := n.ChildByFieldName("name")
	if nameNode == nil {
		return
	}
	name := v.nodeText(nameNode)
	qualname := v.pkg + "." + name

	sym := core.Symbol{
		Name:      name,
		Qualname:  qualname,
		Kind:      core.SymbolFunction,
		Scope:     core.ScopeGlobal,
		File:      v.path,
		LineStart: lineNumber(n.StartPoint().Row),
		LineEnd:   lineNumber(n.EndPoint().Row),
		Docstring: v.docComment(n),
		Snippet:   v.snippet(n),
	}
	v.symbols = append(v.symbols, sym)

	v.handleParameters(n.ChildByFieldName("parameters"), qualname)

	if body := n.ChildByFieldName("body"); body != nil {
		v.walkBody(body, qualname)
	}
}

// handleMethod processes a method_declaration (`func (r Recv) Foo() {}`).
func (v *visitor) handleMethod(n *sitter.Node) {
	nameNode := n.ChildByFieldName("name")
	if nameNode == nil {
		return
	}
	methodName := v.nodeText(nameNode)
	receiverType := v.receiverTypeName(n.ChildByFieldName("receiver"))
	parentQualname := ""
	methodQualname := v.pkg + "." + methodName
	if receiverType != "" {
		parentQualname = v.pkg + "." + receiverType
		methodQualname = parentQualname + "." + methodName
	}

	sym := core.Symbol{
		Name:           methodName,
		Qualname:       methodQualname,
		Kind:           core.SymbolMethod,
		Scope:          core.ScopeClass,
		File:           v.path,
		LineStart:      lineNumber(n.StartPoint().Row),
		LineEnd:        lineNumber(n.EndPoint().Row),
		ParentQualname: parentQualname,
		Docstring:      v.docComment(n),
		Snippet:        v.snippet(n),
	}
	v.symbols = append(v.symbols, sym)

	v.handleParameters(n.ChildByFieldName("parameters"), methodQualname)

	if body := n.ChildByFieldName("body"); body != nil {
		v.walkBody(body, methodQualname)
	}
}

// receiverTypeName extracts the type name from a method receiver, e.g. `(r *Foo)` → "Foo".
func (v *visitor) receiverTypeName(receiver *sitter.Node) string {
	if receiver == nil {
		return ""
	}
	var find func(n *sitter.Node) string
	find = func(n *sitter.Node) string {
		if n == nil {
			return ""
		}
		if n.Type() == "type_identifier" {
			return v.nodeText(n)
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			if name := find(n.Child(i)); name != "" {
				return name
			}
		}
		return ""
	}
	return find(receiver)
}

// handleParameters emits parameter symbols.
func (v *visitor) handleParameters(params *sitter.Node, funcQualname string) {
	if params == nil {
		return
	}
	for i := 0; i < int(params.ChildCount()); i++ {
		child := params.Child(i)
		if child == nil || child.Type() != "parameter_declaration" {
			continue
		}
		nameNode := child.ChildByFieldName("name")
		if nameNode == nil {
			continue
		}
		name := v.nodeText(nameNode)
		if !identRe.MatchString(name) {
			continue
		}
		v.symbols = append(v.symbols, core.Symbol{
			Name:           name,
			Qualname:       funcQualname + "." + name,
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

// handleTypeDecl handles `type Foo struct { ... }`, `type Bar interface { ... }`, alias, etc.
// Grouped `type ( ... )` declarations are walked recursively.
func (v *visitor) handleTypeDecl(n *sitter.Node) {
	for i := 0; i < int(n.ChildCount()); i++ {
		child := n.Child(i)
		if child == nil {
			continue
		}
		switch child.Type() {
		case "type_spec":
			v.handleTypeSpec(child, n)
		case "type_alias":
			v.handleTypeSpec(child, n)
		}
	}
}

func (v *visitor) handleTypeSpec(spec *sitter.Node, parent *sitter.Node) {
	nameNode := spec.ChildByFieldName("name")
	if nameNode == nil {
		return
	}
	name := v.nodeText(nameNode)
	qualname := v.pkg + "." + name

	v.symbols = append(v.symbols, core.Symbol{
		Name:      name,
		Qualname:  qualname,
		Kind:      core.SymbolClass,
		Scope:     core.ScopeGlobal,
		File:      v.path,
		LineStart: lineNumber(spec.StartPoint().Row),
		LineEnd:   lineNumber(spec.EndPoint().Row),
		Docstring: v.docComment(parent),
		Snippet:   v.snippet(spec),
	})

	// Embedded interface/struct fields → inherit edges.
	if typeNode := spec.ChildByFieldName("type"); typeNode != nil {
		v.collectEmbedded(typeNode, qualname)
	}
}

// collectEmbedded walks a struct/interface type to find embedded type names.
func (v *visitor) collectEmbedded(n *sitter.Node, parentQualname string) {
	if n == nil {
		return
	}
	switch n.Type() {
	case "struct_type", "interface_type":
		for i := 0; i < int(n.ChildCount()); i++ {
			v.collectEmbedded(n.Child(i), parentQualname)
		}
	case "field_declaration_list", "interface_type_name", "method_elem":
		for i := 0; i < int(n.ChildCount()); i++ {
			v.collectEmbedded(n.Child(i), parentQualname)
		}
	case "field_declaration":
		// Embedded if there is no name field — type-only declaration.
		if n.ChildByFieldName("name") == nil {
			if t := n.ChildByFieldName("type"); t != nil {
				name := v.nodeText(t)
				if name != "" {
					v.edges = append(v.edges, core.Edge{
						FromQualname: parentQualname,
						ToQualname:   name,
						Kind:         core.EdgeInherit,
						Resolved:     false,
					})
				}
			}
		}
	case "type_identifier", "qualified_type":
		// Direct embedding inside an interface.
		name := v.nodeText(n)
		if name != "" {
			v.edges = append(v.edges, core.Edge{
				FromQualname: parentQualname,
				ToQualname:   name,
				Kind:         core.EdgeInherit,
				Resolved:     false,
			})
		}
	}
}

// handleValueDecl handles const_declaration and var_declaration (both single and grouped).
func (v *visitor) handleValueDecl(n *sitter.Node, kind core.SymbolKind) {
	var visit func(node *sitter.Node)
	visit = func(node *sitter.Node) {
		if node == nil {
			return
		}
		if node.Type() == "const_spec" || node.Type() == "var_spec" {
			nameNode := node.ChildByFieldName("name")
			if nameNode == nil {
				return
			}
			// name field may be a list of identifiers (a, b, c = 1, 2, 3).
			for i := 0; i < int(nameNode.ChildCount()); i++ {
				id := nameNode.Child(i)
				if id == nil || id.Type() != "identifier" {
					continue
				}
				name := v.nodeText(id)
				if !identRe.MatchString(name) {
					continue
				}
				v.symbols = append(v.symbols, core.Symbol{
					Name:      name,
					Qualname:  v.pkg + "." + name,
					Kind:      kind,
					Scope:     core.ScopeGlobal,
					File:      v.path,
					LineStart: lineNumber(node.StartPoint().Row),
					LineEnd:   lineNumber(node.EndPoint().Row),
					Snippet:   v.snippet(node),
				})
			}
			// Single-identifier case: name field is the identifier itself.
			if nameNode.ChildCount() == 0 {
				name := v.nodeText(nameNode)
				if identRe.MatchString(name) {
					v.symbols = append(v.symbols, core.Symbol{
						Name:      name,
						Qualname:  v.pkg + "." + name,
						Kind:      kind,
						Scope:     core.ScopeGlobal,
						File:      v.path,
						LineStart: lineNumber(node.StartPoint().Row),
						LineEnd:   lineNumber(node.EndPoint().Row),
						Snippet:   v.snippet(node),
					})
				}
			}
			return
		}
		for i := 0; i < int(node.ChildCount()); i++ {
			visit(node.Child(i))
		}
	}
	visit(n)

	// Best-effort: also note exported flag. Currently unused but reserved for future.
	_ = isExported
}

// walkBody recursively walks a function body collecting call edges and
// reference edges for imported package names.
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
	if n.Type() == "identifier" && n.Parent() != nil {
		parentType := n.Parent().Type()
		if parentType != "function_declaration" &&
			parentType != "method_declaration" &&
			parentType != "parameter_declaration" &&
			parentType != "var_spec" &&
			parentType != "const_spec" &&
			parentType != "short_var_declaration" &&
			parentType != "import_spec" {
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
