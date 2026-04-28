// Package python provides a tree-sitter based Python parser for codemap.
package python

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	sitter "github.com/smacker/go-tree-sitter"
	tspython "github.com/smacker/go-tree-sitter/python"

	"github.com/devchan97/code-map/internal/core"
)

// Parser implements parser.Parser for Python source files.
type Parser struct{}

// New returns a Python Parser.
func New() *Parser { return &Parser{} }

// Language returns "python".
func (p *Parser) Language() string { return "python" }

// Parse extracts symbols and edges from a Python source file.
// path should be repo-relative and forward-slash separated.
// src is the raw file bytes.
func (p *Parser) Parse(path string, src []byte) (symbols []core.Symbol, edges []core.Edge, err error) {
	sp := sitter.NewParser()
	sp.SetLanguage(tspython.GetLanguage())

	tree, parseErr := sp.ParseCtx(context.Background(), nil, src)
	if parseErr != nil {
		return nil, nil, fmt.Errorf("python parse %q: %w", path, parseErr)
	}

	root := tree.RootNode()
	module := moduleQualname(path)

	v := &visitor{
		src:           src,
		path:          path,
		module:        module,
		importedNames: make(map[string]bool),
	}
	v.walkModule(root)

	return v.symbols, v.edges, nil
}

// visitor holds parsing state while walking the tree-sitter AST.
type visitor struct {
	src           []byte
	path          string
	module        string
	symbols       []core.Symbol
	edges         []core.Edge
	importedNames map[string]bool // names brought in via import statements
}

// moduleQualname derives the dotted module qualname from a repo-relative path.
// app/billing/totals.py  → app.billing.totals
// app/billing/__init__.py → app.billing
func moduleQualname(path string) string {
	// Normalise to forward slashes and strip leading ./
	path = strings.ReplaceAll(path, "\\", "/")
	path = strings.TrimPrefix(path, "./")

	// Strip extension
	ext := filepath.Ext(path)
	noExt := strings.TrimSuffix(path, ext)

	// Replace / with .
	dotted := strings.ReplaceAll(noExt, "/", ".")

	// Drop trailing .__init__
	dotted = strings.TrimSuffix(dotted, ".__init__")

	return dotted
}

// nodeText returns the source text for a node.
func (v *visitor) nodeText(n *sitter.Node) string {
	if n == nil {
		return ""
	}
	return string(v.src[n.StartByte():n.EndByte()])
}

// snippet returns the source slice for the node, capped at 10 lines.
func (v *visitor) snippet(n *sitter.Node) string {
	text := string(v.src[n.StartByte():n.EndByte()])
	lines := strings.Split(text, "\n")
	if len(lines) > 10 {
		lines = lines[:10]
		lines = append(lines, "…")
	}
	return strings.Join(lines, "\n")
}

// lineNumber converts a 0-based row to 1-based line number.
func lineNumber(row uint32) int { return int(row) + 1 }

// docstring extracts the first string literal from the body of a function or class.
// Returns "" if not found.
func (v *visitor) docstring(bodyNode *sitter.Node) string {
	if bodyNode == nil {
		return ""
	}
	for i := 0; i < int(bodyNode.ChildCount()); i++ {
		child := bodyNode.Child(i)
		if child == nil {
			continue
		}
		// expression_statement containing a string
		if child.Type() == "expression_statement" {
			if child.ChildCount() > 0 {
				strNode := child.Child(0)
				if strNode != nil && strNode.Type() == "string" {
					raw := v.nodeText(strNode)
					return cleanDocstring(raw)
				}
				// concatenated_string
				if strNode != nil && strNode.Type() == "concatenated_string" {
					raw := v.nodeText(strNode)
					return cleanDocstring(raw)
				}
			}
		}
		// string directly under body (some grammars)
		if child.Type() == "string" {
			raw := v.nodeText(child)
			return cleanDocstring(raw)
		}
		// Stop at first non-docstring statement
		break
	}
	return ""
}

var wsCollapser = regexp.MustCompile(`\s+`)

