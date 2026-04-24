// Package binutil provides allocation-free helpers for reading and
// writing fixed-endian wire formats used by AppleTalk and AFP packets.
//
// The package does not define Marshaler/Unmarshaler interfaces itself;
// those live at call sites where the concrete framing is known. The
// Wire interface below is the canonical shape:
//
//	type Wire interface {
//	    MarshalWire(b []byte) (n int, err error)
//	    UnmarshalWire(b []byte) (n int, err error)
//	    WireSize() int
//	}
//
// Implementations should return io.ErrShortBuffer when the buffer is
// too small, and a more specific error when the payload is malformed.
package binutil

import (
	"encoding/binary"
	"errors"
	"io"
)

// ErrShortBuffer is returned when a caller-supplied buffer is too
// small to hold the marshalled form, or too short to decode.
var ErrShortBuffer = io.ErrShortBuffer

// ErrMalformed indicates that the bytes do not conform to the expected
// wire format (bad length prefix, invalid enum, etc.).
var ErrMalformed = errors.New("binutil: malformed wire data")

// PutU8 writes v at b[0] and returns the number of bytes written.
// Returns ErrShortBuffer if len(b) < 1.
func PutU8(b []byte, v uint8) (int, error) {
	if len(b) < 1 {
		return 0, ErrShortBuffer
	}
	b[0] = v
	return 1, nil
}

// PutU16 writes v big-endian at b[0:2].
func PutU16(b []byte, v uint16) (int, error) {
	if len(b) < 2 {
		return 0, ErrShortBuffer
	}
	binary.BigEndian.PutUint16(b, v)
	return 2, nil
}

// PutU32 writes v big-endian at b[0:4].
func PutU32(b []byte, v uint32) (int, error) {
	if len(b) < 4 {
		return 0, ErrShortBuffer
	}
	binary.BigEndian.PutUint32(b, v)
	return 4, nil
}

// PutU64 writes v big-endian at b[0:8].
func PutU64(b []byte, v uint64) (int, error) {
	if len(b) < 8 {
		return 0, ErrShortBuffer
	}
	binary.BigEndian.PutUint64(b, v)
	return 8, nil
}

// GetU8 reads a uint8 from b[0].
func GetU8(b []byte) (uint8, int, error) {
	if len(b) < 1 {
		return 0, 0, ErrShortBuffer
	}
	return b[0], 1, nil
}

// GetU16 reads a big-endian uint16 from b[0:2].
func GetU16(b []byte) (uint16, int, error) {
	if len(b) < 2 {
		return 0, 0, ErrShortBuffer
	}
	return binary.BigEndian.Uint16(b), 2, nil
}

// GetU32 reads a big-endian uint32 from b[0:4].
func GetU32(b []byte) (uint32, int, error) {
	if len(b) < 4 {
		return 0, 0, ErrShortBuffer
	}
	return binary.BigEndian.Uint32(b), 4, nil
}

// GetU64 reads a big-endian uint64 from b[0:8].
func GetU64(b []byte) (uint64, int, error) {
	if len(b) < 8 {
		return 0, 0, ErrShortBuffer
	}
	return binary.BigEndian.Uint64(b), 8, nil
}

// PutPString writes a length-prefixed Pascal string: 1 byte length
// followed by s. Returns ErrMalformed if len(s) > 255.
func PutPString(b []byte, s []byte) (int, error) {
	if len(s) > 255 {
		return 0, ErrMalformed
	}
	need := 1 + len(s)
	if len(b) < need {
		return 0, ErrShortBuffer
	}
	b[0] = uint8(len(s))
	copy(b[1:], s)
	return need, nil
}

// GetPString reads a length-prefixed Pascal string. The returned slice
// aliases b; callers that retain it across further writes must copy.
func GetPString(b []byte) ([]byte, int, error) {
	if len(b) < 1 {
		return nil, 0, ErrShortBuffer
	}
	n := int(b[0])
	if len(b) < 1+n {
		return nil, 0, ErrShortBuffer
	}
	return b[1 : 1+n], 1 + n, nil
}
