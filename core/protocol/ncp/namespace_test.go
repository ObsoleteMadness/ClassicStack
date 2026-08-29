package ncp

import (
	"errors"
	"testing"
)

// TestParseHPath_Basic decodes a well-formed NW_HPATH with two Pascal-string
// components and checks every field plus the consumed byte count.
func TestParseHPath_Basic(t *testing.T) {
	b := []byte{
		0x03,                   // volume
		0x01, 0x02, 0x03, 0x04, // base
		HPathFlagBase, // flag
		2,             // components
		3, 'f', 'o', 'o',
		3, 'b', 'a', 'r',
		0xAA, // trailing byte not part of the path
	}
	h, n, err := ParseHPath(b)
	if err != nil {
		t.Fatalf("ParseHPath: %v", err)
	}
	if h.Volume != 0x03 {
		t.Errorf("Volume = %#x, want 0x03", h.Volume)
	}
	if h.Base != [4]byte{0x01, 0x02, 0x03, 0x04} {
		t.Errorf("Base = %v, want [1 2 3 4]", h.Base)
	}
	if h.Flag != HPathFlagBase {
		t.Errorf("Flag = %#x, want HPathFlagBase", h.Flag)
	}
	if want := []string{"foo", "bar"}; len(h.Components) != 2 || h.Components[0] != want[0] || h.Components[1] != want[1] {
		t.Errorf("Components = %v, want %v", h.Components, want)
	}
	if n != len(b)-1 {
		t.Errorf("consumed = %d, want %d (excluding trailing byte)", n, len(b)-1)
	}
}

// TestParseHPath_ZeroComponents decodes a path anchored purely by handle/base,
// with no trailing Pascal strings.
func TestParseHPath_ZeroComponents(t *testing.T) {
	b := []byte{0x01, 0x00, 0x00, 0x00, 0x2A, HPathFlagHandle, 0}
	h, n, err := ParseHPath(b)
	if err != nil {
		t.Fatalf("ParseHPath: %v", err)
	}
	if len(h.Components) != 0 {
		t.Errorf("Components = %v, want none", h.Components)
	}
	if n != len(b) {
		t.Errorf("consumed = %d, want %d", n, len(b))
	}
}

// TestParseHPath_Rejects covers every truncation point ParseHPath must reject with
// ErrShortHPath: below the fixed header, a components count that overruns the
// buffer, and a component length byte whose declared length overruns the buffer.
func TestParseHPath_Rejects(t *testing.T) {
	cases := map[string][]byte{
		"empty":                     {},
		"shorter than fixed header": {0x01, 0x00, 0x00, 0x00, 0x00, HPathFlagNone},
		"components count truncated, no room for length byte": {
			0x01, 0x00, 0x00, 0x00, 0x00, HPathFlagNone, 1, // says 1 component, 0 bytes follow
		},
		"component length overruns buffer": {
			0x01, 0x00, 0x00, 0x00, 0x00, HPathFlagNone, 1,
			5, 'a', 'b', // length byte says 5, only 2 bytes follow
		},
	}
	for name, b := range cases {
		t.Run(name, func(t *testing.T) {
			if _, _, err := ParseHPath(b); !errors.Is(err, ErrShortHPath) {
				t.Fatalf("ParseHPath(%v) err = %v, want ErrShortHPath", b, err)
			}
		})
	}
}

// TestHPath_BaseHandle checks the little-endian reassembly of the 4-byte base,
// including the aliasing convention (Flag==HPathFlagHandle → low byte is the
// short dir handle, still readable through BaseHandle's low byte).
func TestHPath_BaseHandle(t *testing.T) {
	h := &HPath{Base: [4]byte{0x78, 0x56, 0x34, 0x12}}
	if got, want := h.BaseHandle(), uint32(0x12345678); got != want {
		t.Fatalf("BaseHandle() = %#x, want %#x", got, want)
	}

	handle := &HPath{Flag: HPathFlagHandle, Base: [4]byte{0x2A, 0, 0, 0}}
	if got := handle.BaseHandle() & 0xFF; got != 0x2A {
		t.Fatalf("short-handle low byte = %#x, want 0x2A", got)
	}
}

// TestMarshalDirInfo_EmptyMask asserts an infomask of 0 appends nothing —
// MarshalDirInfo must not emit any section unless its bit is set.
func TestMarshalDirInfo_EmptyMask(t *testing.T) {
	e := DirEntryInfo{Name: "ignored", Size: 42}
	if got := e.MarshalDirInfo(0, nil); len(got) != 0 {
		t.Fatalf("MarshalDirInfo(0, nil) = %v, want empty", got)
	}
}

// TestMarshalDirInfo_EntryName pins the entry-name section's wire layout: a
// 1-byte Pascal length followed by the raw name bytes, little-endian family
// (though this particular section has no multi-byte fields).
func TestMarshalDirInfo_EntryName(t *testing.T) {
	e := DirEntryInfo{Name: "readme.txt"}
	got := e.MarshalDirInfo(InfoMskEntryName, nil)
	want := append([]byte{byte(len("readme.txt"))}, "readme.txt"...)
	if string(got) != string(want) {
		t.Fatalf("MarshalDirInfo(InfoMskEntryName) = %v, want %v", got, want)
	}
}

// TestMarshalDirInfo_DataStreamSize pins the little-endian 4-byte size field, and
// that MarshalDirInfo appends to (not replaces) an existing dst slice.
func TestMarshalDirInfo_DataStreamSize(t *testing.T) {
	e := DirEntryInfo{Size: 0x01020304}
	prefix := []byte{0xEE}
	got := e.MarshalDirInfo(InfoMskDataStreamSize, prefix)
	want := []byte{0xEE, 0x04, 0x03, 0x02, 0x01} // LE
	if string(got) != string(want) {
		t.Fatalf("MarshalDirInfo(InfoMskDataStreamSize) = %v, want %v", got, want)
	}
}

// TestMarshalDirInfo_TotalDataStreamSize pins the size(LE32) + stream-count(1)
// layout, the one section with a trailing fixed byte after the LE32 field.
func TestMarshalDirInfo_TotalDataStreamSize(t *testing.T) {
	e := DirEntryInfo{Size: 0x00000005}
	got := e.MarshalDirInfo(InfoMskTotalDataStreamSz, nil)
	want := []byte{0x05, 0x00, 0x00, 0x00, 0x01}
	if string(got) != string(want) {
		t.Fatalf("MarshalDirInfo(InfoMskTotalDataStreamSz) = %v, want %v", got, want)
	}
}

// TestMarshalDirInfo_AscendingBitOrder proves multiple set bits are appended in
// ascending bit order (matching mars_nwe's build_dir_info), not request-field
// declaration order.
func TestMarshalDirInfo_AscendingBitOrder(t *testing.T) {
	e := DirEntryInfo{Size: 7, Attributes: 0x20}
	got := e.MarshalDirInfo(InfoMskAttributeInfo|InfoMskDataStreamSpace, nil)
	// InfoMskDataStreamSpace (0x02) sorts before InfoMskAttributeInfo (0x04).
	want := []byte{0x07, 0, 0, 0, 0x20, 0, 0, 0}
	if string(got) != string(want) {
		t.Fatalf("MarshalDirInfo(space|attr) = %v, want %v (DataStreamSpace before AttributeInfo)", got, want)
	}
}
