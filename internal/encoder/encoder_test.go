// Package encoder_test contains interface-contract tests that run in BOTH
// build configurations (default and -tags encoder).
package encoder_test

import (
	"github.com/devchan97/code-map/internal/encoder"
)

// Compile-time assertion: ensure the Encoder interface is exported and has
// the three required methods. A concrete fake type must satisfy it without
// any build-tag gating.
type fakeEncoder struct{}

func (f *fakeEncoder) Name() string                             { return "fake" }
func (f *fakeEncoder) EncodeQuery(q string) ([]float32, error) { return []float32{1, 0}, nil }
func (f *fakeEncoder) EncodeBatch(snips []string) ([][]float32, error) {
	out := make([][]float32, len(snips))
	for i := range snips {
		out[i] = []float32{1, 0}
	}
	return out, nil
}

// Compile-time check that fakeEncoder satisfies the Encoder interface.
var _ encoder.Encoder = (*fakeEncoder)(nil)
