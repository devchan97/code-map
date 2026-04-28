// Package java provides a tree-sitter based Java parser for codemap.
package java

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"unicode"

	sitter "github.com/smacker/go-tree-sitter"
	tsj "github.com/smacker/go-tree-sitter/java"

	"github.com/devchan97/code-map/internal/core"
	"github.com/devchan97/code-map/internal/parser"
)

func init() { parser.Register(&adapter{}) }

// adapter implements parser.Parser for Java source files.
type adapter struct{}

// Language returns "java".
func (a *adapter) Language() string { return "java" }

// Parse extracts symbols and edges from a Java source file.
func (a *adapter) Parse(path string, src []byte) ([]core.Symbol, []core.Edge, error) {
	sp := sitter.NewParser()
	sp.SetLanguage(tsj.GetLanguage())

	tree, err := sp.ParseCtx(context.Background(), nil, src)
	if err != nil {
		return nil, nil, fmt.Errorf("java parse %q: %w", path, err)
	}

	root := tree.RootNode()
	module := fileQualname(path)

	v := &visitor{
		src:           src,
		path:          path,
		module:        module,
		importedNames: make(map[string]bool),
	}
	v.walkRoot(root)
	return v.symbols, v.edges, nil
}

// visitor holds state while walking the Java AST.
type visitor struct {
	src           []byte
	path          string
	module        string
	symbols       []core.Symbol
	edges         []core.Edge
	importedNames map[string]bool
}

// fileQualname derives a dotted qualname from a repo-relative path.
// com/example/Foo.java → com.example.Foo
func fileQualname(path string) string {
	path = strings.ReplaceAll(path, "\\", "/")
	path = strings.TrimPrefix(path, "./")
	ext := filepath.Ext(path)
	noExt := strings.TrimSuffix(path, ext)
	return strings.ReplaceAll(noExt, "/", ".")
}

// nodeText returns the source text for a node.
func (v *visitor) nodeText(n *sitter.Node) string {
	if n == nil {
		return ""
	}
	return string(v.src[n.StartByte():n.EndByte()])
}

// snippet returns up to 10 lines of source for a node.
func (v *visitor) snippet(n *sitter.Node) string {
	text := string(v.src[n.StartByte():n.EndByte()])
	lines := strings.Split(text, "\n")
	if len(lines) > 10 {
		lines = lines[:10]
		lines = append(lines, "…")
	}
	return strings.Join(lines, "\n")
}

// lineNumber converts 0-based row to 1-based.
func lineNumber(row uint32) int { return int(row) + 1 }

// isAllCaps returns true if the name is ALL_CAPS (Java constant convention).
func isAllCaps(name string) bool {
	if name == "" {
		return false
	}
	hasUpper := false
	for i, r := range name {
		if i == 0 && r == '_' {
			continue
		}
		if unicode.IsLower(r) {
			return false
		}
		if unicode.IsUpper(r) {
			hasUpper = true
		}
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
			return false
		}
	}
	return hasUpper
}

// isIdentifier returns true if s is a valid Java identifier.
func isIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if i == 0 {
			if !unicode.IsLetter(r) && r != '_' {
				return false
			}
		} else {
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
				return false
			}
		}
	}
	return true
}

// hasFinalModifier returns true if the node contains a "final" keyword.
func (v *visitor) hasFinalModifier(n *sitter.Node) bool {
	if n == nil {
		return false
	}
	for i := 0; i < int(n.ChildCount()); i++ {
		child := n.Child(i)
		if child != nil && child.Type() == "final" {
			return true
		}
	}
	return false
}

// walkRoot walks top-level declarations in a Java compilation_unit.
func (v *visitor) walkRoot(root *sitter.Node) {
	for i := 0; i < int(root.ChildCount()); i++ {
		child := root.Child(i)
		if child == nil {
			continue
		}
		switch child.Type() {
		case "package_declaration":
			v.handlePackage(child)
		case "import_declaration":
			v.handleImport(child)
		case "class_declaration", "interface_declaration",
			"enum_declaration", "record_declaration",
			"annotation_type_declaration":
			v.handleClass(child, v.module)
		}
	}
}

