package skill

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSkillTemplate_NoDriftBetweenCopies(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")

	embedded, err := os.ReadFile(filepath.Join(filepath.Dir(thisFile), "SKILL.md.tmpl"))
	if err != nil {
		t.Fatalf("read embedded template: %v", err)
	}
	canonical, err := os.ReadFile(filepath.Join(repoRoot, "skill", "SKILL.md.tmpl"))
	if err != nil {
		t.Fatalf("read canonical template: %v", err)
	}
	if string(embedded) != string(canonical) {
		t.Fatalf("SKILL.md.tmpl drift: skill/SKILL.md.tmpl and internal/skill/SKILL.md.tmpl differ — keep them byte-identical")
	}
}
