//go:build !windows

package install

import (
	"strings"
	"testing"
)

// TestStripMarkerBlock_RoundTripsCleanly guards that uninstall-self can
// remove exactly the block install-self appended to a shell rc file
// without touching anything else and that the operation is idempotent.
func TestStripMarkerBlock_RoundTripsCleanly(t *testing.T) {
	original := `# user shell rc
alias ll='ls -alF'
export FOO=bar
`
	added := original + "\n" + markerBegin + "\nexport PATH=\"/x:$PATH\"\n" + markerEnd + "\n"
	stripped, removed := stripMarkerBlock(added)
	if !removed {
		t.Fatal("expected removed=true")
	}
	if strings.Contains(stripped, markerBegin) || strings.Contains(stripped, markerEnd) {
		t.Errorf("stripped still contains marker:\n%s", stripped)
	}
	if !strings.HasPrefix(stripped, "# user shell rc") {
		t.Errorf("stripped lost original content: %q", stripped)
	}
	stripped2, removed2 := stripMarkerBlock(stripped)
	if removed2 || stripped2 != stripped {
		t.Errorf("strip is not idempotent")
	}
}