// cleanDocstring strips triple-quote delimiters, unquotes, and collapses whitespace.
func cleanDocstring(raw string) string {
	s := raw
	for _, q := range []string{`"""`, `'''`, `"`, `'`} {
		if strings.HasPrefix(s, q) && strings.HasSuffix(s, q) && len(s) >= 2*len(q) {
			s = s[len(q) : len(s)-len(q)]
			break
		}
	}
	s = wsCollapser.ReplaceAllString(strings.TrimSpace(s), " ")
	return s
}

// isAllCaps returns true if name is a valid ALL_CAPS identifier (letters, digits, underscore,
// starts with letter or underscore, at least one uppercase letter, no lowercase letters).
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

// walkModule iterates over top-level statements in the module node.
func (v *visitor) walkModule(module *sitter.Node) {
	for i := 0; i < int(module.ChildCount()); i++ {
		child := module.Child(i)
		if child == nil {
			continue
		}
		switch child.Type() {
		case "function_definition":
			v.handleTopFunction(child)
		case "class_definition":
			v.handleClass(child)
		case "expression_statement":
			v.handleModuleAssignment(child)
		case "assignment":
			v.handleModuleAssignmentNode(child)
		case "import_statement":
			v.handleImport(child, v.module)
		case "import_from_statement":
			v.handleImportFrom(child, v.module)
		}
	}
}

// handleTopFunction processes a module-level function_definition node.
func (v *visitor) handleTopFunction(n *sitter.Node) {
	nameNode := n.ChildByFieldName("name")
	if nameNode == nil {
		return
	}
	name := v.nodeText(nameNode)
	qualname := v.module + "." + name

	bodyNode := n.ChildByFieldName("body")
	doc := v.docstring(bodyNode)

	sym := core.Symbol{
		Name:      name,
		Qualname:  qualname,
		Kind:      core.SymbolFunction,
		Scope:     core.ScopeGlobal,
		File:      v.path,
		LineStart: lineNumber(n.StartPoint().Row),
		LineEnd:   lineNumber(n.EndPoint().Row),
		Docstring: doc,
		Snippet:   v.snippet(n),
	}
	v.symbols = append(v.symbols, sym)

	// Parameters
	paramsNode := n.ChildByFieldName("parameters")
	v.handleParameters(paramsNode, qualname)

	// Local variables and nested calls in body
	if bodyNode != nil {
		v.walkFunctionBody(bodyNode, qualname)
	}
}

// handleClass processes a class_definition node.
func (v *visitor) handleClass(n *sitter.Node) {
	nameNode := n.ChildByFieldName("name")
	if nameNode == nil {
		return
	}
	className := v.nodeText(nameNode)
	classQualname := v.module + "." + className

	bodyNode := n.ChildByFieldName("body")
	doc := v.docstring(bodyNode)

	sym := core.Symbol{
		Name:      className,
		Qualname:  classQualname,
		Kind:      core.SymbolClass,
		Scope:     core.ScopeGlobal,
		File:      v.path,
		LineStart: lineNumber(n.StartPoint().Row),
		LineEnd:   lineNumber(n.EndPoint().Row),
		Docstring: doc,
		Snippet:   v.snippet(n),
	}
	v.symbols = append(v.symbols, sym)

	// Superclass inherit edges
	superclassesNode := n.ChildByFieldName("superclasses")
	if superclassesNode != nil {
		v.handleSuperclasses(superclassesNode, classQualname)
	}

	// Class body members
	if bodyNode != nil {
		v.walkClassBody(bodyNode, className, classQualname)
	}
}

// handleSuperclasses emits inherit edges for each superclass.
func (v *visitor) handleSuperclasses(n *sitter.Node, classQualname string) {
	for i := 0; i < int(n.ChildCount()); i++ {
		child := n.Child(i)
		if child == nil {
			continue
		}
		switch child.Type() {
		case "identifier":
			superName := v.nodeText(child)
			v.edges = append(v.edges, core.Edge{
				FromQualname: classQualname,
				ToQualname:   superName,
				Kind:         core.EdgeInherit,
				Resolved:     false,
			})
		case "attribute":
			superName := v.nodeText(child)
			v.edges = append(v.edges, core.Edge{
				FromQualname: classQualname,
				ToQualname:   superName,
				Kind:         core.EdgeInherit,
				Resolved:     false,
			})
		}
	}
}

