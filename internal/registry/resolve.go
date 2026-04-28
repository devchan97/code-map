package registry

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/devchan97/code-map/internal/core"
	"github.com/devchan97/code-map/internal/platform"
)

// ResolveInput carries the inputs to the repo resolver.
type ResolveInput struct {
	// FlagRepo is the value of the --repo flag (NAME or PATH); empty if absent.
	FlagRepo string
	// Cwd is the working directory to use for resolution (typically os.Getwd()).
	Cwd string
}

// Resolution is the result of a successful repo resolution.
type Resolution struct {
	// Entry is the resolved registry entry.
	Entry core.RegistryEntry
	// Notice is a human-readable note intended for stderr; empty if none.
	Notice string
}

// Resolve picks the repo entry to operate on using the 6-step resolution
// algorithm defined in design §6.2. It returns core.ErrRepoNotFound (wrapped)
// when no repo can be identified.
//
// Resolution order (first match wins):
//  1. --repo NAME|PATH flag
//  2. Cwd is inside a registered path
//  3. Cwd (or an ancestor) contains .codemap/index.db → auto-register if needed
//  4. state.DefaultRepo matches a registered Name
//  5. Exactly one entry in the registry → return it with a Notice
//  6. Otherwise → ErrRepoNotFound with hint listing known names
func Resolve(in ResolveInput, reg Registry, st State) (Resolution, error) {
	entries, err := reg.Load()
	if err != nil {
		return Resolution{}, fmt.Errorf("resolve: load registry: %w", err)
	}

	// ── Step 1: --repo flag ──────────────────────────────────────────────────
	if in.FlagRepo != "" {
		// 1a. Match by Name.
		for _, e := range entries {
			if e.Name == in.FlagRepo {
				return Resolution{Entry: e}, nil
			}
		}

		// 1b. Treat FlagRepo as a path; resolve it against Cwd if relative.
		flagPath := in.FlagRepo
		if !filepath.IsAbs(platform.FromSlash(flagPath)) && in.Cwd != "" {
			flagPath = platform.ToSlash(
				filepath.Join(platform.FromSlash(in.Cwd), platform.FromSlash(flagPath)),
			)
		}
		canonFlag, err2 := canonPath(flagPath)
		if err2 == nil {
			for _, e := range entries {
				canonE, err3 := canonPath(e.Path)
				if err3 == nil && canonE == canonFlag {
					return Resolution{Entry: e}, nil
				}
			}

			// 1c. Path not in registry but has .codemap/index.db → auto-register.
			if hasIndexDB(canonFlag) {
				entry, err3 := autoRegister(reg, canonFlag)
				if err3 != nil {
					return Resolution{}, fmt.Errorf("resolve: auto-register %q: %w", canonFlag, err3)
				}
				return Resolution{Entry: entry}, nil
			}
		}

		// 1d. Not found.
		return Resolution{}, fmt.Errorf("resolve: --repo %q: %w%s",
			in.FlagRepo, core.ErrRepoNotFound, knownNamesHint(entries))
	}

	// ── Step 2: Cwd inside a registered path ─────────────────────────────────
	if in.Cwd != "" {
		canonCwd, err2 := canonPath(in.Cwd)
		if err2 == nil {
			for _, e := range entries {
				canonE, err3 := canonPath(e.Path)
				if err3 != nil {
					continue
				}
				if isUnder(canonCwd, canonE) {
					return Resolution{Entry: e}, nil
				}
			}
		}

		// ── Step 3: Walk up Cwd looking for .codemap/index.db ────────────────
		root, found := walkUpForIndexDB(in.Cwd)
		if found {
			canonRoot, err2 := canonPath(root)
			if err2 == nil {
				// Check if already registered.
				for _, e := range entries {
					canonE, err3 := canonPath(e.Path)
					if err3 == nil && canonE == canonRoot {
						return Resolution{Entry: e}, nil
					}
				}
				// Auto-register.
				entry, err3 := autoRegister(reg, canonRoot)
				if err3 != nil {
					return Resolution{}, fmt.Errorf("resolve: auto-register %q: %w", canonRoot, err3)
				}
				return Resolution{Entry: entry}, nil
			}
		}
	}

	// ── Step 4: state.DefaultRepo ─────────────────────────────────────────────
	if st.DefaultRepo != "" {
		for _, e := range entries {
			if e.Name == st.DefaultRepo {
				return Resolution{Entry: e}, nil
			}
		}
	}

	// ── Step 5: Exactly one registry entry ───────────────────────────────────
	if len(entries) == 1 {
		return Resolution{
			Entry:  entries[0],
			Notice: fmt.Sprintf("using only registered repo: %s", entries[0].Name),
		}, nil
	}

	// ── Step 6: Error ─────────────────────────────────────────────────────────
	return Resolution{}, fmt.Errorf("resolve: cannot identify target repo: %w%s",
		core.ErrRepoNotFound, knownNamesHint(entries))
}

// autoRegister creates and upserts a new RegistryEntry for repoRoot.
// Name is set to the base name of the path; Embedder = "lexical"; SchemaVer = 1.
func autoRegister(reg Registry, repoRoot string) (core.RegistryEntry, error) {
	name := filepath.Base(platform.FromSlash(repoRoot))
	e := core.RegistryEntry{
		Name:      name,
		Path:      repoRoot,
		CreatedAt: time.Now().UTC(),
		Embedder:  "lexical",
		SchemaVer: 1,
	}
	if err := reg.Upsert(e); err != nil {
		return core.RegistryEntry{}, fmt.Errorf("autoRegister: %w", err)
	}
	return e, nil
}

// hasIndexDB reports whether repoRoot/.codemap/index.db exists.
func hasIndexDB(repoRoot string) bool {
	dbPath := platform.FromSlash(repoRoot + "/.codemap/index.db")
	_, err := os.Stat(dbPath)
	return err == nil
}

// walkUpForIndexDB walks from dir upward and returns the first ancestor that
// contains .codemap/index.db, along with a found flag.
func walkUpForIndexDB(dir string) (string, bool) {
	cur := platform.FromSlash(dir)
	for {
		dbPath := filepath.Join(cur, ".codemap", "index.db")
		if _, err := os.Stat(dbPath); err == nil {
			return platform.ToSlash(cur), true
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	return "", false
}

// isUnder reports whether child is equal to or a sub-path of parent.
// Both paths must be canonical (absolute, clean, forward-slash).
func isUnder(child, parent string) bool {
	if child == parent {
		return true
	}
	return strings.HasPrefix(child, parent+"/")
}

// knownNamesHint returns a formatted string listing known repo names, or
// an empty string if entries is empty.
func knownNamesHint(entries []core.RegistryEntry) string {
	if len(entries) == 0 {
		return "; no repos registered (run 'codemap init')"
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name)
	}
	return fmt.Sprintf("; known repos: %s", strings.Join(names, ", "))
}
