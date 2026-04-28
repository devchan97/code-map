// Package ts provides tree-sitter based parsers for JavaScript, TypeScript, and TSX.
// It registers three language tags from a single file:
//   - "js"  → extensions .js, .jsx, .mjs, .cjs
//   - "ts"  → extension .ts
//   - "tsx" → extension .tsx
package ts

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"unicode"

	sitter "github.com/smacker/go-tree-sitter"
	tsjs "github.com/smacker/go-tree-sitter/javascript"
	tsts "github.com/smacker/go-tree-sitter/typescript/typescript"
	tstsx "github.com/smacker/go-tree-sitter/typescript/tsx"

	"github.com/devchan97/code-map/internal/core"
	"github.com/devchan97/code-map/internal/parser"
)

func init() {
	parser.Register(&adapter{lang: "js", getLang: tsjs.GetLanguage})
	parser.Register(&adapter{lang: "ts", getLang: tsts.GetLanguage})
	parser.Register(&adapter{lang: "tsx", getLang: tstsx.GetLanguage})
}

// adapter implements parser.Parser for a single JS/TS/TSX language variant.
type adapter struct {
	lang    string
	getLang func() *sitter.Language
}

// Language returns the language tag ("js", "ts", or "tsx").
func (a *adapter) Language() string { return a.lang }

// Parse extracts symbols and edges from a JS/TS/TSX source file.
func (a *adapter) Parse(path string, src []byte) ([]core.Symbol, []core.Edge, error) {
	sp := sitter.NewParser()
	sp.SetLanguage(a.getLang())

	tree, err := sp.ParseCtx(context.Background(), nil, src)
	if err != nil {
		return nil, nil, fmt.Errorf("%s parse %q: %w", a.lang, path, err)
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

// visitor holds state while walking the JS/TS/TSX AST.
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

// isIdentifier returns true if s is a valid JS/TS identifier.
func isIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if i == 0 {
			if !unicode.IsLetter(r) && r != '_' && r != '$' {
				return false
			}
		} else {
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '$' {
				return false
			}
		}
	}
	return true
}

// walkRoot walks top-level statements in the program/module node.
func (v *visitor) walkRoot(root *sitter.Node) {
	for i := 0; i < int(root.ChildCount()); i++ {
		child := root.Child(i)
		if child == nil {
			continue
		}
		v.walkTopStatement(child)
	}
}

// walkTopStatement processes a single top-level statement.
func (v *visitor) walkTopStatement(n *sitter.Node) {
	switch n.Type() {
	case "import_statement":
		v.handleImport(n)
	case "function_declaration":
		v.handleFunction(n, v.module)
	case "class_declaration":
		v.handleClass(n, v.module)
	case "lexical_declaration", "variable_declaration":
		v.handleTopLevelVarDecl(n)
	case "export_statement":
		v.handleExport(n)
	case "expression_statement":
		// e.g. module.exports = ...
		v.walkForCalls(n, v.module)
	}
}

