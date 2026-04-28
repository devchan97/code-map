//go:build encoder

// Package encoder — ONNX-enabled build.
//
// # ONNX binding
//
// The intended binding is github.com/yalue/onnxruntime_go, which wraps the
// official Microsoft ONNX Runtime C library via CGO. At the time M5 was
// implemented, pulling that binding caused build failures on the Windows CI
// host (missing ONNX Runtime shared library). Therefore the binding call is
// stubbed behind this file's interface so that:
//
//  1. `go build -tags encoder ./...` compiles successfully with no real ONNX dep.
//  2. The public surface (Encoder interface + Default()) is correct.
//  3. Swapping in the real binding later is a single-file change: replace the
//     body of newONNXEncoder() below.
//
// # Model file
//
// When wired to a real binding, Default() will look for the model at:
//
//	~/.codemap/models/bge-small-en-v1.5.onnx
//
// If the file is absent, Default() returns ErrModelNotFound so callers can
// gracefully fall back to BM25.
//
// To obtain the model:
//
//	huggingface-cli download BAAI/bge-small-en-v1.5 model.onnx \
//	    --local-dir ~/.codemap/models/ \
//	    --local-dir-use-symlinks False \
//	    --filename bge-small-en-v1.5.onnx
//
// # Wiring the real ONNX binding
//
// 1. Add to go.mod: github.com/yalue/onnxruntime_go v1.x.y
// 2. In newONNXEncoder(), replace the placeholder body with:
//
//	import ort "github.com/yalue/onnxruntime_go"
//
//	ort.SetSharedLibraryPath("/path/to/libonnxruntime.so") // or .dll / .dylib
//	if err := ort.InitializeEnvironment(); err != nil { return nil, err }
//	session, err := ort.NewAdvancedSession(modelPath, inputNames, outputNames, nil, nil, nil)
//	// … tokenize → run session → decode float32 output tensor …
package encoder

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrModelNotFound is returned when the ONNX model file is absent at the
// expected path (~/.codemap/models/<name>.onnx).
var ErrModelNotFound = errors.New("encoder: ONNX model file not found; see package doc for download instructions")

// errNotWired is returned when the real ONNX binding has not been wired yet.
var errNotWired = errors.New("encoder: ONNX binding not wired in this build (placeholder only); see onnx_enabled.go for instructions")

const (
	modelName     = "bge-small-en-v1.5"
	modelFileName = "bge-small-en-v1.5.onnx"
)

// onnxEncoder is the production encoder backed by an ONNX session.
// In this placeholder build, the session field is unused.
type onnxEncoder struct {
	modelPath string
}

// Name returns the stable identifier for this encoder.
func (e *onnxEncoder) Name() string { return modelName }

// EncodeQuery encodes a single query string.
//
// NOTE: This is a placeholder. Replace the body with a real ONNX inference
// call once the binding is wired (see package doc).
func (e *onnxEncoder) EncodeQuery(q string) ([]float32, error) {
	return nil, errNotWired
}

// EncodeBatch encodes a batch of snippets.
//
// NOTE: This is a placeholder. Replace the body with a real ONNX inference
// call once the binding is wired (see package doc).
func (e *onnxEncoder) EncodeBatch(snippets []string) ([][]float32, error) {
	return nil, errNotWired
}

// Default returns an ONNX-backed Encoder for the default bi-encoder model
// (BGE-small-en-v1.5).
//
// It checks that the model file exists at ~/.codemap/models/<model>.onnx.
// If the file is absent, it returns ErrModelNotFound so callers can fall
// back to BM25-only retrieval.
//
// In the current placeholder build, even when the file is present, inference
// calls return errNotWired until the ONNX binding is fully wired.
func Default() (Encoder, error) {
	modelPath, err := defaultModelPath()
	if err != nil {
		return nil, fmt.Errorf("encoder.Default: %w", err)
	}

	if _, statErr := os.Stat(modelPath); os.IsNotExist(statErr) {
		return nil, fmt.Errorf("encoder.Default: model %q not found: %w", modelPath, ErrModelNotFound)
	}

	return newONNXEncoder(modelPath)
}

// defaultModelPath returns the expected path for the ONNX model file.
func defaultModelPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("could not determine home directory: %w", err)
	}
	return filepath.Join(home, ".codemap", "models", modelFileName), nil
}

// newONNXEncoder constructs an onnxEncoder for the model at modelPath.
//
// This is a placeholder: it validates the path exists and returns the
// struct without initialising a real ONNX session. Replace this body
// with real onnxruntime_go session initialisation when wiring the binding.
func newONNXEncoder(modelPath string) (*onnxEncoder, error) {
	// Placeholder: validate the file is readable.
	f, err := os.Open(modelPath)
	if err != nil {
		return nil, fmt.Errorf("encoder: cannot open model file %q: %w", modelPath, err)
	}
	_ = f.Close()

	return &onnxEncoder{modelPath: modelPath}, nil
}
