package core

import (
	"errors"
	"fmt"
	"testing"
)

func TestExitCode(t *testing.T) {
	tests := []struct {
		err  error
		want int
	}{
		{nil, 0},
		{errors.New("generic"), 1},
		{ErrUsage, 2},
		{ErrRepoNotFound, 3},
		{ErrSchemaMismatch, 4},
		{ErrIndexCorrupt, 5},
		{fmt.Errorf("wrap: %w", ErrUsage), 2},
		{fmt.Errorf("wrap: %w", ErrRepoNotFound), 3},
		{fmt.Errorf("wrap: %w", ErrSchemaMismatch), 4},
		{fmt.Errorf("wrap: %w", ErrIndexCorrupt), 5},
		{fmt.Errorf("layered: %w", fmt.Errorf("inner: %w", ErrUsage)), 2},
	}
	for _, tt := range tests {
		got := ExitCode(tt.err)
		if got != tt.want {
			t.Errorf("ExitCode(%v) = %d, want %d", tt.err, got, tt.want)
		}
	}
}
