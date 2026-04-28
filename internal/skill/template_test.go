package skill

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRender_GoldenMatch renders the template with fixed values and compares
// byte-for-byte against testdata/skill_golden.md.
func TestRender_GoldenMatch(t *testing.T) {
	got, err := Render("codemap", "0.1.0")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	goldenPath := filepath.Join("testdata", "skill_golden.md")
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden file %s: %v", goldenPath, err)
	}

	if got != string(want) {
		t.Errorf("rendered output does not match golden file %s\n--- got ---\n%s\n--- want ---\n%s",
			goldenPath, got, string(want))
	}
}

// TestRender_SubstitutesVariables verifies both placeholders are replaced.
func TestRender_SubstitutesVariables(t *testing.T) {
	body, err := Render("foobin", "v9.9.9")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, want := range []string{"foobin", "v9.9.9"} {
		found := false
		for i := 0; i < len(body)-len(want)+1; i++ {
			if body[i:i+len(want)] == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("rendered body missing %q", want)
		}
	}
	// prose uses literal "codemap" regardless of BinaryName
	literalCodemap := "codemap"
	found := false
	for i := 0; i < len(body)-len(literalCodemap)+1; i++ {
		if body[i:i+len(literalCodemap)] == literalCodemap {
			found = true
			break
		}
	}
	if !found {
		t.Error("rendered body missing literal 'codemap' in prose")
	}
}
