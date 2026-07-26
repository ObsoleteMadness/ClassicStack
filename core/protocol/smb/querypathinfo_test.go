package smb

import (
	"testing"
	"time"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"
)

// timeToFiletime is the inverse of filetimeToTime, for building test fixtures.
func timeToFiletime(t time.Time) uint64 {
	return uint64(t.UnixNano()/100) + filetimeEpochDelta100ns
}

// makeTrans2Response frames a minimal SMB_COM_TRANSACTION2 response (WCT=10) carrying data
// as its data block, so ParseQueryPathInfo can be exercised without a live server.
func makeTrans2Response(data []byte) []byte {
	const wct = 10
	h := Header{Command: CommandTransaction2, Status: StatusSuccess, Flags: 0x80}
	out := h.Encode(nil)
	out = append(out, wct)
	words := make([]byte, 2*wct)
	// DataCount at w[12:14], DataOffset at w[14:16]; the rest (param counts/offsets) 0.
	dataOff := HeaderLen + 1 + 2*wct + 2 // header + WCT + words + BCC
	bp.PutLE16(words[12:14], uint16(len(data)))
	bp.PutLE16(words[14:16], uint16(dataOff))
	out = append(out, words...)
	bcc := len(data)
	out = append(out, byte(bcc), byte(bcc>>8)) // ByteCount
	out = append(out, data...)
	return out
}

// TestParseQueryPathInfoBasicInfo round-trips SMB_QUERY_FILE_BASIC_INFO: build the 40-byte
// data block with four FILETIMEs + attributes, frame it, and confirm ParseQueryPathInfo
// decodes the timestamps and attributes.
func TestParseQueryPathInfoBasicInfo(t *testing.T) {
	create := time.Date(1999, 4, 24, 15, 22, 0, 0, time.UTC)
	access := time.Date(2026, 7, 6, 17, 0, 0, 0, time.UTC)
	write := time.Date(2026, 7, 7, 4, 31, 14, 0, time.UTC)
	change := write

	data := make([]byte, 40)
	bp.PutLE64(data[0:8], timeToFiletime(create))
	bp.PutLE64(data[8:16], timeToFiletime(access))
	bp.PutLE64(data[16:24], timeToFiletime(write))
	bp.PutLE64(data[24:32], timeToFiletime(change))
	bp.PutLE32(data[32:36], uint32(AttrHidden|AttrSystem))
	// data[36:40] Reserved

	resp := makeTrans2Response(data)
	bi, err := ParseQueryPathInfo(resp)
	if err != nil {
		t.Fatalf("ParseQueryPathInfo: %v", err)
	}
	if !bi.CreateTime.Equal(create) {
		t.Errorf("CreateTime = %v, want %v", bi.CreateTime, create)
	}
	if !bi.WriteTime.Equal(write) {
		t.Errorf("WriteTime = %v, want %v", bi.WriteTime, write)
	}
	if bi.Attrs != (AttrHidden | AttrSystem) {
		t.Errorf("Attrs = %#x, want %#x", bi.Attrs, AttrHidden|AttrSystem)
	}
	if bi.IsDir() {
		t.Error("IsDir = true, want false (no directory bit set)")
	}
}

// TestFiletimeZero confirms a zero FILETIME decodes to the zero time (so callers can test
// IsZero and fall back), and that a zero UTIME does too.
func TestFiletimeZero(t *testing.T) {
	if !filetimeToTime(0).IsZero() {
		t.Error("filetimeToTime(0) should be the zero time")
	}
	if !utimeToTime(0).IsZero() {
		t.Error("utimeToTime(0) should be the zero time")
	}
}