// handlePackage updates the module qualname from the package declaration.
func (v *visitor) handlePackage(n *sitter.Node) {
	for i := 0; i < int(n.ChildCount()); i++ {
		child := n.Child(i)
		if child == nil {
			continue
		}
		switch child.Type() {
		case "identifier", "scoped_identifier":
			v.module = v.nodeText(child)
			return
		}
	}
}

// handleImport processes an import_declaration.
func (v *visitor) handleImport(n *sitter.Node) {
	var importedPath string
	for i := 0; i < int(n.ChildCount()); i++ {
		child := n.Child(i)
		if child == nil {
			continue
		}
		switch child.Type() {
		case "scoped_identifier", "identifier":
			importedPath = v.nodeText(child)
		case "asterisk":
			if importedPath != "" {
				importedPath += ".*"
			}
		}
	}
	if importedPath == "" {
		return
	}

	parts := strings.Split(importedPath, ".")
	localName := parts[len(parts)-1]
	if localName == "*" && len(parts) > 1 {
		localName = parts[len(parts)-2] + ".*"
	}

	sym := core.Symbol{
		Name:      localName,
		Qualname:  v.module + "." + localName,
		Kind:      core.SymbolImport,
		Scope:     core.ScopeGlobal,
		File:      v.path,
		LineStart: lineNumber(n.StartPoint().Row),
		LineEnd:   lineNumber(n.EndPoint().Row),
		Snippet:   v.snippet(n),
	}
	v.symbols = append(v.symbols, sym)
	v.importedNames[localName] = true

	v.edges = append(v.edges, core.Edge{
		FromQualname: v.module,
		ToQualname:   importedPath,
		Kind:         core.EdgeImport,
		Resolved:     false,
	})
}

// handleClass processes class, interface, enum, and record declarations.
func (v *visitor) handleClass(n *sitter.Node, parentQualname string) {
	nameNode := n.ChildByFieldName("name")
	if nameNode == nil {
		return
	}
	className := v.nodeText(nameNode)
	classQualname := parentQualname + "." + className

	sym := core.Symbol{
		Name:      className,
		Qualname:  classQualname,
		Kind:      core.SymbolClass,
		Scope:     core.ScopeGlobal,
		File:      v.path,
		LineStart: lineNumber(n.StartPoint().Row),
		LineEnd:   lineNumber(n.EndPoint().Row),
		Snippet:   v.snippet(n),
	}
	v.symbols = append(v.symbols, sym)

	v.handleInheritance(n, classQualname)

	bodyNode := n.ChildByFieldName("body")
	if bodyNode == nil {
		for i := 0; i < int(n.ChildCount()); i++ {
			child := n.Child(i)
			if child != nil && (child.Type() == "class_body" ||
				child.Type() == "interface_body" ||
				child.Type() == "enum_body") {
				bodyNode = child
				break
			}
		}
	}
	if bodyNode != nil {
		v.walkClassBody(bodyNode, classQualname)
	}
}

// handleInheritance emits EdgeInherit for superclass and interfaces.
func (v *visitor) handleInheritance(n *sitter.Node, classQualname string) {
	superNode := n.ChildByFieldName("superclass")
	if superNode != nil {
		superName := v.nodeText(superNode)
		v.edges = append(v.edges, core.Edge{
			FromQualname: classQualname,
			ToQualname:   superName,
			Kind:         core.EdgeInherit,
			Resolved:     false,
		})
	}

	ifaceNode := n.ChildByFieldName("interfaces")
	if ifaceNode != nil {
		v.extractTypeList(ifaceNode, classQualname)
	}
	extendsNode := n.ChildByFieldName("extends_interfaces")
	if extendsNode != nil {
		v.extractTypeList(extendsNode, classQualname)
	}
}

// extractTypeList emits inherit edges for each type_identifier in a node subtree.
func (v *visitor) extractTypeList(n *sitter.Node, classQualname string) {
	for i := 0; i < int(n.ChildCount()); i++ {
		child := n.Child(i)
		if child == nil {
			continue
		}
		if child.Type() == "type_identifier" || child.Type() == "generic_type" {
			v.edges = append(v.edges, core.Edge{
				FromQualname: classQualname,
				ToQualname:   v.nodeText(child),
				Kind:         core.EdgeInherit,
				Resolved:     false,
			})
		}
	}
}

