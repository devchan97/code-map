//go:build !encoder

package encoder_test

import (
	"errors"
	"testing"

	"github.com/devchan97/code-map/internal/encoder"
)

// TestDefault_StubReturnsNilEncoder verifies that Default() returns a nil
// Encoder in the default (non-encoder) build, so callers that attempt to use
// a nil encoder are immediately caught.
func TestDefault_StubReturnsNilEncoder(t *testing.T) {
	enc, err := encoder.Default()
	if enc != nil {
		t.Errorf("expected nil Encoder in default build; got %T(%v)", enc, enc)
	}
	if err == nil {
		t.Fatal("expected non-nil error from Default() in stub build")
	}
}

// TestDefault_StubReturnsErrUnsupported verifies the sentinel error.
func TestDefault_StubReturnsErrUnsupported(t *testing.T) {
	_, err := encoder.Default()
	if !errors.Is(err, encoder.ErrUnsupported) {
		t.Errorf("expected errors.Is(err, ErrUnsupported); got %v", err)
	}
}

// TestDefault_StubCompatibleWithErrNotBuilt verifies that the deprecated
// ErrNotBuilt alias still matches via errors.Is (backward compat).
func TestDefault_StubCompatibleWithErrNotBuilt(t *testing.T) {
	_, err := encoder.Default()
	if !errors.Is(err, encoder.ErrNotBuilt) {
		t.Errorf("expected errors.Is(err, ErrNotBuilt) for backward compat; got %v", err)
	}
}
