//go:build afp || all

package afp

import (
	"bytes"
	"testing"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"
	protocol "github.com/ObsoleteMadness/ClassicStack/core/protocol/afp"
)

// TestEnumEntry_LayoutNoPadByte pins the FPEnumerate per-entry framing that the
// M7 refactor got wrong: a framed entry is [len][type][params] — exactly TWO
// bytes before the params, with any even-length pad applied at the TAIL, never a
// pad byte between the type byte and the params. The refactor inserted that
// extra pad byte, shifting the params one byte right so every client mis-read
// the name-offset word. This test reproduces the exact read a Mac performs
// (name at byte 2 + nameOffset) and fails if the pad byte ever comes back.
func TestEnumEntry_LayoutNoPadByte(t *testing.T) {
	t.Parallel()

	// A minimal LongName-only param block: a 2-byte offset word (LongName is the
	// lowest requested bit, so its offset is the first field), then the pstring in
	// the variable area. fixedSize is 2 (just the offset word), so the name sits
	// at offset 2 from the start of the params.
	var params []byte
	params = bp.AppendBE16(params, 2) // name offset = fixedSize(2) + 0
	params = putPString(params, []byte("alpha.txt"))

	entry := enumEntry(false, params) // a file

	// entry[0] is the length byte (covering the whole entry).
	if int(entry[0]) != len(entry) {
		t.Fatalf("length byte = %d, want %d (the full entry length)", entry[0], len(entry))
	}
	// entry[1] is the type byte: 0x00 for a file (0x80 for a dir), NOT a pad.
	if entry[1] != 0 {
		t.Fatalf("type byte = %#x, want 0x00 (file)", entry[1])
	}
	// The params must begin at byte 2 (len + type), with NO pad byte. A Mac reads
	// the name at (2 + nameOffset) within the framed entry.
	nameOff := int(bp.BE16(entry[2:4]))
	namePos := 2 + nameOff
	got, _, ok := pString(entry, namePos)
	if !ok || string(got) != "alpha.txt" {
		t.Fatalf("name decoded from byte 2+offset = %q (ok=%v), want %q; a stray pad byte after the type byte shifts this", got, ok, "alpha.txt")
	}

	// Directory entries set the type byte high bit.
	dir := enumEntry(true, params)
	if dir[1] != isDirFlag {
		t.Fatalf("dir type byte = %#x, want %#x", dir[1], isDirFlag)
	}
	// Even total length (word alignment) always holds.
	if len(entry)%2 != 0 || len(dir)%2 != 0 {
		t.Fatalf("entry lengths not even: file=%d dir=%d", len(entry), len(dir))
	}
}

// TestFPEnumerateRes_Header pins the FPEnumerate reply header:
// FileBitmap(2) DirBitmap(2) ActCount(2) then the entries verbatim.
func TestFPEnumerateRes_Header(t *testing.T) {
	t.Parallel()
	res := &FPEnumerateRes{
		FileDirBitmaps: protocol.FileDirBitmaps{FileBitmap: 0x07FB, DirBitmap: 0x0DFF},
		ActCount:       3,
		Entries:        []byte{0xAA, 0xBB},
	}
	want := []byte{0x07, 0xFB, 0x0D, 0xFF, 0x00, 0x03, 0xAA, 0xBB}
	if got := res.Marshal(); !bytes.Equal(got, want) {
		t.Fatalf("FPEnumerateRes header drift:\n got:  %x\n want: %x", got, want)
	}
}

// TestFPEnumerateRes_MarshalGolden holds FPEnumerateRes.Marshal to the exact bytes
// the pre-refactor DTO produced (service/afp fpenumerateres_basic.hex), so the
// FPEnumerate reply framing can never drift from the known-good main-branch wire
// format again.
func TestFPEnumerateRes_MarshalGolden(t *testing.T) {
	t.Parallel()
	res := &FPEnumerateRes{
		FileDirBitmaps: protocol.FileDirBitmaps{FileBitmap: 0x07FB, DirBitmap: 0x0DFF},
		ActCount:       3,
		Entries:        []byte("enumerate-payload"),
	}
	got := res.Marshal()
	want := goldenBytes(t, "fpenumerateres_basic.hex", got)
	if !bytes.Equal(got, want) {
		t.Fatalf("FPEnumerateRes marshal drift:\n got:  %x\n want: %x", got, want)
	}
}

// TestDirIDReplyGolden pins the FPOpenDir / FPCreateDir reply, which is just the
// 4-byte big-endian directory id, to the pre-refactor wire bytes. Both handlers
// build it with bp.AppendBE32(nil, did); this guards that encoding.
func TestDirIDReplyGolden(t *testing.T) {
	t.Parallel()
	// fpopendirres_basic.hex == 0xCAFEF00D, fpcreatedirres_basic.hex == 0xDEADBEEF.
	openDir := bp.AppendBE32(nil, 0xCAFEF00D)
	if want := goldenBytes(t, "fpopendirres_basic.hex", openDir); !bytes.Equal(openDir, want) {
		t.Fatalf("FPOpenDir DID reply drift:\n got:  %x\n want: %x", openDir, want)
	}
	createDir := bp.AppendBE32(nil, 0xDEADBEEF)
	if want := goldenBytes(t, "fpcreatedirres_basic.hex", createDir); !bytes.Equal(createDir, want) {
		t.Fatalf("FPCreateDir DID reply drift:\n got:  %x\n want: %x", createDir, want)
	}
}

// TestFPGetFileDirParmsRes_Header pins the FPGetFileDirParms reply header for
// both a file (00 00 type/pad) and a directory (80 00): FileBitmap(2)
// DirBitmap(2) type(1) pad(1) then the params. The refactor once collapsed the
// two bitmaps into one word here (2 bytes short) — this guards against that.
func TestFPGetFileDirParmsRes_Header(t *testing.T) {
	t.Parallel()
	bitmaps := protocol.FileDirBitmaps{FileBitmap: 0x07FB, DirBitmap: 0x0DFF}
	file := (&FPGetFileDirParmsRes{FileDirBitmaps: bitmaps, IsDir: false, Params: []byte{0xAA}}).Marshal()
	if want := []byte{0x07, 0xFB, 0x0D, 0xFF, 0x00, 0x00, 0xAA}; !bytes.Equal(file, want) {
		t.Fatalf("file header drift:\n got:  %x\n want: %x", file, want)
	}
	dir := (&FPGetFileDirParmsRes{FileDirBitmaps: bitmaps, IsDir: true, Params: []byte{0xAA}}).Marshal()
	if want := []byte{0x07, 0xFB, 0x0D, 0xFF, 0x80, 0x00, 0xAA}; !bytes.Equal(dir, want) {
		t.Fatalf("dir header drift:\n got:  %x\n want: %x", dir, want)
	}
}
