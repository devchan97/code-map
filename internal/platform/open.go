package platform

import (
	"fmt"
	"os/exec"
	"runtime"
)

// OpenInBrowser opens the given path in the OS default browser or file
// handler. On Windows it uses `cmd /c start`, on macOS `open`, and on
// Linux `xdg-open`.
func OpenInBrowser(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		// Use cmd /c start so that paths with spaces are handled correctly.
		cmd = exec.Command("cmd", "/c", "start", "", path)
	case "darwin":
		cmd = exec.Command("open", path)
	default:
		// Covers linux and any other POSIX-like OS.
		cmd = exec.Command("xdg-open", path)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("OpenInBrowser: %w", err)
	}
	return nil
}
