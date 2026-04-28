package platform

import (
	"os"

	"golang.org/x/term"
)

// IsTerminal reports whether the file descriptor fd refers to a terminal.
func IsTerminal(fd uintptr) bool {
	return term.IsTerminal(int(fd))
}

// SupportsColor reports whether the current stdout supports ANSI color output.
// It returns true only when stdout is a terminal, NO_COLOR is unset, and
// TERM is not "dumb".
func SupportsColor() bool {
	if !IsTerminal(os.Stdout.Fd()) {
		return false
	}
	if _, set := os.LookupEnv("NO_COLOR"); set {
		return false
	}
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	return true
}