// handleImport processes an import_statement.
func (v *visitor) handleImport(n *sitter.Node) {
	// import defaultExport from "module-name";
	// import { named } from "module-name";
	// import * as alias from "module-name";
	source := ""
	for i := 0; i < int(n.ChildCount()); i++ {
		child := n.Child(i)
		if child == nil {
			continue
		}
		if child.Type() == "string" {
			raw := v.nodeText(child)
			source = strings.Trim(raw, `"'`)
		}
	}

	// Collect imported names.
	var localNames []string
	for i := 0; i < int(n.ChildCount()); i++ {
		child := n.Child(i)
		if child == nil {
			continue
		}
		switch child.Type() {
		case "identifier":
			// default import
			localNames = append(localNames, v.nodeText(child))
		case "namespace_import":
			// import * as alias
			for j := 0; j < int(child.ChildCount()); j++ {
				sub := child.Child(j)
				if sub != nil && sub.Type() == "identifier" {
					localNames = append(localNames, v.nodeText(sub))
				}
			}
		case "named_imports":
			// import { a, b as c }
			for j := 0; j < int(child.ChildCount()); j++ {
				sub := child.Child(j)
				if sub == nil || sub.Type() != "import_specifier" {
					continue
				}
				aliasNode := sub.ChildByFieldName("alias")
				nameNode := sub.ChildByFieldName("name")
				if aliasNode != nil {
					localNames = append(localNames, v.nodeText(aliasNode))
				} else if nameNode != nil {
					localNames = append(localNames, v.nodeText(nameNode))
				}
			}
		}
	}

	if len(localNames) == 0 && source != "" {
		// bare import
		parts := strings.Split(source, "/")
		localName := parts[len(parts)-1]
		localName = strings.TrimPrefix(localName, "@")
		if isIdentifier(localName) {
			localNames = append(localNames, localName)
		}
	}

	for _, localName := range localNames {
		if !isIdentifier(localName) {
			continue
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

		target := source
		if target == "" {
			target = localName
		}
		v.edges = append(v.edges, core.Edge{
			FromQualname: v.module,
			ToQualname:   target,
			Kind:         core.EdgeImport,
			Resolved:     false,
		})
	}
}

// handleExport processes an export_statement, delegating to the inner declaration.
func (v *visitor) handleExport(n *sitter.Node) {
	for i := 0; i < int(n.ChildCount()); i++ {
		child := n.Child(i)
		if child == nil {
			continue
		}
		switch child.Type() {
		case "function_declaration":
			v.handleFunction(child, v.module)
		case "class_declaration":
			v.handleClass(child, v.module)
		case "lexical_declaration", "variable_declaration":
			v.handleTopLevelVarDecl(child)
		}
	}
}

// handleFunction processes a function_declaration or function node.
func (v *visitor) handleFunction(n *sitter.Node, parentQualname string) {
	nameNode := n.ChildByFieldName("name")
	if nameNode == nil {
		return
	}
	funcName := v.nodeText(nameNode)
	if !isIdentifier(funcName) {
		return
	}
	funcQualname := parentQualname + "." + funcName

	sym := core.Symbol{
		Name:      funcName,
		Qualname:  funcQualname,
		Kind:      core.SymbolFunction,
		Scope:     core.ScopeGlobal,
		File:      v.path,
		LineStart: lineNumber(n.StartPoint().Row),
		LineEnd:   lineNumber(n.EndPoint().Row),
		Snippet:   v.snippet(n),
	}
	v.symbols = append(v.symbols, sym)

	paramsNode := n.ChildByFieldName("parameters")
	v.handleParameters(paramsNode, funcQualname)

	bodyNode := n.ChildByFieldName("body")
	if bodyNode != nil {
		v.walkFunctionBody(bodyNode, funcQualname)
	}
}

// handleClass processes a class_declaration.
func (v *visitor) handleClass(n *sitter.Node, parentQualname string) {
	nameNode := n.ChildByFieldName("name")
	if nameNode == nil {
		return
	}
	className := v.nodeText(nameNode)
	if !isIdentifier(className) {
		return
	}
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

	// Inheritance: JS grammar uses "class_heritage" as a direct child node type,
	// not a named field. Walk children to find it.
	for i := 0; i < int(n.ChildCount()); i++ {
		child := n.Child(i)
		if child != nil && child.Type() == "class_heritage" {
			v.handleClassHeritage(child, classQualname)
		}
	}

	bodyNode := n.ChildByFieldName("body")
	if bodyNode != nil {
		v.walkClassBody(bodyNode, classQualname)
	}
}

// handleClassHeritage emits EdgeInherit for class_heritage / extends clauses.
// JS:  class_heritage → "extends" identifier | member_expression
// TS:  class_heritage → extends_clause → identifier
//                     → implements_clause → type_identifier
func (v *visitor) handleClassHeritage(n *sitter.Node, classQualname string) {
	v.extractHeritageEdges(n, classQualname)
}

// extractHeritageEdges recursively extracts base type identifiers from a heritage node.
func (v *visitor) extractHeritageEdges(n *sitter.Node, classQualname string) {
	if n == nil {
		return
	}
	switch n.Type() {
	case "identifier", "type_identifier", "member_expression":
		name := v.nodeText(n)
		if name != "" && name != "extends" && name != "implements" {
			v.edges = append(v.edges, core.Edge{
				FromQualname: classQualname,
				ToQualname:   name,
				Kind:         core.EdgeInherit,
				Resolved:     false,
			})
		}
		return
	}
	// Recurse into extends_clause, implements_clause, and other containers.
	for i := 0; i < int(n.ChildCount()); i++ {
		child := n.Child(i)
		if child != nil {
			v.extractHeritageEdges(child, classQualname)
		}
	}
}

// walkClassBody processes members of a class body.
func (v *visitor) walkClassBody(bodyNode *sitter.Node, classQualname string) {
	for i := 0; i < int(bodyNode.ChildCount()); i++ {
		child := bodyNode.Child(i)
		if child == nil {
			continue
		}
		switch child.Type() {
		case "method_definition":
			v.handleMethodDefinition(child, classQualname)
		case "public_field_definition", "field_definition":
			v.handleFieldDefinition(child, classQualname)
		}
	}
}

// handleMethodDefinition processes a method_definition inside a class.
func (v *visitor) handleMethodDefinition(n *sitter.Node, classQualname string) {
	nameNode := n.ChildByFieldName("name")
	if nameNode == nil {
		return
	}
	methodName := v.nodeText(nameNode)
	if !isIdentifier(methodName) {
		return
	}
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

// handleFieldDefinition processes a public_field_definition inside a class.
func (v *visitor) handleFieldDefinition(n *sitter.Node, classQualname string) {
	nameNode := n.ChildByFieldName("name")
	if nameNode == nil {
		return
	}
	name := v.nodeText(nameNode)
	if !isIdentifier(name) {
		return
	}

	// Check for static keyword indicating possible constant.
	isStatic := false
	for i := 0; i < int(n.ChildCount()); i++ {
		child := n.Child(i)
		if child != nil && child.Type() == "static" {
			isStatic = true
		}
	}
	kind := core.SymbolVariable
	if isAllCaps(name) && isStatic {
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

// handleTopLevelVarDecl processes lexical_declaration / variable_declaration at module scope.
func (v *visitor) handleTopLevelVarDecl(n *sitter.Node) {
	// Determine if const.
	isConst := false
	for i := 0; i < int(n.ChildCount()); i++ {
		child := n.Child(i)
		if child != nil && v.nodeText(child) == "const" {
			isConst = true
		}
	}

	for i := 0; i < int(n.ChildCount()); i++ {
		child := n.Child(i)
		if child == nil || child.Type() != "variable_declarator" {
			continue
		}
		nameNode := child.ChildByFieldName("name")
		if nameNode == nil {
			continue
		}
		name := v.extractPatternName(nameNode)
		if name == "" || !isIdentifier(name) {
			continue
		}
		kind := core.SymbolVariable
		if isConst || isAllCaps(name) {
			kind = core.SymbolConstant
		}

		// Check if the value is a function/arrow function → emit as function.
		valueNode := child.ChildByFieldName("value")
		if valueNode != nil && (valueNode.Type() == "function" ||
			valueNode.Type() == "arrow_function" ||
			valueNode.Type() == "function_expression") {
			v.handleFunctionExpression(name, valueNode, v.module)
			continue
		}

		sym := core.Symbol{
			Name:      name,
			Qualname:  v.module + "." + name,
			Kind:      kind,
			Scope:     core.ScopeGlobal,
			File:      v.path,
			LineStart: lineNumber(n.StartPoint().Row),
			LineEnd:   lineNumber(n.EndPoint().Row),
			Snippet:   v.snippet(n),
		}
		v.symbols = append(v.symbols, sym)
	}
}

// handleFunctionExpression handles `const fn = () => {}` style declarations.
func (v *visitor) handleFunctionExpression(name string, n *sitter.Node, parentQualname string) {
	funcQualname := parentQualname + "." + name

	sym := core.Symbol{
		Name:      name,
		Qualname:  funcQualname,
		Kind:      core.SymbolFunction,
		Scope:     core.ScopeGlobal,
		File:      v.path,
		LineStart: lineNumber(n.StartPoint().Row),
		LineEnd:   lineNumber(n.EndPoint().Row),
		Snippet:   v.snippet(n),
	}
	v.symbols = append(v.symbols, sym)

	paramsNode := n.ChildByFieldName("parameters") // for arrow_function: "parameter"
	if paramsNode == nil {
		paramsNode = n.ChildByFieldName("parameter")
	}
	v.handleParameters(paramsNode, funcQualname)

	bodyNode := n.ChildByFieldName("body")
	if bodyNode != nil {
		v.walkFunctionBody(bodyNode, funcQualname)
	}
}

// extractPatternName extracts the first identifier from a possibly complex pattern.
func (v *visitor) extractPatternName(n *sitter.Node) string {
	if n == nil {
		return ""
	}
	if n.Type() == "identifier" {
		return v.nodeText(n)
	}
	// For destructuring patterns, return the first identifier.
	for i := 0; i < int(n.ChildCount()); i++ {
		child := n.Child(i)
		if child != nil && child.Type() == "identifier" {
			return v.nodeText(child)
		}
	}
	return ""
}

// handleParameters emits param symbols for a formal_parameters node.
func (v *visitor) handleParameters(paramsNode *sitter.Node, funcQualname string) {
	if paramsNode == nil {
		return
	}
	for i := 0; i < int(paramsNode.ChildCount()); i++ {
		child := paramsNode.Child(i)
		if child == nil {
			continue
		}
		paramName := ""
		switch child.Type() {
		case "identifier":
			paramName = v.nodeText(child)
		case "assignment_pattern":
			leftNode := child.ChildByFieldName("left")
			if leftNode != nil && leftNode.Type() == "identifier" {
				paramName = v.nodeText(leftNode)
			}
		case "rest_pattern":
			for j := 0; j < int(child.ChildCount()); j++ {
				sub := child.Child(j)
				if sub != nil && sub.Type() == "identifier" {
					paramName = v.nodeText(sub)
					break
				}
			}
		case "required_parameter", "optional_parameter":
			// TypeScript-specific parameter types
			patternNode := child.ChildByFieldName("pattern")
			if patternNode != nil && patternNode.Type() == "identifier" {
				paramName = v.nodeText(patternNode)
			}
		}

		if paramName == "" || !isIdentifier(paramName) {
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

// walkFunctionBody extracts local variables and call edges from a function body.
func (v *visitor) walkFunctionBody(bodyNode *sitter.Node, funcQualname string) {
	// Iterate direct children of the statement_block body.
	for i := 0; i < int(bodyNode.ChildCount()); i++ {
		child := bodyNode.Child(i)
		if child == nil {
			continue
		}
		switch child.Type() {
		case "lexical_declaration", "variable_declaration":
			v.handleLocalVarDecl(child, funcQualname)
		}
		// Walk all descendants for call edges.
		v.walkForCalls(child, funcQualname)
	}
}

// handleLocalVarDecl emits variable symbols for local declarations in a function body.
func (v *visitor) handleLocalVarDecl(n *sitter.Node, funcQualname string) {
	isConst := false
	for i := 0; i < int(n.ChildCount()); i++ {
		child := n.Child(i)
		if child != nil && v.nodeText(child) == "const" {
			isConst = true
		}
	}

	for i := 0; i < int(n.ChildCount()); i++ {
		child := n.Child(i)
		if child == nil || child.Type() != "variable_declarator" {
			continue
		}
		nameNode := child.ChildByFieldName("name")
		if nameNode == nil {
			continue
		}
		name := v.extractPatternName(nameNode)
		if name == "" || !isIdentifier(name) {
			continue
		}
		kind := core.SymbolVariable
		if isConst || isAllCaps(name) {
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

// walkForCalls recursively walks a node's descendants looking for call expressions.
func (v *visitor) walkForCalls(n *sitter.Node, enclosingQualname string) {
	if n == nil {
		return
	}
	if n.Type() == "call_expression" {
		funcNode := n.ChildByFieldName("function")
		if funcNode != nil {
			switch funcNode.Type() {
			case "identifier":
				callTarget := v.nodeText(funcNode)
				v.edges = append(v.edges, core.Edge{
					FromQualname: enclosingQualname,
					ToQualname:   callTarget,
					Kind:         core.EdgeCall,
					Resolved:     false,
				})
			case "member_expression":
				callTarget := v.nodeText(funcNode)
				v.edges = append(v.edges, core.Edge{
					FromQualname: enclosingQualname,
					ToQualname:   callTarget,
					Kind:         core.EdgeCall,
					Resolved:     false,
				})
			}
		}
	}

	for i := 0; i < int(n.ChildCount()); i++ {
		child := n.Child(i)
		if child != nil {
			v.walkForCalls(child, enclosingQualname)
		}
	}
}
