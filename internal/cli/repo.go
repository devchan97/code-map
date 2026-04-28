package cli

import (
	"fmt"
	"os"

	"github.com/devchan97/code-map/internal/core"
	"github.com/devchan97/code-map/internal/registry"
	"github.com/devchan97/code-map/internal/store"
)

// resolveRepo resolves a registry entry using the 6-step algorithm.
// flagRepo is the value of the --repo flag (may be empty).
// If resolution yields a notice, it is printed to stderr.
func resolveRepo(flagRepo string) (core.RegistryEntry, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return core.RegistryEntry{}, fmt.Errorf("resolveRepo: getwd: %w", err)
	}

	reg := registry.NewFileRegistry("")
	st, err := registry.LoadState()
	if err != nil {
		return core.RegistryEntry{}, fmt.Errorf("resolveRepo: load state: %w", err)
	}

	res, err := registry.Resolve(registry.ResolveInput{
		FlagRepo: flagRepo,
		Cwd:      cwd,
	}, reg, st)
	if err != nil {
		return core.RegistryEntry{}, err
	}

	if res.Notice != "" {
		fmt.Fprintln(os.Stderr, "notice:", res.Notice)
	}

	return res.Entry, nil
}

// openStore opens (and if necessary initialises) the SQLite store for repoRoot.
func openStore(repoRoot string) (*store.Store, error) {
	st, err := store.Open(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("openStore: %w", err)
	}
	return st, nil
}
