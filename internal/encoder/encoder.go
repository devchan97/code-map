// Package encoder defines the optional dense-encoder interface used by codemap's
// rerank stage. Real ONNX-backed implementations are gated behind the "encoder" build tag.
//
// # Build modes
//
//   - Default build (`go build ./...`): Default() returns ErrUnsupported. Zero non-stdlib deps.
//   - Encoder build (`go build -tags encoder ./...`): Default() attempts to load an ONNX model
//     from ~/.codemap/models/<name>.onnx (e.g. bge-small-en-v1.5.onnx). The ONNX binding
//     used is github.com/yalue/onnxruntime_go (see onnx_enabled.go for details).
//
// # Adding a new encoder
//
// Implement the Encoder interface and return your implementation from Default() inside
// a new file guarded by the appropriate build tag.
package encoder

import "errors"

// Encoder represents a dense embedding model.
//
// Encoders are not bundled in the default codemap binary. Build with
//
//	go build -tags encoder
//
// to include an ONNX runtime and a default model.
type Encoder interface {
	// Name returns a stable identifier for the encoder (e.g. "bge-small-en").
	Name() string

	// EncodeQuery encodes a single query string and returns its embedding vector.
	EncodeQuery(q string) ([]float32, error)

	// EncodeBatch encodes a batch of snippets and returns their embedding vectors.
	// The returned slice has the same length as snippets.
	EncodeBatch(snippets []string) ([][]float32, error)
}

// ErrUnsupported is returned by Default in builds that do not include the encoder
// runtime, or when no model file is available. The caller should log a warning and
// fall back to BM25-only retrieval rather than returning an error to the user.
var ErrUnsupported = errors.New("encoder: unsupported in this build; rebuild with -tags encoder")

// ErrNotBuilt is an alias for ErrUnsupported kept for backward compatibility.
//
// Deprecated: use ErrUnsupported.
var ErrNotBuilt = ErrUnsupported
