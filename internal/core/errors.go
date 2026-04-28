package core

import "errors"

// ErrUsage is returned when the caller provides invalid arguments; maps to exit code 2.
var ErrUsage = errors.New("usage error")

// ErrRepoNotFound is returned when the target repository cannot be located; maps to exit code 3.
var ErrRepoNotFound = errors.New("repo not found")

// ErrSchemaMismatch is returned when the index schema version does not match the binary; maps to exit code 4.
var ErrSchemaMismatch = errors.New("schema mismatch")

// ErrIndexCorrupt is returned when the index database appears to be corrupted; maps to exit code 5.
var ErrIndexCorrupt = errors.New("index corrupt")

// ExitCode maps an error to the appropriate CLI exit code.
// Uses errors.Is for sentinel matching: nil → 0, unmatched → 1.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	switch {
	case errors.Is(err, ErrUsage):
		return 2
	case errors.Is(err, ErrRepoNotFound):
		return 3
	case errors.Is(err, ErrSchemaMismatch):
		return 4
	case errors.Is(err, ErrIndexCorrupt):
		return 5
	default:
		return 1
	}
}