// walkClassBody iterates over class body statements.
func (v *visitor) walkClassBody(bodyNode *sitter.Node, className, classQualname string) {
	for i := 0; i < int(bodyNode.ChildCount()); i++ {
		child := bodyNode.Child(i)
		if child == nil {
			continue
		}
		switch child.Type() {
		case "function_definition":
			v.handleMethod(child, className, classQualname)
		case "expression_statement":
			v.handleClassAssignment(child, classQualname)
		case "assignment":
			v.handleClassAssignmentNode(child, classQualname)
		case "annotated_assignment":
			v.handleClassAnnotatedAssignment(child, classQualname)
		}
	}
}

// handleMethod processes a method inside a class body.
func (v *visitor) handleMethod(n *sitter.Node, className, classQualname string) {
	nameNode := n.ChildByFieldName("name")
	if nameNode == nil {
		return
	}
	methodName := v.nodeText(nameNode)
	methodQualname := classQualname + "." + methodName

	bodyNode := n.ChildByFieldName("body")
	doc := v.docstring(bodyNode)

	sym := core.Symbol{
		Name:           methodName,
		Qualname:       methodQualname,
		Kind:           core.SymbolMethod,
		Scope:          core.ScopeClass,
		File:           v.path,
		LineStart:      lineNumber(n.StartPoint().Row),
		LineEnd:        lineNumber(n.EndPoint().Row),
		ParentQualname: classQualname,
		Docstring:      doc,
		Snippet:        v.snippet(n),
	}
	v.symbols = append(v.symbols, sym)

	// Parameters
	paramsNode := n.ChildByFieldName("parameters")
	v.handleParameters(paramsNode, methodQualname)

	// Local variables and calls
	if bodyNode != nil {
		v.walkFunctionBody(bodyNode, methodQualname)
	}
}

