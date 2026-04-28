package skill

import (
	"errors"
	"strings"
	"testing"
)

func TestResolvePath_Table(t *testing.T) {
	tmpRepo := t.TempDir()

	tests := []struct {
		name        string
		target      Target
		wantSuffix  string
		wantErr     bool
		wantErrHint string // substring expected in error message
	}{
		{
			name:       "claude-code user",
			target:     Target{Agent: "claude-code", Scope: "user"},
			wantSuffix: "/.claude/skills/codemap/SKILL.md",
		},
		{
			name:       "claude-code project",
			target:     Target{Agent: "claude-code", Scope: "project", Repo: tmpRepo},
			wantSuffix: "/.claude/skills/codemap/SKILL.md",
		},
		{
			name:        "claude-code project missing repo",
			target:      Target{Agent: "claude-code", Scope: "project"},
			wantErr:     true,
			wantErrHint: "Repo must be set",
		},
		{
			name:        "codex user — unsupported",
			target:      Target{Agent: "codex", Scope: "user"},
			wantErr:     true,
			wantErrHint: "verify",
		},
		{
			name:        "codex project — unsupported",
			target:      Target{Agent: "codex", Scope: "project"},
			wantErr:     true,
			wantErrHint: "verify",
		},
		{
			name:        "unknown agent",
			target:      Target{Agent: "bogus", Scope: "user"},
			wantErr:     true,
			wantErrHint: "unknown agent",
		},
		{
			name:        "unknown scope",
			target:      Target{Agent: "claude-code", Scope: "weird"},
			wantErr:     true,
			wantErrHint: "unknown scope",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolvePath(tc.target)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got path %q", got)
				}
				if tc.wantErrHint != "" && !strings.Contains(err.Error(), tc.wantErrHint) {
					t.Errorf("error %q missing hint %q", err.Error(), tc.wantErrHint)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.HasSuffix(got, tc.wantSuffix) {
				t.Errorf("got %q; want suffix %q", got, tc.wantSuffix)
			}
			if strings.Contains(got, "\\") {
				t.Errorf("path contains backslash (not forward-slash normalized): %q", got)
			}
		})
	}
}

// TestResolvePath_CodexErrSentinel checks the codex error wraps errCodexPending.
func TestResolvePath_CodexErrSentinel(t *testing.T) {
	_, err := ResolvePath(Target{Agent: "codex", Scope: "user"})
	if err == nil {
		t.Fatal("expected error for codex target")
	}
	if !errors.Is(err, errCodexPending) {
		t.Errorf("expected errCodexPending in chain, got: %v", err)
	}
}
