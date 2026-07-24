package smb

import (
	"testing"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"
)

// TestBuildNetShareEnumRequest checks the RAP NetShareEnum request shape: WCT=14, the
// \PIPE\LANMAN transaction name, the "WrLeh"/"B13BWz" descriptors + level 1, and — the
// bug that made real Win98 misframe the reply — MaxParameterCount = 8 (NOT the receive
// buffer length).
func TestBuildNetShareEnumRequest(t *testing.T) {
	b := &Builder{PID: 0xFEFF, TID: 0x1234, UID: 5}
	req := b.BuildNetShareEnum()

	h, err := DecodeHeader(req)
	if err != nil {
		t.Fatalf("decode header: %v", err)
	}
	if h.Command != CommandTransaction {
		t.Fatalf("command = 0x%02x, want TRANSACTION 0x%02x", h.Command, CommandTransaction)
	}
	wct := int(req[HeaderLen])
	if wct != 14 {
		t.Fatalf("WCT = %d, want 14", wct)
	}
	w := req[HeaderLen+1 : HeaderLen+1+2*wct]
	if got := bp.LE16(w[4:6]); got != rapNetShareEnumReplyParamLen {
		t.Errorf("MaxParameterCount = %d, want %d (a too-large value misframes the Win98 reply)", got, rapNetShareEnumReplyParamLen)
	}
	// The transaction byte area must carry the pipe name and RAP descriptors.
	for _, want := range []string{`\PIPE\LANMAN`, "WrLeh", "B13BWz"} {
		if indexOf(req, want) < 0 {
			t.Errorf("request missing %q", want)
		}
	}
}

// TestParseNetShareEnumResponse round-trips a server-shaped NetShareEnum reply (the exact
// SHARE_INFO_1 layout core/service/smb.buildNetShareEnumResponse produces) through the
// client parser and checks the names, types, and remark heap resolve.
func TestParseNetShareEnumResponse(t *testing.T) {
	shares := []struct {
		name    string
		typ     uint16
		comment string
	}{
		{"C-DRIVE", ShareTypeDisk, "Comment"},
		{"MY DOCUMENTS", ShareTypeDisk, "My Docs"},
		{"IPC$", ShareTypeIPC, ""},
	}

	const entrySize = 20
	remarkBase := len(shares) * entrySize
	remarkOff := remarkBase
	remarks := make([]byte, 0)
	offs := make([]int, len(shares))
	for i, s := range shares {
		offs[i] = remarkOff
		remarks = append(remarks, []byte(s.comment)...)
		remarks = append(remarks, 0)
		remarkOff += len(s.comment) + 1
	}
	data := make([]byte, remarkBase+len(remarks))
	for i, s := range shares {
		base := i * entrySize
		copy(data[base:base+12], s.name)
		bp.PutLE16(data[base+14:base+16], s.typ)
		bp.PutLE32(data[base+16:base+20], uint32(offs[i]))
	}
	copy(data[remarkBase:], remarks)

	params := make([]byte, 8) // Status(2)+Converter(2)+EntriesReturned(2)+EntriesAvailable(2)
	bp.PutLE16(params[4:6], uint16(len(shares)))
	bp.PutLE16(params[6:8], uint16(len(shares)))

	resp := buildTestTransactionResponse(params, data)

	got, err := ParseNetShareEnum(resp)
	if err != nil {
		t.Fatalf("ParseNetShareEnum: %v", err)
	}
	if len(got) != len(shares) {
		t.Fatalf("parsed %d shares, want %d", len(got), len(shares))
	}
	for i, s := range shares {
		if got[i].Name != s.name || got[i].Type != s.typ || got[i].Comment != s.comment {
			t.Errorf("share %d = %+v, want {%q %d %q}", i, got[i], s.name, s.typ, s.comment)
		}
	}
}

// buildTestTransactionResponse assembles a WCT=10 SMB_COM_TRANSACTION response carrying
// the given RAP param/data blocks at header-relative offsets — the reply shape the client
// parser reads.
func buildTestTransactionResponse(params, data []byte) []byte {
	h := Header{Command: CommandTransaction, Flags: FlagReply}
	out := h.Encode(nil)
	out = append(out, 10) // WCT
	w := make([]byte, 20)
	paramOff := HeaderLen + 1 + 20 + 2
	dataOff := paramOff + len(params)
	bp.PutLE16(w[0:2], uint16(len(params))) // TotalParameterCount
	bp.PutLE16(w[2:4], uint16(len(data)))   // TotalDataCount
	bp.PutLE16(w[6:8], uint16(len(params))) // ParameterCount
	bp.PutLE16(w[8:10], uint16(paramOff))   // ParameterOffset
	bp.PutLE16(w[12:14], uint16(len(data))) // DataCount
	bp.PutLE16(w[14:16], uint16(dataOff))   // DataOffset
	out = append(out, w...)
	bcc := len(params) + len(data)
	out = append(out, byte(bcc), byte(bcc>>8))
	out = append(out, params...)
	out = append(out, data...)
	return out
}

// indexOf reports the first index of sub in b, or -1.
func indexOf(b []byte, sub string) int {
	s := []byte(sub)
	for i := 0; i+len(s) <= len(b); i++ {
		if string(b[i:i+len(s)]) == sub {
			return i
		}
	}
	return -1
}
