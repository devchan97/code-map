package registry

import (
	"bytes"
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
	"github.com/devchan97/code-map/internal/platform"
)

// State holds machine-local default settings persisted in ~/.codemap/state.toml.
type State struct {
	// DefaultRepo is the name of the repository to use when no other
	// resolution method succeeds (step 4 of the 6-step resolver).
	DefaultRepo string `toml:"default_repo"`
}

// LoadState reads ~/.codemap/state.toml and returns the decoded State.
// A missing file is not an error; it returns a zero State and nil.
func LoadState() (State, error) {
	path := platform.StatePath()
	data, err := os.ReadFile(platform.FromSlash(path))
	if err != nil {
		if os.IsNotExist(err) {
			return State{}, nil
		}
		return State{}, fmt.Errorf("LoadState: read %q: %w", path, err)
	}

	var s State
	if _, err = toml.Decode(string(data), &s); err != nil {
		return State{}, fmt.Errorf("LoadState: decode %q: %w", path, err)
	}
	return s, nil
}

// SaveState writes s to ~/.codemap/state.toml atomically.
func SaveState(s State) error {
	path := platform.StatePath()

	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(s); err != nil {
		return fmt.Errorf("SaveState: encode: %w", err)
	}

	if err := platform.EnsureDir(platform.CodemapHome()); err != nil {
		return fmt.Errorf("SaveState: ensure dir: %w", err)
	}
	if err := platform.AtomicWrite(platform.FromSlash(path), buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("SaveState: write %q: %w", path, err)
	}
	return nil
}
