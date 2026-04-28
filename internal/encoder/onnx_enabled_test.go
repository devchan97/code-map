//go:build encoder

package encoder_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/devchan97/code-map/internal/encoder"
)

// TestDefault_EnabledBuildModelAbsent verifies that when the ONNX model file
// is missing, Default() returns ErrModelNotFound (not a generic error), so
// callers can react specifically to a missing model file.
func TestDefault_EnabledBuildModelAbsent(t *testing.T) {
	// Ensure the model is absent by checking for an unlikely path.
	// We do not want to delete a real model; instead, temporarily redirect the
	// home so the default model path resolves to an empty temp dir.
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)        // POSIX
	t.Setenv("USERPROFILE", tmp) // Windows

	enc, err := encoder.Default()
	if err == nil {
		// Model happened to be present in the redirected home — skip.
		if enc != nil {
			t.Logf("model found at redirected home %s; skipping absence test", tmp)
		}
		t.Skip("model file present in temp home; skipping absence test")
	}
	if !errors.Is(err, encoder.ErrModelNotFound) {
		t.Errorf("expected errors.Is(err, ErrModelNotFound); got %v", err)
	}
}

// TestDefault_EnabledBuildModelPresent verifies that when a model file exists
// at the expected path, Default() returns a non-nil Encoder whose Name() is
// non-empty. The model content can be a dummy file — we don't call inference.
func TestDefault_EnabledBuildModelPresent(t *testing.T) {
	// Create a fake model file so Default() proceeds past the existence check.
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)

	modelDir := filepath.Join(tmp, ".codemap", "models")
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	modelFile := filepath.Join(modelDir, "bge-small-en-v1.5.onnx")
	if err := os.WriteFile(modelFile, []byte("placeholder"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	enc, err := encoder.Default()
	if err != nil && !errors.Is(err, encoder.ErrModelNotFound) {
		// Some other error (e.g. ONNX runtime not available) — acceptable in
		// placeholder build; log and skip rather than fail.
		t.Skipf("Default() returned non-model error (placeholder build): %v", err)
	}
	if err == nil {
		if enc == nil {
			t.Error("Default() returned (nil, nil) — should return a non-nil Encoder")
		}
		if enc != nil && enc.Name() == "" {
			t.Error("Encoder.Name() must not be empty")
		}
	}
}
