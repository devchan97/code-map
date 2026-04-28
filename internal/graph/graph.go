// Package graph answers refs (incoming) and calls (outgoing) graph queries
// against a codemap index.
package graph

import (
	"context"
	"fmt"

	"github.com/devchan97/code-map/internal/core"
	"github.com/devchan97/code-map/internal/store"
)

// EdgeView represents a graph edge with its endpoint symbols hydrated where
// available. Unresolved edges (no matching symbol for an endpoint) carry nil
// From or To pointers.
type EdgeView struct {
	// From is the hydrated source symbol, or nil when unresolved.
	From *core.Symbol
	// To is the hydrated target symbol, or nil when unresolved.
	To *core.Symbol
	// FromName is the raw from_qualname string (always populated).
	FromName string
	// ToName is the raw to_qualname string (always populated).
	ToName string
	// Kind is the edge relationship type.
	Kind core.EdgeKind
	// Resolved mirrors the edge's resolved flag from the database.
	Resolved bool
}

// Refs returns all edges where ToName == qualname, representing incoming
// references and callers of the named symbol. Unresolved edges are included
// with Resolved == false.
func Refs(ctx context.Context, st *store.Store, qualname string) ([]EdgeView, error) {
	var views []EdgeView

	err := st.WithTx(ctx, func(tx store.Tx) error {
		edges, err := tx.EdgesTo(qualname)
		if err != nil {
			return fmt.Errorf("graph.Refs EdgesTo %q: %w", qualname, err)
		}

		// Hydrate the "To" symbol once — it is the same for all edges.
		toSym := lookupFirst(tx, qualname)

		for _, e := range edges {
			fromSym := lookupFirst(tx, e.FromQualname)
			views = append(views, EdgeView{
				From:     fromSym,
				To:       toSym,
				FromName: e.FromQualname,
				ToName:   e.ToQualname,
				Kind:     e.Kind,
				Resolved: e.Resolved,
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return views, nil
}

// Calls returns all edges where FromName == qualname, representing outgoing
// calls and references made by the named symbol.
func Calls(ctx context.Context, st *store.Store, qualname string) ([]EdgeView, error) {
	var views []EdgeView

	err := st.WithTx(ctx, func(tx store.Tx) error {
		edges, err := tx.EdgesFrom(qualname)
		if err != nil {
			return fmt.Errorf("graph.Calls EdgesFrom %q: %w", qualname, err)
		}

		// Hydrate the "From" symbol once — it is the same for all edges.
		fromSym := lookupFirst(tx, qualname)

		for _, e := range edges {
			toSym := lookupFirst(tx, e.ToQualname)
			views = append(views, EdgeView{
				From:     fromSym,
				To:       toSym,
				FromName: e.FromQualname,
				ToName:   e.ToQualname,
				Kind:     e.Kind,
				Resolved: e.Resolved,
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return views, nil
}

// lookupFirst queries the store for a symbol by qualname and returns a pointer
// to the first match, or nil when none is found or the query fails.
func lookupFirst(tx store.Tx, qualname string) *core.Symbol {
	syms, err := tx.SymbolsByQualname(qualname)
	if err != nil || len(syms) == 0 {
		return nil
	}
	s := syms[0]
	return &s
}
