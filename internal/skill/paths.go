package skill

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/devchan97/code-map/internal/platform"
)

// Target identifies where to install/uninstall a SKILL.md and supplies the
// values needed to render the template.
type Target struct {
	Agent      string // "claude-code" | "codex"
	Scope      string // "user" | "project"
	Repo       string // required when Scope == "project"
	BinaryName string // executable name substituted into the template; defaults to "codemap"
	Version    string // version string substituted into the template
}

// ErrCodexPending is returned when a codex target is requested while the
// upstream Codex skill spec is still being finalised. Exported so the CLI
// layer can detect the case via errors.Is and replace the long wrap chain
// with a single user-readable message.
var ErrCodexPending = errors.New("codex skill spec pending; verify at release time")

// errCodexPending keeps the previous unexported alias for tests in this
// package; new code should use ErrCodexPending.
var errCodexPending = ErrCodexPending

// ResolvePath returns the absolute file path for the SKILL.md of the given target.
// Output paths are normalized to forward slashes.
//
// Path rules:
//   - claude-code + user    → <UserHome>/.claude/skills/codemap/SKILL.md
//   - claude-code + project → <Repo>/.claude/skills/codemap/SKILL.md
//   - codex + any           → error (spec pending)
//   - unknown agent         → error
//   - unknown scope         → error
func ResolvePath(t Target) (string, error) {
	switch t.Agent {
	case "claude-code":
		switch t.Scope {
		case "user":
			home, err := platform.UserHome()
			if err != nil {
				return "", fmt.Errorf("skill: resolve path: %w", err)
			}
			p := filepath.Join(home, ".claude", "skills", "codemap", "SKILL.md")
			return platform.ToSlash(p), nil
		case "project":
			if t.Repo == "" {
				return "", fmt.Errorf("skill: resolve path: Repo must be set for project scope")
			}
			p := filepath.Join(t.Repo, ".claude", "skills", "codemap", "SKILL.md")
			return platform.ToSlash(p), nil
		default:
			return "", fmt.Errorf("skill: resolve path: unknown scope %q", t.Scope)
		}
	case "codex":
		return "", fmt.Errorf("skill: resolve path: %w", errCodexPending)
	default:
		return "", fmt.Errorf("skill: resolve path: unknown agent %q", t.Agent)
	}
}
