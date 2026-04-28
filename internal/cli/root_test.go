package cli

import (
	"testing"
)

func TestNewRoot_HasAllSubcommands(t *testing.T) {
	root := NewRoot()
	want := []string{
		"init", "index", "reindex", "list", "status", "forget",
		"search", "show", "refs", "calls", "visualize",
		"install-skill", "uninstall-skill", "version",
	}
	got := map[string]bool{}
	for _, c := range root.Commands() {
		got[c.Name()] = true
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("subcommand %q missing", w)
		}
	}
}

func TestNewRoot_SilenceUsage(t *testing.T) {
	root := NewRoot()
	if !root.SilenceUsage {
		t.Error("root.SilenceUsage should be true")
	}
}

func TestVersion_DefaultUnset(t *testing.T) {
	if Version == "" {
		t.Error("Version should have a non-empty default")
	}
}
