// Package core defines codemap's domain types and sentinel errors.
package core

import (
	"encoding/json"
	"fmt"
)

// SymbolKind classifies what a symbol represents in source code.
type SymbolKind string

const (
	// SymbolFunction represents a top-level or nested function.
	SymbolFunction SymbolKind = "function"
	// SymbolMethod represents a method attached to a type or class.
	SymbolMethod SymbolKind = "method"
	// SymbolClass represents a class or struct definition.
	SymbolClass SymbolKind = "class"
	// SymbolVariable represents a variable declaration or assignment.
	SymbolVariable SymbolKind = "variable"
	// SymbolImport represents an import statement.
	SymbolImport SymbolKind = "import"
	// SymbolConstant represents a constant declaration.
	SymbolConstant SymbolKind = "constant"
)

// ParseSymbolKind parses a string into a SymbolKind, returning an error on unknown values.
func ParseSymbolKind(s string) (SymbolKind, error) {
	switch SymbolKind(s) {
	case SymbolFunction, SymbolMethod, SymbolClass, SymbolVariable, SymbolImport, SymbolConstant:
		return SymbolKind(s), nil
	default:
		return "", fmt.Errorf("unknown SymbolKind %q", s)
	}
}

// String returns the underlying string value of the SymbolKind.
func (k SymbolKind) String() string { return string(k) }

// MarshalJSON encodes the SymbolKind as a bare JSON string.
func (k SymbolKind) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(k))
}

// UnmarshalJSON decodes the SymbolKind from a bare JSON string.
func (k *SymbolKind) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	parsed, err := ParseSymbolKind(s)
	if err != nil {
		return err
	}
	*k = parsed
	return nil
}

// Scope describes the lexical scope in which a symbol is defined.
type Scope string

const (
	// ScopeGlobal represents a module- or package-level symbol.
	ScopeGlobal Scope = "global"
	// ScopeClass represents a symbol defined inside a class body.
	ScopeClass Scope = "class"
	// ScopeLocal represents a symbol defined inside a function body.
	ScopeLocal Scope = "local"
	// ScopeParam represents a function parameter.
	ScopeParam Scope = "param"
)

// ParseScope parses a string into a Scope, returning an error on unknown values.
func ParseScope(s string) (Scope, error) {
	switch Scope(s) {
	case ScopeGlobal, ScopeClass, ScopeLocal, ScopeParam:
		return Scope(s), nil
	default:
		return "", fmt.Errorf("unknown Scope %q", s)
	}
}

// String returns the underlying string value of the Scope.
func (s Scope) String() string { return string(s) }

// MarshalJSON encodes the Scope as a bare JSON string.
func (s Scope) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(s))
}

// UnmarshalJSON decodes the Scope from a bare JSON string.
func (s *Scope) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}
	parsed, err := ParseScope(str)
	if err != nil {
		return err
	}
	*s = parsed
	return nil
}

// EdgeKind classifies the relationship between two symbols in an edge.
type EdgeKind string

const (
	// EdgeCall represents a function/method call edge.
	EdgeCall EdgeKind = "call"
	// EdgeReference represents a non-call reference to a symbol.
	EdgeReference EdgeKind = "reference"
	// EdgeInherit represents a class inheritance edge.
	EdgeInherit EdgeKind = "inherit"
	// EdgeImport represents an import-based dependency edge.
	EdgeImport EdgeKind = "import"
)

// ParseEdgeKind parses a string into an EdgeKind, returning an error on unknown values.
func ParseEdgeKind(s string) (EdgeKind, error) {
	switch EdgeKind(s) {
	case EdgeCall, EdgeReference, EdgeInherit, EdgeImport:
		return EdgeKind(s), nil
	default:
		return "", fmt.Errorf("unknown EdgeKind %q", s)
	}
}

// String returns the underlying string value of the EdgeKind.
func (k EdgeKind) String() string { return string(k) }

// MarshalJSON encodes the EdgeKind as a bare JSON string.
func (k EdgeKind) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(k))
}

// UnmarshalJSON decodes the EdgeKind from a bare JSON string.
func (k *EdgeKind) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	parsed, err := ParseEdgeKind(s)
	if err != nil {
		return err
	}
	*k = parsed
	return nil
}
