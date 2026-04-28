package encoder

import (
	"encoding/binary"
	"fmt"
	"math"
)

// EncodeVecBlob serialises a []float32 to a binary blob.
// Format: 4-byte little-endian uint32 element count, followed by
// len(v)*4 bytes of little-endian IEEE-754 float32 values.
func EncodeVecBlob(v []float32) []byte {
	b := make([]byte, 4+len(v)*4)
	binary.LittleEndian.PutUint32(b[:4], uint32(len(v)))
	for i, f := range v {
		binary.LittleEndian.PutUint32(b[4+i*4:], math.Float32bits(f))
	}
	return b
}

// DecodeVecBlob deserialises a []float32 from a blob produced by EncodeVecBlob.
// Returns an error if the blob is shorter than 4 bytes or the declared length
// does not match the remaining payload.
func DecodeVecBlob(b []byte) ([]float32, error) {
	if len(b) < 4 {
		return nil, fmt.Errorf("encoder: blob too short (%d bytes, need at least 4)", len(b))
	}
	n := int(binary.LittleEndian.Uint32(b[:4]))
	if len(b) != 4+n*4 {
		return nil, fmt.Errorf("encoder: blob length mismatch: header declares %d elements (%d payload bytes), got %d payload bytes",
			n, n*4, len(b)-4)
	}
	v := make([]float32, n)
	for i := range v {
		bits := binary.LittleEndian.Uint32(b[4+i*4:])
		v[i] = math.Float32frombits(bits)
	}
	return v, nil
}
