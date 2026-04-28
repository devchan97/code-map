package encoder

import (
	"math"
	"testing"
)

func TestEncodeDecodeVecBlob_Roundtrip(t *testing.T) {
	cases := [][]float32{
		nil,
		{},
		{0},
		{1, 2, 3.5, -0.25, math.MaxFloat32, -math.MaxFloat32},
	}
	for _, in := range cases {
		blob := EncodeVecBlob(in)
		got, err := DecodeVecBlob(blob)
		if err != nil {
			t.Fatalf("Decode(%v): %v", in, err)
		}
		if len(got) != len(in) {
			t.Errorf("len mismatch: got %d, want %d", len(got), len(in))
		}
		for i := range in {
			if got[i] != in[i] {
				t.Errorf("idx %d: got %v want %v", i, got[i], in[i])
			}
		}
	}
}

func TestDecodeVecBlob_TooShort(t *testing.T) {
	_, err := DecodeVecBlob([]byte{1, 2})
	if err == nil {
		t.Error("expected error for blob < 4 bytes")
	}
}

func TestDecodeVecBlob_LengthMismatch(t *testing.T) {
	// header says 5 elements, but only 4 bytes of payload follow.
	b := []byte{5, 0, 0, 0, 0, 0, 0, 0}
	_, err := DecodeVecBlob(b)
	if err == nil {
		t.Error("expected length mismatch error")
	}
}
