// Package registry manages the global codemap registry of indexed repos.
package registry

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/BurntSushi/toml"
	"github.com/devchan97/code-map/internal/core"
	"github.com/devchan97/code-map/internal/platform"
)

// tomlFile is the on-disk representation of registry.toml.
type tomlFile struct {
	Index []core.RegistryEntry `toml:"index"`
}

// Registry is the abstract registry interface (file-backed by FileRegistry).
type Registry interface {
	// Load reads all entries from the registry. Returns an empty slice and
	// nil error if the file does not exist.
	Load() ([]core.RegistryEntry, error)

	// Upsert inserts or replaces the entry matched by Path (canonicalized).
	// Entries are sorted by Name after every write.
	Upsert(e core.RegistryEntry) error

	// Remove deletes the entry whose Name equals nameOrPath (exact) OR whose
	// canonical Path equals the canonicalized nameOrPath. If no match is
	// found, nil is returned.
	Remove(nameOrPath string) error

	// Get returns the entry with the given name, a found flag, and any error.
	Get(name string) (core.RegistryEntry, bool, error)
}

// FileRegistry persists entries to a TOML file.
type FileRegistry struct {
	path string
}

// NewFileRegistry returns a Registry backed by the file at path.
// If path is empty, it defaults to platform.RegistryPath().
func NewFileRegistry(path string) *FileRegistry {
	if path == "" {
		path = platform.RegistryPath()
	}
	return &FileRegistry{path: path}
}

// Load reads all registry entries. Missing file returns (nil, nil).
func (r *FileRegistry) Load() ([]core.RegistryEntry, error) {
	data, err := os.ReadFile(platform.FromSlash(r.path))
	if err != nil {
		if os.IsNotExist(err) {
			return []core.RegistryEntry{}, nil
		}
		return nil, fmt.Errorf("registry load %q: %w", r.path, err)
	}

	var tf tomlFile
	if _, err = toml.Decode(string(data), &tf); err != nil {
		return nil, fmt.Errorf("registry decode %q: %w", r.path, err)
	}
	return tf.Index, nil
}

// Upsert inserts or replaces an entry matched by canonical Path, then saves.
// Entries are sorted by Name for determinism.
func (r *FileRegistry) Upsert(e core.RegistryEntry) error {
	canonNew, err := canonPath(e.Path)
	if err != nil {
		return fmt.Errorf("registry upsert: canonicalize path %q: %w", e.Path, err)
	}
	e.Path = canonNew

	entries, err := r.Load()
	if err != nil {
		return fmt.Errorf("registry upsert: load: %w", err)
	}

	replaced := false
	for i, ex := range entries {
		canonEx, err2 := canonPath(ex.Path)
		if err2 != nil {
			continue
		}
		if canonEx == canonNew {
			entries[i] = e
			replaced = true
			break
		}
	}
	if !replaced {
		entries = append(entries, e)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})

	if err = r.save(entries); err != nil {
		return fmt.Errorf("registry upsert: save: %w", err)
	}
	return nil
}

// Remove deletes the entry matched by Name (exact) or canonical Path.
// Returns nil if no match is found.
func (r *FileRegistry) Remove(nameOrPath string) error {
	entries, err := r.Load()
	if err != nil {
		return fmt.Errorf("registry remove: load: %w", err)
	}

	canonTarget, _ := canonPath(nameOrPath)

	kept := entries[:0]
	for _, e := range entries {
		if e.Name == nameOrPath {
			continue
		}
		canonEx, err2 := canonPath(e.Path)
		if err2 == nil && canonEx == canonTarget {
			continue
		}
		kept = append(kept, e)
	}

	if len(kept) == len(entries) {
		// No match found — not an error per spec.
		return nil
	}

	if err = r.save(kept); err != nil {
		return fmt.Errorf("registry remove: save: %w", err)
	}
	return nil
}

// Get returns the entry with the given name, whether it was found, and any error.
func (r *FileRegistry) Get(name string) (core.RegistryEntry, bool, error) {
	entries, err := r.Load()
	if err != nil {
		return core.RegistryEntry{}, false, fmt.Errorf("registry get: %w", err)
	}
	for _, e := range entries {
		if e.Name == name {
			return e, true, nil
		}
	}
	return core.RegistryEntry{}, false, nil
}

// save marshals entries to TOML and writes atomically to r.path.
func (r *FileRegistry) save(entries []core.RegistryEntry) error {
	if entries == nil {
		entries = []core.RegistryEntry{}
	}
	tf := tomlFile{Index: entries}

	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(tf); err != nil {
		return fmt.Errorf("registry encode: %w", err)
	}

	dir := filepath.Dir(platform.FromSlash(r.path))
	if err := platform.EnsureDir(platform.ToSlash(dir)); err != nil {
		return fmt.Errorf("registry save: ensure dir: %w", err)
	}

	if err := platform.AtomicWrite(platform.FromSlash(r.path), buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("registry save: write: %w", err)
	}
	return nil
}

// canonPath returns a canonical, forward-slash-normalized absolute path.
func canonPath(p string) (string, error) {
	abs, err := filepath.Abs(platform.FromSlash(p))
	if err != nil {
		return "", err
	}
	return platform.ToSlash(filepath.Clean(abs)), nil
}
