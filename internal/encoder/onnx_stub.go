//go:build !encoder

// Package encoder stub: Default() returns (nil, ErrUnsupported) in default builds.
// This file has zero non-stdlib dependencies.
package encoder

// Default returns a nil Encoder and ErrUnsupported in default (non-encoder) builds.
//
// Callers should treat this as a soft failure: log a warning and fall back to
// BM25-only retrieval rather than propagating the error.
func Default() (Encoder, error) {
	return nil, ErrUnsupported
}