// walkClassBody processes members of a class or interface body.
func (v *visitor) walkClassBody(bodyNode *sitter.Node, classQualname string) {
	for i := 0; i < int(bodyNode.ChildCount()); i++ {
		child := bodyNode.Child(i)
		if child == nil {
			continue
		}
		switch child.Type() {
		case "method_declaration":
			v.handleMethod(child, classQualname)
		case "constructor_declaration":
			v.handleConstructor(child, classQualname)
		case "field_declaration":
			v.handleField(child, classQualname)
		case "class_declaration", "interface_declaration", "enum_declaration":
			v.handleNestedClass(child, classQualname)
		}
	}
}

// handleMethod processes a method_declaration inside a class.
func (v *visitor) handleMethod(n *sitter.Node, classQualname string) {
	nameNode := n.ChildByFieldName("name")
	if nameNode == nil {
		return
	}
	methodName := v.nodeText(nameNode)
	methodQualname := classQualname + "." + methodName

	sym := core.Symbol{
		Name:           methodName,
		Qualname:       methodQualname,
		Kind:           core.SymbolMethod,
		Scope:          core.ScopeClass,
		File:           v.path,
		LineStart:      lineNumber(n.StartPoint().Row),
		LineEnd:        lineNumber(n.EndPoint().Row),
		ParentQualname: classQualname,
		Snippet:        v.snippet(n),
	}
	v.symbols = append(v.symbols, sym)

	paramsNode := n.ChildByFieldName("parameters")
	v.handleParameters(paramsNode, methodQualname)

	bodyNode := n.ChildByFieldName("body")
	if bodyNode != nil {
		v.walkFunctionBody(bodyNode, methodQualname)
	}
}

// handleConstructor processes a constructor_declaration.
func (v *visitor) handleConstructor(n *sitter.Node, classQualname string) {
	nameNode := n.ChildByFieldName("name")
	if nameNode == nil {
		return
	}
	ctorName := v.nodeText(nameNode)
	ctorQualname := classQualname + "." + ctorName

	sym := core.Symbol{
		Name:           ctorName,
		Qualname:       ctorQualname,
		Kind:           core.SymbolMethod,
		Scope:          core.ScopeClass,
		File:           v.path,
		LineStart:      lineNumber(n.StartPoint().Row),
		LineEnd:        lineNumber(n.EndPoint().Row),
		ParentQualname: classQualname,
		Snippet:        v.snippet(n),
	}
	v.symbols = append(v.symbols, sym)

	paramsNode := n.ChildByFieldName("parameters")
	v.handleParameters(paramsNode, ctorQualname)

	bodyNode := n.ChildByFieldName("body")
	if bodyNode != nil {
		v.walkFunctionBody(bodyNode, ctorQualname)
	}
}

// handleField processes a field_declaration inside a class body.
func (v *visitor) handleField(n *sitter.Node, classQualname string) {
	modNode := n.ChildByFieldName("modifiers")
	isFinal := v.hasFinalModifier(modNode)

	for i := 0; i < int(n.ChildCount()); i++ {
		child := n.Child(i)
		if child == nil || child.Type() != "variable_declarator" {
			continue
		}
		nameNode := child.ChildByFieldName("name")
		if nameNode == nil {
			continue
		}
		name := v.nodeText(nameNode)
		if !isIdentifier(name) {
			continue
		}
		kind := core.SymbolVariable
		if isFinal || isAllCaps(name) {
			kind = core.SymbolConstant
		}
		sym := core.Symbol{
			Name:           name,
			Qualname:       classQualname + "." + name,
			Kind:           kind,
			Scope:          core.ScopeClass,
			File:           v.path,
			LineStart:      lineNumber(n.StartPoint().Row),
			LineEnd:        lineNumber(n.EndPoint().Row),
			ParentQualname: classQualname,
			Snippet:        v.snippet(n),
		}
		v.symbols = append(v.symbols, sym)
	}
}