// handleParameters emits variable symbols for each function parameter.
func (v *visitor) handleParameters(paramsNode *sitter.Node, funcQualname string) {
	if paramsNode == nil {
		return
	}
	for i := 0; i < int(paramsNode.ChildCount()); i++ {
		child := paramsNode.Child(i)
		if child == nil {
			continue
		}
		var paramName string
		switch child.Type() {
		case "identifier":
			paramName = v.nodeText(child)
		case "typed_parameter":
			// first named child is the identifier
			idNode := child.ChildByFieldName("name")
			if idNode != nil {
				paramName = v.nodeText(idNode)
			}
		case "default_parameter":
			nameNode := child.ChildByFieldName("name")
			if nameNode != nil {
				paramName = v.nodeText(nameNode)
			}
		case "typed_default_parameter":
			nameNode := child.ChildByFieldName("name")
			if nameNode != nil {
				paramName = v.nodeText(nameNode)
			}
		case "list_splat_pattern":
			// *args
			if child.ChildCount() > 0 {
				id := child.Child(0)
				if id != nil && id.Type() == "identifier" {
					paramName = v.nodeText(id)
				}
			}
		case "dictionary_splat_pattern":
			// **kwargs
			if child.ChildCount() > 0 {
				id := child.Child(0)
				if id != nil && id.Type() == "identifier" {
					paramName = v.nodeText(id)
				}
			}
		}

		if paramName == "" || paramName == "," || paramName == "(" || paramName == ")" {
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

// walkFunctionBody walks statements in a function body, extracting local vars and call edges.
func (v *visitor) walkFunctionBody(bodyNode *sitter.Node, funcQualname string) {
	for i := 0; i < int(bodyNode.ChildCount()); i++ {
		child := bodyNode.Child(i)
		if child == nil {
			continue
		}
		switch child.Type() {
		case "expression_statement":
			v.handleLocalAssignment(child, funcQualname)
		case "assignment":
			v.handleLocalAssignmentNode(child, funcQualname)
		}
		// Walk all descendants for call edges
		v.walkForCalls(child, funcQualname)
	}
}

// handleModuleAssignment handles expression_statement at module level (may contain assignment).
func (v *visitor) handleModuleAssignment(n *sitter.Node) {
	// expression_statement can wrap an assignment in some grammars
	for i := 0; i < int(n.ChildCount()); i++ {
		child := n.Child(i)
		if child == nil {
			continue
		}
		if child.Type() == "assignment" {
			v.handleModuleAssignmentNode(child)
		}
	}
}

// handleModuleAssignmentNode handles an assignment node at module level.
func (v *visitor) handleModuleAssignmentNode(n *sitter.Node) {
	leftNode := n.ChildByFieldName("left")
	if leftNode == nil {
		return
	}
	names := v.extractAssignmentTargets(leftNode)
	for _, name := range names {
		kind := core.SymbolVariable
		if isAllCaps(name) {
			kind = core.SymbolConstant
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

// handleClassAssignment handles expression_statement inside class body.
func (v *visitor) handleClassAssignment(n *sitter.Node, classQualname string) {
	for i := 0; i < int(n.ChildCount()); i++ {
		child := n.Child(i)
		if child == nil {
			continue
		}
		if child.Type() == "assignment" {
			v.handleClassAssignmentNode(child, classQualname)
		}
	}
}

// handleClassAssignmentNode handles an assignment node in a class body.
func (v *visitor) handleClassAssignmentNode(n *sitter.Node, classQualname string) {
	leftNode := n.ChildByFieldName("left")
	if leftNode == nil {
		return
	}
	names := v.extractAssignmentTargets(leftNode)
	for _, name := range names {
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
}

// handleClassAnnotatedAssignment handles an annotated_assignment node in a class body.
func (v *visitor) handleClassAnnotatedAssignment(n *sitter.Node, classQualname string) {
	// annotated_assignment: <name>: <type> = <value>
	// The name is the first named child or child_by_field_name("name")
	nameNode := n.ChildByFieldName("name")
	if nameNode == nil {
		// try first child
		if n.ChildCount() > 0 {
			nameNode = n.Child(0)
		}
	}
	if nameNode == nil {
		return
	}
	name := v.nodeText(nameNode)
	if name == "" || !isIdentifier(name) {
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

// handleLocalAssignment handles expression_statement in a function body.
func (v *visitor) handleLocalAssignment(n *sitter.Node, funcQualname string) {
	for i := 0; i < int(n.ChildCount()); i++ {
		child := n.Child(i)
		if child == nil {
			continue
		}
		if child.Type() == "assignment" {
			v.handleLocalAssignmentNode(child, funcQualname)
		}
	}
}

// handleLocalAssignmentNode handles an assignment node in a function body.
func (v *visitor) handleLocalAssignmentNode(n *sitter.Node, funcQualname string) {
	leftNode := n.ChildByFieldName("left")
	if leftNode == nil {
		return
	}
	names := v.extractAssignmentTargets(leftNode)
	for _, name := range names {
		sym := core.Symbol{
			Name:           name,
			Qualname:       funcQualname + "." + name,
			Kind:           core.SymbolVariable,
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

// extractAssignmentTargets extracts identifier names from an assignment LHS node.
func (v *visitor) extractAssignmentTargets(n *sitter.Node) []string {
	if n == nil {
		return nil
	}
	switch n.Type() {
	case "identifier":
		name := v.nodeText(n)
		if isIdentifier(name) {
			return []string{name}
		}
	case "pattern_list", "tuple_pattern":
		// a, b = ...
		var names []string
		for i := 0; i < int(n.ChildCount()); i++ {
			child := n.Child(i)
			if child != nil && child.Type() == "identifier" {
				name := v.nodeText(child)
				if isIdentifier(name) {
					names = append(names, name)
				}
			}
		}
		return names
	}
	return nil
}

// isIdentifier returns true if s looks like a valid Python identifier.
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

// handleImport handles `import x` and `import x.y as z` statements.
func (v *visitor) handleImport(n *sitter.Node, fromQualname string) {
	// Children: "import" keyword + dotted_name or aliased_import nodes
	for i := 0; i < int(n.ChildCount()); i++ {
		child := n.Child(i)
		if child == nil {
			continue
		}
		switch child.Type() {
		case "dotted_name":
			importedName := v.nodeText(child)
			// The locally bound name is the first component
			localName := strings.SplitN(importedName, ".", 2)[0]
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
			// import edge: module → importedName
			v.edges = append(v.edges, core.Edge{
				FromQualname: v.module,
				ToQualname:   importedName,
				Kind:         core.EdgeImport,
				Resolved:     false,
			})
		case "aliased_import":
			// import x.y as z
			nameNode := child.ChildByFieldName("name")
			aliasNode := child.ChildByFieldName("alias")
			importedName := ""
			localName := ""
			if nameNode != nil {
				importedName = v.nodeText(nameNode)
			}
			if aliasNode != nil {
				localName = v.nodeText(aliasNode)
			} else if nameNode != nil {
				localName = strings.SplitN(importedName, ".", 2)[0]
			}
			if localName == "" {
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
			if importedName != "" {
				v.edges = append(v.edges, core.Edge{
					FromQualname: v.module,
					ToQualname:   importedName,
					Kind:         core.EdgeImport,
					Resolved:     false,
				})
			}
		}
	}
}

// handleImportFrom handles `from x import y` statements.
func (v *visitor) handleImportFrom(n *sitter.Node, fromQualname string) {
	// module_name is the "from" part
	moduleNameNode := n.ChildByFieldName("module_name")
	moduleName := ""
	if moduleNameNode != nil {
		moduleName = v.nodeText(moduleNameNode)
	}

	// Each imported name is either a dotted_name/identifier or aliased_import
	for i := 0; i < int(n.ChildCount()); i++ {
		child := n.Child(i)
		if child == nil {
			continue
		}
		switch child.Type() {
		case "dotted_name":
			// this is the module part, not the imported names (already handled via module_name field)
			// skip if it matches moduleName
			if v.nodeText(child) == moduleName {
				continue
			}
			imported := v.nodeText(child)
			localName := strings.SplitN(imported, ".", 2)[0]
			v.emitFromImport(n, moduleName, localName)
		case "identifier":
			// Could be the imported name
			text := v.nodeText(child)
			if text == moduleName || text == "from" || text == "import" || text == "*" {
				continue
			}
			v.emitFromImport(n, moduleName, text)
		case "aliased_import":
			nameNode := child.ChildByFieldName("name")
			aliasNode := child.ChildByFieldName("alias")
			imported := ""
			localName := ""
			if nameNode != nil {
				imported = v.nodeText(nameNode)
			}
			if aliasNode != nil {
				localName = v.nodeText(aliasNode)
			} else {
				localName = imported
			}
			if localName == "" {
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
			target := moduleName
			if imported != "" {
				if moduleName != "" {
					target = moduleName + "." + imported
				} else {
					target = imported
				}
			}
			if target != "" {
				v.edges = append(v.edges, core.Edge{
					FromQualname: v.module,
					ToQualname:   target,
					Kind:         core.EdgeImport,
					Resolved:     false,
				})
			}
		}
	}
}

// emitFromImport emits a symbol + import edge for `from module import name`.
func (v *visitor) emitFromImport(n *sitter.Node, moduleName, localName string) {
	if localName == "" || !isIdentifier(localName) {
		return
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

	target := localName
	if moduleName != "" {
		target = moduleName + "." + localName
	}
	v.edges = append(v.edges, core.Edge{
		FromQualname: v.module,
		ToQualname:   target,
		Kind:         core.EdgeImport,
		Resolved:     false,
	})
}

// walkForCalls recursively walks a node's descendants looking for call expressions
// and identifier references to imported names.
func (v *visitor) walkForCalls(n *sitter.Node, enclosingQualname string) {
	if n == nil {
		return
	}
	if n.Type() == "call" {
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
			case "attribute":
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

	// Best-effort reference edges: identifier used as a value that matches an imported name.
	if n.Type() == "identifier" && n.Parent() != nil {
		// Only emit reference when the identifier is used as a value (not as a definition target).
		parentType := n.Parent().Type()
		if parentType != "function_definition" &&
			parentType != "class_definition" &&
			parentType != "assignment" &&
			parentType != "parameters" &&
			parentType != "typed_parameter" &&
			parentType != "default_parameter" &&
			parentType != "typed_default_parameter" &&
			parentType != "import_statement" &&
			parentType != "import_from_statement" &&
			parentType != "aliased_import" {
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

	// Recurse into all children
	for i := 0; i < int(n.ChildCount()); i++ {
		child := n.Child(i)
		if child != nil {
			v.walkForCalls(child, enclosingQualname)
		}
	}
}
