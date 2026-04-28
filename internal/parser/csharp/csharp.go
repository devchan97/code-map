// Package csharp provides a tree-sitter based C# parser for codemap.
package csharp

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"unicode"

	sitter "github.com/smacker/go-tree-sitter"
	tscs "github.com/smacker/go-tree-sitter/csharp"

	"github.com/devchan97/code-map/internal/core"
	"github.com/devchan97/code-map/internal/parser"
)

func init() { parser.Register(&adapter{}) }

// adapter implements parser.Parser for C# source files.
type adapter struct{}

// Language returns "csharp".
func (a *adapter) Language() string { return "csharp" }

// Parse extracts symbols and edges from a C# source file.
func (a *adapter) Parse(path string, src []byte) ([]core.Symbol, []core.Edge, error) {
	sp := sitter.NewParser()
	sp.SetLanguage(tscs.GetLanguage())

	tree, err := sp.ParseCtx(context.Background(), nil, src)
	if err != nil {
		return nil, nil, fmt.Errorf("csharp parse %q: %w", path, err)
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

// visitor holds state while walking the C# AST.
type visitor struct {
	src           []byte
	path          string
	module        string
	symbols       []core.Symbol
	edges         []core.Edge
	importedNames map[string]bool
}

// fileQualname derives a dotted qualname from a repo-relative path.
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

// isAllCaps returns true if the name is ALL_CAPS.
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

// isIdentifier returns true if s is a valid C# identifier.
func isIdentifier(s string) bool {
	s = strings.TrimPrefix(s, "@")
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

// hasModifierKeyword checks if a declaration node contains a modifier with the given keyword.
// In the C# grammar, each modifier is a "modifier" node whose first child is the keyword node.
func (v *visitor) hasModifierKeyword(n *sitter.Node, keyword string) bool {
	if n == nil {
		return false
	}
	for i := 0; i < int(n.ChildCount()); i++ {
		child := n.Child(i)
		if child == nil || child.Type() != "modifier" {
			continue
		}
		// The modifier node has child nodes: e.g., [const] "const"
		for j := 0; j < int(child.ChildCount()); j++ {
			sub := child.Child(j)
			if sub != nil && sub.Type() == keyword {
				return true
			}
		}
		// Also check the modifier text directly.
		if v.nodeText(child) == keyword {
			return true
		}
	}
	return false
}

// walkRoot walks top-level declarations in a C# compilation_unit.
func (v *visitor) walkRoot(root *sitter.Node) {
	for i := 0; i < int(root.ChildCount()); i++ {
		child := root.Child(i)
		if child == nil {
			continue
		}
		switch child.Type() {
		case "using_directive":
			v.handleUsing(child)
		case "namespace_declaration", "file_scoped_namespace_declaration":
			v.handleNamespace(child)
		case "class_declaration", "struct_declaration",
			"interface_declaration", "record_declaration",
			"enum_declaration":
			v.handleClass(child, v.module)
		}
	}
}

// handleUsing processes a using_directive (C# import).
func (v *visitor) handleUsing(n *sitter.Node) {
	// using System.Collections.Generic;
	// using alias = Namespace;
	// using static Namespace.Class;
	var importedPath string
	for i := 0; i < int(n.ChildCount()); i++ {
		child := n.Child(i)
		if child == nil {
			continue
		}
		switch child.Type() {
		case "identifier":
			text := v.nodeText(child)
			if text != "using" && text != "static" {
				importedPath = text
			}
		case "qualified_name":
			importedPath = v.nodeText(child)
		case "name_equals":
			// using X = Namespace; we take the alias.
			importedPath = v.nodeText(child)
		}
	}
	if importedPath == "" {
		return
	}

	parts := strings.Split(importedPath, ".")
	localName := parts[len(parts)-1]
	if !isIdentifier(localName) {
		localName = strings.ReplaceAll(importedPath, ".", "_")
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

// handleNamespace processes a namespace_declaration.
// It uses ChildByFieldName("name") and ChildByFieldName("body").
func (v *visitor) handleNamespace(n *sitter.Node) {
	nameNode := n.ChildByFieldName("name")
	if nameNode != nil {
		v.module = v.nodeText(nameNode)
	}

	bodyNode := n.ChildByFieldName("body")
	if bodyNode == nil {
		// file-scoped namespace: scan children directly
		for i := 0; i < int(n.ChildCount()); i++ {
			child := n.Child(i)
			if child == nil {
				continue
			}
			v.walkTopDecl(child)
		}
		return
	}

	for i := 0; i < int(bodyNode.ChildCount()); i++ {
		child := bodyNode.Child(i)
		if child == nil {
			continue
		}
		v.walkTopDecl(child)
	}
}

// walkTopDecl handles a single top-level declaration within a namespace or root.
func (v *visitor) walkTopDecl(child *sitter.Node) {
	switch child.Type() {
	case "class_declaration", "struct_declaration",
		"interface_declaration", "record_declaration",
		"enum_declaration":
		v.handleClass(child, v.module)
	case "using_directive":
		v.handleUsing(child)
	case "namespace_declaration":
		v.handleNamespace(child)
	}
}

// handleClass processes class, struct, interface, record, and enum declarations.
func (v *visitor) handleClass(n *sitter.Node, parentQualname string) {
	// Name field works in C# grammar.
	nameNode := n.ChildByFieldName("name")
	if nameNode == nil {
		// Fallback: find first identifier.
		for i := 0; i < int(n.ChildCount()); i++ {
			child := n.Child(i)
			if child != nil && child.Type() == "identifier" {
				nameNode = child
				break
			}
		}
	}
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

	// Inheritance: look for base_list.
	v.handleInheritance(n, classQualname)

	// Class body: C# uses "declaration_list".
	bodyNode := n.ChildByFieldName("body")
	if bodyNode == nil {
		for i := 0; i < int(n.ChildCount()); i++ {
			child := n.Child(i)
			if child != nil && child.Type() == "declaration_list" {
				bodyNode = child
				break
			}
		}
	}
	if bodyNode != nil {
		v.walkClassBody(bodyNode, classQualname)
	}
}

// handleInheritance emits EdgeInherit for base_list entries.
func (v *visitor) handleInheritance(n *sitter.Node, classQualname string) {
	// C# grammar: base_list is a direct child of class_declaration.
	for i := 0; i < int(n.ChildCount()); i++ {
		child := n.Child(i)
		if child == nil || child.Type() != "base_list" {
			continue
		}
		// base_list children: ":", base_type nodes
		for j := 0; j < int(child.ChildCount()); j++ {
			base := child.Child(j)
			if base == nil {
				continue
			}
			switch base.Type() {
			case "identifier", "qualified_name", "generic_name":
				v.edges = append(v.edges, core.Edge{
					FromQualname: classQualname,
					ToQualname:   v.nodeText(base),
					Kind:         core.EdgeInherit,
					Resolved:     false,
				})
			}
		}
		break
	}
}

// walkClassBody processes members of a class body (declaration_list).
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
		case "property_declaration":
			v.handleProperty(child, classQualname)
		case "class_declaration", "struct_declaration",
			"interface_declaration", "record_declaration":
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

// handleField processes a field_declaration inside a class.
// The C# grammar has individual "modifier" children (not a grouped modifiers field).
func (v *visitor) handleField(n *sitter.Node, classQualname string) {
	isConst := v.hasModifierKeyword(n, "const") || v.hasModifierKeyword(n, "readonly")

	for i := 0; i < int(n.ChildCount()); i++ {
		child := n.Child(i)
		if child == nil || child.Type() != "variable_declaration" {
			continue
		}
		v.extractVariableDeclaration(child, classQualname, core.ScopeClass, isConst)
	}
}

// handleProperty processes a property_declaration inside a class.
func (v *visitor) handleProperty(n *sitter.Node, classQualname string) {
	nameNode := n.ChildByFieldName("name")
	if nameNode == nil {
		return
	}
	name := v.nodeText(nameNode)
	if !isIdentifier(name) {
		return
	}
	sym := core.Symbol{
		Name:           name,
		Qualname:       classQualname + "." + name,
		Kind:           core.SymbolVariable,
		Scope:          core.ScopeClass,
		File:           v.path,
		LineStart:      lineNumber(n.StartPoint().Row),
		LineEnd:        lineNumber(n.EndPoint().Row),
		ParentQualname: classQualname,
		Snippet:        v.snippet(n),
	}
	v.symbols = append(v.symbols, sym)
}

// handleNestedClass registers a nested class with class scope.
func (v *visitor) handleNestedClass(n *sitter.Node, classQualname string) {
	nameNode := n.ChildByFieldName("name")
	if nameNode == nil {
		for i := 0; i < int(n.ChildCount()); i++ {
			child := n.Child(i)
			if child != nil && child.Type() == "identifier" {
				nameNode = child
				break
			}
		}
	}
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
	if bodyNode == nil {
		for i := 0; i < int(n.ChildCount()); i++ {
			child := n.Child(i)
			if child != nil && child.Type() == "declaration_list" {
				bodyNode = child
				break
			}
		}
	}
	if bodyNode != nil {
		v.walkClassBody(bodyNode, nestedQualname)
	}
}

// handleParameters emits variable symbols for method/constructor parameters.
// C# uses "parameter_list" (accessed via field "parameters").
func (v *visitor) handleParameters(paramsNode *sitter.Node, funcQualname string) {
	if paramsNode == nil {
		return
	}
	for i := 0; i < int(paramsNode.ChildCount()); i++ {
		child := paramsNode.Child(i)
		if child == nil || child.Type() != "parameter" {
			continue
		}
		// Parameter field "name" holds the identifier.
		nameNode := child.ChildByFieldName("name")
		if nameNode == nil {
			// Fallback: find identifier child.
			for j := 0; j < int(child.ChildCount()); j++ {
				sub := child.Child(j)
				if sub != nil && sub.Type() == "identifier" {
					nameNode = sub
					break
				}
			}
		}
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

// walkFunctionBody extracts local variables and call edges from a method body (block).
func (v *visitor) walkFunctionBody(bodyNode *sitter.Node, funcQualname string) {
	v.walkForLocalsAndCalls(bodyNode, funcQualname)
}

// walkForLocalsAndCalls recursively walks for local declarations and calls.
func (v *visitor) walkForLocalsAndCalls(n *sitter.Node, funcQualname string) {
	if n == nil {
		return
	}
	switch n.Type() {
	case "local_declaration_statement":
		v.handleLocalDeclaration(n, funcQualname)
	case "invocation_expression":
		v.handleInvocationExpression(n, funcQualname)
	}
	for i := 0; i < int(n.ChildCount()); i++ {
		child := n.Child(i)
		if child != nil {
			v.walkForLocalsAndCalls(child, funcQualname)
		}
	}
}

// handleLocalDeclaration emits variable symbols for local_declaration_statement.
func (v *visitor) handleLocalDeclaration(n *sitter.Node, funcQualname string) {
	for i := 0; i < int(n.ChildCount()); i++ {
		child := n.Child(i)
		if child == nil || child.Type() != "variable_declaration" {
			continue
		}
		v.extractVariableDeclaration(child, funcQualname, core.ScopeLocal, false)
	}
}

// extractVariableDeclaration extracts variable names from a variable_declaration node.
func (v *visitor) extractVariableDeclaration(n *sitter.Node, qualname string, scope core.Scope, forceConst bool) {
	for i := 0; i < int(n.ChildCount()); i++ {
		child := n.Child(i)
		if child == nil || child.Type() != "variable_declarator" {
			continue
		}
		// variable_declarator field "name" holds the identifier.
		nameNode := child.ChildByFieldName("name")
		if nameNode == nil {
			// Fallback: find first identifier.
			for j := 0; j < int(child.ChildCount()); j++ {
				sub := child.Child(j)
				if sub != nil && sub.Type() == "identifier" {
					nameNode = sub
					break
				}
			}
		}
		if nameNode == nil {
			continue
		}
		name := v.nodeText(nameNode)
		if !isIdentifier(name) {
			continue
		}
		kind := core.SymbolVariable
		if forceConst || isAllCaps(name) {
			kind = core.SymbolConstant
		}
		sym := core.Symbol{
			Name:           name,
			Qualname:       qualname + "." + name,
			Kind:           kind,
			Scope:          scope,
			File:           v.path,
			LineStart:      lineNumber(n.StartPoint().Row),
			LineEnd:        lineNumber(n.EndPoint().Row),
			ParentQualname: qualname,
			Snippet:        v.snippet(n),
		}
		v.symbols = append(v.symbols, sym)
	}
}

// handleInvocationExpression emits an EdgeCall for an invocation_expression node.
func (v *visitor) handleInvocationExpression(n *sitter.Node, funcQualname string) {
	funcNode := n.ChildByFieldName("function")
	if funcNode == nil {
		return
	}
	target := v.nodeText(funcNode)
	v.edges = append(v.edges, core.Edge{
		FromQualname: funcQualname,
		ToQualname:   target,
		Kind:         core.EdgeCall,
		Resolved:     false,
	})
}