// handleNestedClass registers a nested class with class scope.
func (v *visitor) handleNestedClass(n *sitter.Node, classQualname string) {
	nameNode := n.ChildByFieldName("name")
	if nameNode == nil {
		return
	}
	className := v.nodeText(nameNode)
	nestedQualname := classQualname + "." + className

	sym := core.Symbol{
		Name:           className,
		Qualname:       nestedQualname,
		Kind:           core.SymbolClass,
		Scope:          core.ScopeClass,
		File:           v.path,
		LineStart:      lineNumber(n.StartPoint().Row),
		LineEnd:        lineNumber(n.EndPoint().Row),
		ParentQualname: classQualname,
		Snippet:        v.snippet(n),
	}
	v.symbols = append(v.symbols, sym)

	bodyNode := n.ChildByFieldName("body")
	if bodyNode != nil {
		v.walkClassBody(bodyNode, nestedQualname)
	}
}

// handleParameters emits variable symbols for method/constructor parameters.
func (v *visitor) handleParameters(paramsNode *sitter.Node, funcQualname string) {
	if paramsNode == nil {
		return
	}
	for i := 0; i < int(paramsNode.ChildCount()); i++ {
		child := paramsNode.Child(i)
		if child == nil {
			continue
		}
		switch child.Type() {
		case "formal_parameter", "spread_parameter":
			nameNode := child.ChildByFieldName("name")
			if nameNode == nil {
				continue
			}
			paramName := v.nodeText(nameNode)
			if !isIdentifier(paramName) {
				continue
			}
			sym := core.Symbol{
				Name:           paramName,
				Qualname:       funcQualname + "." + paramName,
				Kind:           core.SymbolVariable,
				Scope:          core.ScopeParam,
				File:           v.path,
				LineStart:      lineNumber(child.StartPoint().Row),
				LineEnd:        lineNumber(child.EndPoint().Row),
				ParentQualname: funcQualname,
				Snippet:        v.nodeText(child),
			}
			v.symbols = append(v.symbols, sym)
		}
	}
}

// walkFunctionBody extracts local variables and call edges from a method body.
func (v *visitor) walkFunctionBody(bodyNode *sitter.Node, funcQualname string) {
	v.walkForLocalsAndCalls(bodyNode, funcQualname)
}

// walkForLocalsAndCalls recursively walks for local declarations and calls.
func (v *visitor) walkForLocalsAndCalls(n *sitter.Node, funcQualname string) {
	if n == nil {
		return
	}
	switch n.Type() {
	case "local_variable_declaration":
		v.handleLocalVariable(n, funcQualname)
	case "method_invocation":
		v.handleMethodCall(n, funcQualname)
	}
	for i := 0; i < int(n.ChildCount()); i++ {
		child := n.Child(i)
		if child != nil {
			v.walkForLocalsAndCalls(child, funcQualname)
		}
	}
}

// handleLocalVariable emits variable symbols for local_variable_declaration.
func (v *visitor) handleLocalVariable(n *sitter.Node, funcQualname string) {
	modNode := n.ChildByFieldName("modifiers")
	isFinal := v.hasFinalModifier(modNode)

	for i := 0; i < int(n.ChildCount()); i++ {
		child := n.Child(i)
		if child == nil || child.Type() != "variable_declarator" {
			continue
		}
		nameNode := child.ChildByFieldName("name")
		if nameNode == nil {
			continue
		}
		name := v.nodeText(nameNode)
		if !isIdentifier(name) {
			continue
		}
		kind := core.SymbolVariable
		if isFinal || isAllCaps(name) {
			kind = core.SymbolConstant
		}
		sym := core.Symbol{
			Name:           name,
			Qualname:       funcQualname + "." + name,
			Kind:           kind,
			Scope:          core.ScopeLocal,
			File:           v.path,
			LineStart:      lineNumber(n.StartPoint().Row),
			LineEnd:        lineNumber(n.EndPoint().Row),
			ParentQualname: funcQualname,
			Snippet:        v.snippet(n),
		}
		v.symbols = append(v.symbols, sym)
	}
}

// handleMethodCall emits an EdgeCall for a method_invocation node.
func (v *visitor) handleMethodCall(n *sitter.Node, funcQualname string) {
	nameNode := n.ChildByFieldName("name")
	if nameNode == nil {
		return
	}
	callName := v.nodeText(nameNode)
	objNode := n.ChildByFieldName("object")
	target := callName
	if objNode != nil {
		target = v.nodeText(objNode) + "." + callName
	}
	v.edges = append(v.edges, core.Edge{
		FromQualname: funcQualname,
		ToQualname:   target,
		Kind:         core.EdgeCall,
		Resolved:     false,
	})
}
