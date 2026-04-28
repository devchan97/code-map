// Package cpp provides a tree-sitter based C++ parser for codemap.
package cpp

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"unicode"

	sitter "github.com/smacker/go-tree-sitter"
	tscpp "github.com/smacker/go-tree-sitter/cpp"

	"github.com/devchan97/code-map/internal/core"
	"github.com/devchan97/code-map/internal/parser"
)

func init() { parser.Register(&adapter{}) }

// adapter implements parser.Parser for C++ source files.
type adapter struct{}

// Language returns "cpp".
func (a *adapter) Language() string { return "cpp" }

// Parse extracts symbols and edges from a C++ source file.
func (a *adapter) Parse(path string, src []byte) ([]core.Symbol, []core.Edge, error) {
	sp := sitter.NewParser()
	sp.SetLanguage(tscpp.GetLanguage())

	tree, err := sp.ParseCtx(context.Background(), nil, src)
	if err != nil {
		return nil, nil, fmt.Errorf("cpp parse %q: %w", path, err)
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

// visitor holds state while walking the C++ AST.
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

// isIdentifier returns true if s is a valid C++ identifier.
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

// walkRoot walks top-level declarations in a C++ translation_unit.
func (v *visitor) walkRoot(root *sitter.Node) {
	for i := 0; i < int(root.ChildCount()); i++ {
		child := root.Child(i)
		if child == nil {
			continue
		}
		v.walkTopLevel(child, v.module)
	}
}

// walkTopLevel dispatches top-level and namespace-level declarations.
func (v *visitor) walkTopLevel(n *sitter.Node, qualname string) {
	switch n.Type() {
	case "preproc_include":
		v.handleInclude(n)
	case "namespace_definition":
		v.handleNamespace(n, qualname)
	case "function_definition":
		v.handleFunction(n, qualname, core.ScopeGlobal, "")
	case "class_specifier", "struct_specifier":
		v.handleClass(n, qualname)
	case "declaration":
		v.handleDeclaration(n, qualname, core.ScopeGlobal)
	case "template_declaration":
		v.handleTemplate(n, qualname)
	}
}

// handleInclude processes a #include directive as an import.
func (v *visitor) handleInclude(n *sitter.Node) {
	for i := 0; i < int(n.ChildCount()); i++ {
		child := n.Child(i)
		if child == nil {
			continue
		}
		if child.Type() == "string_literal" || child.Type() == "system_lib_string" {
			raw := v.nodeText(child)
			raw = strings.Trim(raw, "<>\"")
			localName := filepath.Base(raw)
			localName = strings.TrimSuffix(localName, filepath.Ext(localName))
			if !isIdentifier(localName) {
				localName = strings.ReplaceAll(localName, ".", "_")
				localName = strings.ReplaceAll(localName, "-", "_")
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
				ToQualname:   raw,
				Kind:         core.EdgeImport,
				Resolved:     false,
			})
			return
		}
	}
}

// handleNamespace processes a namespace_definition.
// In C++ grammar: field "name" → namespace_identifier, field "body" → declaration_list.
func (v *visitor) handleNamespace(n *sitter.Node, parentQualname string) {
	nameNode := n.ChildByFieldName("name")
	nsQualname := parentQualname
	if nameNode != nil {
		nsName := v.nodeText(nameNode)
		if nsName != "" {
			nsQualname = parentQualname + "." + nsName
		}
	}

	bodyNode := n.ChildByFieldName("body")
	if bodyNode == nil {
		return
	}
	for i := 0; i < int(bodyNode.ChildCount()); i++ {
		child := bodyNode.Child(i)
		if child != nil {
			v.walkTopLevel(child, nsQualname)
		}
	}
}

// handleTemplate unwraps a template_declaration and processes the inner declaration.
func (v *visitor) handleTemplate(n *sitter.Node, qualname string) {
	for i := 0; i < int(n.ChildCount()); i++ {
		child := n.Child(i)
		if child == nil {
			continue
		}
		switch child.Type() {
		case "function_definition":
			v.handleFunction(child, qualname, core.ScopeGlobal, "")
		case "class_specifier", "struct_specifier":
			v.handleClass(child, qualname)
		case "declaration":
			v.handleDeclaration(child, qualname, core.ScopeGlobal)
		}
	}
}

// handleFunction processes a function_definition at namespace or class scope.
// scope and classQualname are used when the function is defined outside the class (Foo::bar).
func (v *visitor) handleFunction(n *sitter.Node, parentQualname string, defScope core.Scope, classQualname string) {
	declaratorNode := n.ChildByFieldName("declarator")
	if declaratorNode == nil {
		return
	}

	funcName, classScope, resolvedClassQualname := v.extractFunctionName(declaratorNode, parentQualname)
	if funcName == "" {
		return
	}

	kind := core.SymbolFunction
	funcScope := defScope
	var parentQN string
	if classScope != "" {
		kind = core.SymbolMethod
		funcScope = core.ScopeClass
		parentQN = resolvedClassQualname
	}

	funcQualname := parentQualname + "." + funcName
	if classScope != "" {
		funcQualname = parentQualname + "." + classScope + "." + funcName
	}

	sym := core.Symbol{
		Name:           funcName,
		Qualname:       funcQualname,
		Kind:           kind,
		Scope:          funcScope,
		File:           v.path,
		LineStart:      lineNumber(n.StartPoint().Row),
		LineEnd:        lineNumber(n.EndPoint().Row),
		ParentQualname: parentQN,
		Snippet:        v.snippet(n),
	}
	v.symbols = append(v.symbols, sym)

	v.handleParametersFromDeclarator(declaratorNode, funcQualname)

	bodyNode := n.ChildByFieldName("body")
	if bodyNode != nil {
		v.walkFunctionBody(bodyNode, funcQualname)
	}
}

// handleClassMethod processes a function_definition inside a class body (inline method).
func (v *visitor) handleClassMethod(n *sitter.Node, classQualname string) {
	declaratorNode := n.ChildByFieldName("declarator")
	if declaratorNode == nil {
		return
	}
	// For inline methods, the function name is a simple identifier or field_identifier.
	funcName := v.extractInlineMethodName(declaratorNode)
	if funcName == "" {
		return
	}
	methodQualname := classQualname + "." + funcName

	sym := core.Symbol{
		Name:           funcName,
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

	v.handleParametersFromDeclarator(declaratorNode, methodQualname)

	bodyNode := n.ChildByFieldName("body")
	if bodyNode != nil {
		v.walkFunctionBody(bodyNode, methodQualname)
	}
}

// extractInlineMethodName extracts the method name from an inline (inside class) function declarator.
func (v *visitor) extractInlineMethodName(n *sitter.Node) string {
	if n == nil {
		return ""
	}
	switch n.Type() {
	case "function_declarator":
		inner := n.ChildByFieldName("declarator")
		return v.extractInlineMethodName(inner)
	case "field_identifier", "identifier":
		return v.nodeText(n)
	case "destructor_name":
		return v.nodeText(n)
	case "operator_name":
		return "operator_" + strings.TrimPrefix(v.nodeText(n), "operator")
	case "qualified_identifier":
		nameNode := n.ChildByFieldName("name")
		if nameNode != nil {
			return v.nodeText(nameNode)
		}
	case "pointer_declarator", "reference_declarator":
		for i := 0; i < int(n.ChildCount()); i++ {
			child := n.Child(i)
			if child != nil {
				name := v.extractInlineMethodName(child)
				if name != "" {
					return name
				}
			}
		}
	}
	return ""
}

// extractFunctionName extracts the function name and optional class scope from a declarator.
// Returns (funcName, classScope, classQualname).
func (v *visitor) extractFunctionName(n *sitter.Node, parentQualname string) (string, string, string) {
	if n == nil {
		return "", "", ""
	}
	switch n.Type() {
	case "function_declarator":
		inner := n.ChildByFieldName("declarator")
		return v.extractFunctionName(inner, parentQualname)
	case "qualified_identifier":
		scopeNode := n.ChildByFieldName("scope")
		nameNode := n.ChildByFieldName("name")
		scope := ""
		classQualname := ""
		if scopeNode != nil {
			scope = v.nodeText(scopeNode)
			classQualname = parentQualname + "." + scope
		}
		name := ""
		if nameNode != nil {
			name = v.nodeText(nameNode)
		}
		return name, scope, classQualname
	case "identifier", "field_identifier":
		return v.nodeText(n), "", ""
	case "destructor_name":
		return v.nodeText(n), "", ""
	case "operator_name":
		return "operator_" + strings.TrimPrefix(v.nodeText(n), "operator"), "", ""
	case "pointer_declarator", "reference_declarator":
		for i := 0; i < int(n.ChildCount()); i++ {
			child := n.Child(i)
			if child != nil {
				name, scope, cq := v.extractFunctionName(child, parentQualname)
				if name != "" {
					return name, scope, cq
				}
			}
		}
	}
	return "", "", ""
}

// handleParametersFromDeclarator finds the function_declarator's parameter_list.
func (v *visitor) handleParametersFromDeclarator(n *sitter.Node, funcQualname string) {
	if n == nil {
		return
	}
	if n.Type() == "function_declarator" {
		paramsNode := n.ChildByFieldName("parameters")
		v.handleParameters(paramsNode, funcQualname)
		return
	}
	for i := 0; i < int(n.ChildCount()); i++ {
		child := n.Child(i)
		if child != nil {
			v.handleParametersFromDeclarator(child, funcQualname)
		}
	}
}

// handleParameters emits param symbols for a parameter_list.
func (v *visitor) handleParameters(paramsNode *sitter.Node, funcQualname string) {
	if paramsNode == nil {
		return
	}
	for i := 0; i < int(paramsNode.ChildCount()); i++ {
		child := paramsNode.Child(i)
		if child == nil {
			continue
		}
		if child.Type() == "parameter_declaration" {
			declarator := child.ChildByFieldName("declarator")
			paramName := v.simpleDeclaratorName(declarator)
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
}

// simpleDeclaratorName extracts the identifier name from a declarator node.
func (v *visitor) simpleDeclaratorName(n *sitter.Node) string {
	if n == nil {
		return ""
	}
	switch n.Type() {
	case "identifier", "field_identifier":
		return v.nodeText(n)
	case "pointer_declarator", "reference_declarator", "abstract_reference_declarator":
		for i := 0; i < int(n.ChildCount()); i++ {
			child := n.Child(i)
			if child != nil {
				name := v.simpleDeclaratorName(child)
				if name != "" {
					return name
				}
			}
		}
	}
	return ""
}

// handleClass processes a class_specifier or struct_specifier.
// In C++ grammar: field "name" → type_identifier, field "body" → field_declaration_list.
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

	// C++ class body field is "body" → field_declaration_list.
	bodyNode := n.ChildByFieldName("body")
	if bodyNode != nil {
		v.walkClassBody(bodyNode, classQualname)
	}
}

// handleInheritance emits EdgeInherit for base_class_clause entries.
func (v *visitor) handleInheritance(n *sitter.Node, classQualname string) {
	for i := 0; i < int(n.ChildCount()); i++ {
		child := n.Child(i)
		if child == nil || child.Type() != "base_class_clause" {
			continue
		}
		for j := 0; j < int(child.ChildCount()); j++ {
			base := child.Child(j)
			if base == nil {
				continue
			}
			switch base.Type() {
			case "type_identifier", "qualified_identifier":
				v.edges = append(v.edges, core.Edge{
					FromQualname: classQualname,
					ToQualname:   v.nodeText(base),
					Kind:         core.EdgeInherit,
					Resolved:     false,
				})
			}
		}
	}
}

// walkClassBody processes members of a field_declaration_list (C++ class body).
func (v *visitor) walkClassBody(bodyNode *sitter.Node, classQualname string) {
	for i := 0; i < int(bodyNode.ChildCount()); i++ {
		child := bodyNode.Child(i)
		if child == nil {
			continue
		}
		switch child.Type() {
		case "function_definition":
			v.handleClassMethod(child, classQualname)
		case "field_declaration":
			v.handleFieldDeclaration(child, classQualname)
		case "class_specifier", "struct_specifier":
			v.handleNestedClass(child, classQualname)
		case "access_specifier":
			// skip public:/private:/protected:
		case "declaration":
			v.handleDeclaration(child, classQualname, core.ScopeClass)
		}
	}
}

// handleFieldDeclaration processes a field_declaration inside a class body.
func (v *visitor) handleFieldDeclaration(n *sitter.Node, classQualname string) {
	// Check for static const qualifier.
	isConst := false
	for i := 0; i < int(n.ChildCount()); i++ {
		child := n.Child(i)
		if child != nil && child.Type() == "type_qualifier" && v.nodeText(child) == "const" {
			isConst = true
		}
	}

	// Find field_identifier or identifier children (the field name).
	for i := 0; i < int(n.ChildCount()); i++ {
		child := n.Child(i)
		if child == nil {
			continue
		}
		switch child.Type() {
		case "field_identifier", "identifier":
			name := v.nodeText(child)
			if !isIdentifier(name) {
				continue
			}
			kind := core.SymbolVariable
			if isConst || isAllCaps(name) {
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

// handleTopDeclaration processes a top-level declaration (global variable or const).
func (v *visitor) handleDeclaration(n *sitter.Node, qualname string, scope core.Scope) {
	// Check for const qualifier in type specifiers.
	isConst := false
	for i := 0; i < int(n.ChildCount()); i++ {
		child := n.Child(i)
		if child != nil && child.Type() == "type_qualifier" && v.nodeText(child) == "const" {
			isConst = true
		}
	}

	for i := 0; i < int(n.ChildCount()); i++ {
		child := n.Child(i)
		if child == nil {
			continue
		}
		switch child.Type() {
		case "init_declarator":
			declaratorNode := child.ChildByFieldName("declarator")
			name := v.simpleDeclaratorName(declaratorNode)
			if name == "" {
				name = v.extractFirstIdentifier(child)
			}
			if name == "" || !isIdentifier(name) {
				continue
			}
			kind := core.SymbolVariable
			if isConst || isAllCaps(name) {
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
		case "identifier", "field_identifier":
			// Simple declaration without init.
			name := v.nodeText(child)
			if !isIdentifier(name) {
				continue
			}
			kind := core.SymbolVariable
			if isConst || isAllCaps(name) {
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
}

// extractFirstIdentifier finds the first identifier in a subtree.
func (v *visitor) extractFirstIdentifier(n *sitter.Node) string {
	if n == nil {
		return ""
	}
	if n.Type() == "identifier" || n.Type() == "field_identifier" {
		return v.nodeText(n)
	}
	for i := 0; i < int(n.ChildCount()); i++ {
		child := n.Child(i)
		if child != nil {
			name := v.extractFirstIdentifier(child)
			if name != "" {
				return name
			}
		}
	}
	return ""
}

// walkFunctionBody extracts local variables and call edges from a function body.
func (v *visitor) walkFunctionBody(bodyNode *sitter.Node, funcQualname string) {
	v.walkForLocalsAndCalls(bodyNode, funcQualname)
}

// walkForLocalsAndCalls recursively walks for local declarations and calls.
func (v *visitor) walkForLocalsAndCalls(n *sitter.Node, funcQualname string) {
	if n == nil {
		return
	}
	switch n.Type() {
	case "declaration":
		v.handleDeclaration(n, funcQualname, core.ScopeLocal)
	case "call_expression":
		v.handleCallExpression(n, funcQualname)
	}
	for i := 0; i < int(n.ChildCount()); i++ {
		child := n.Child(i)
		if child != nil {
			v.walkForLocalsAndCalls(child, funcQualname)
		}
	}
}

// handleCallExpression emits an EdgeCall for a call_expression node.
func (v *visitor) handleCallExpression(n *sitter.Node, funcQualname string) {
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
