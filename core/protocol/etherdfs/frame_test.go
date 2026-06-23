package etherdfs

import (
	"bytes"
	"testing"
)

// makeFrame builds a minimal valid request frame for ParseFrame tests.
func makeFrame(t *testing.T, cks bool, payload []byte) []byte {
	t.Helper()
	f := Frame{
		DstMAC:   [6]byte{0x02, 0, 0, 0, 0, 0x01},
		SrcMAC:   [6]byte{0x02, 0, 0, 0, 0, 0x02},
		Sequence: 7,
		Drive:    3,
		Opcode:   OpGetattr,
		CKS:      cks,
		Payload:  payload,
	}
	return f.Encode(nil)
}

func TestFrameRoundTrip(t *testing.T) {
	for _, cks := range []bool{false, true} {
		payload := []byte("C:\\AUTOEXEC.BAT")
		b := makeFrame(t, cks, payload)
		if len(b) < MinFrameLen {
			t.Fatalf("encoded frame shorter than minimum: %d", len(b))
		}
		f, err := ParseFrame(b)
		if err != nil {
			t.Fatalf("ParseFrame(cks=%v): %v", cks, err)
		}
		if f.Sequence != 7 || f.Drive != 3 || f.Opcode != OpGetattr {
			t.Errorf("header mismatch: %+v", f)
		}
		if f.CKS != cks {
			t.Errorf("CKS = %v, want %v", f.CKS, cks)
		}
		if !bytes.Equal(f.Payload, payload) {
			t.Errorf("payload = %q, want %q", f.Payload, payload)
		}
		if f.SrcMAC != [6]byte{0x02, 0, 0, 0, 0, 0x02} {
			t.Errorf("SrcMAC = %v", f.SrcMAC)
		}
	}
}

func TestParseFrameRejectsWrongEtherType(t *testing.T) {
	b := makeFrame(t, false, nil)
	b[12], b[13] = 0x08, 0x00 // IPv4
	if _, err := ParseFrame(b); err != ErrEtherType {
		t.Fatalf("err = %v, want ErrEtherType", err)
	}
}

func TestParseFrameRejectsWrongVersion(t *testing.T) {
	b := makeFrame(t, false, nil)
	b[offVersion] = (b[offVersion] & cksFlag) | 0x03 // version 3
	if _, err := ParseFrame(b); err != ErrVersion {
		t.Fatalf("err = %v, want ErrVersion", err)
	}
}

func TestParseFrameRejectsBadChecksum(t *testing.T) {
	b := makeFrame(t, true, []byte("hello"))
	// Corrupt a payload byte after the checksum was computed.
	b[headerEnd]++
	if _, err := ParseFrame(b); err != ErrChecksum {
		t.Fatalf("err = %v, want ErrChecksum", err)
	}
}

func TestParseFrameShort(t *testing.T) {
	if _, err := ParseFrame(make([]byte, MinFrameLen-1)); err != ErrShort {
		t.Fatalf("err = %v, want ErrShort", err)
	}
}

func TestReplySwapsMACs(t *testing.T) {
	req, err := ParseFrame(makeFrame(t, false, nil))
	if err != nil {
		t.Fatal(err)
	}
	srv := [6]byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF}
	rep := req.Reply(srv, StatusReply(ErrNone))
	if rep.DstMAC != req.SrcMAC {
		t.Errorf("reply DstMAC = %v, want request SrcMAC %v", rep.DstMAC, req.SrcMAC)
	}
	if rep.SrcMAC != srv {
		t.Errorf("reply SrcMAC = %v, want server %v", rep.SrcMAC, srv)
	}
	if rep.Sequence != req.Sequence {
		t.Errorf("reply Sequence = %d, want %d", rep.Sequence, req.Sequence)
	}
}

func TestBSDChecksum(t *testing.T) {
	// The rotate-and-add checksum is order-sensitive: "AB" and "BA" must differ.
	if BSDChecksum([]byte("AB")) == BSDChecksum([]byte("BA")) {
		t.Error("BSD checksum is not order-sensitive")
	}
	// Empty input is zero.
	if got := BSDChecksum(nil); got != 0 {
		t.Errorf("BSDChecksum(nil) = %d, want 0", got)
	}
}

func TestFCBRoundTrip(t *testing.T) {
	cases := map[string]string{
		"REPORT~1.XLS": "REPORT~1.XLS",
		"readme.txt":   "README.TXT",
		"COMMAND.COM":  "COMMAND.COM",
		"NOEXT":        "NOEXT",
		"a.b":          "A.B",
	}
	for in, want := range cases {
		fcb := FilenameToFCB(in)
		if got := FCBToFilename(fcb); got != want {
			t.Errorf("FCB round-trip %q: got %q, want %q", in, got, want)
		}
	}
	// The FCB byte form must be exactly 11 bytes, space-padded, dot-free.
	fcb := FilenameToFCB("A.B")
	if len(fcb) != FCBNameLen {
		t.Fatalf("FCB length = %d", len(fcb))
	}
	if bytes.ContainsRune(fcb[:], '.') {
		t.Errorf("FCB must not contain a dot: %q", fcb[:])
	}
	if fcb[0] != 'A' || fcb[8] != 'B' || fcb[1] != ' ' {
		t.Errorf("FCB layout wrong: %q", fcb[:])
	}
}

func TestNormalizePath(t *testing.T) {
	cases := map[string]string{
		`C:\FOO\BAR.TXT`: "FOO/BAR.TXT",
		`\FOO\BAR`:       "FOO/BAR",
		`C:FOO`:          "FOO",
		`FOO/BAR`:        "FOO/BAR",
		`C:\`:            "",
		``:               "",
	}
	for in, want := range cases {
		if got := NormalizePath(in); got != want {
			t.Errorf("NormalizePath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestReplyDTOEncodings(t *testing.T) {
	if got := (DiskSpaceReply{SectorsPerCluster: 8, BytesPerSector: 512, TotalClusters: 100, FreeClusters: 50}).Encode(nil); len(got) != 10 {
		t.Errorf("DiskSpaceReply len = %d, want 10", len(got))
	}
	if got := (GetAttrReply{Size: 1234, Attr: AttrArchive}).Encode(nil); len(got) != 9 {
		t.Errorf("GetAttrReply len = %d, want 9", len(got))
	}
	if got := (FindReply{FCB: FilenameToFCB("A.TXT")}).Encode(nil); len(got) != 1+FCBNameLen+12 {
		t.Errorf("FindReply len = %d, want %d", len(got), 1+FCBNameLen+12)
	}
	// SPOPNFIL open reply carries the extra 2-byte action result.
	plain := (OpenReply{}).Encode(nil)
	withAction := (OpenReply{HasAction: true}).Encode(nil)
	if len(withAction) != len(plain)+2 {
		t.Errorf("SPOPNFIL reply should be 2 bytes longer: %d vs %d", len(withAction), len(plain))
	}
}

func TestDecodeRequests(t *testing.T) {
	rd, err := DecodeReadRequest([]byte{0x10, 0, 0, 0, 0x05, 0, 0x00, 0x02})
	if err != nil || rd.Offset != 0x10 || rd.FileID != 5 || rd.Length != 0x200 {
		t.Errorf("DecodeReadRequest = %+v, err=%v", rd, err)
	}
	rn, err := DecodeRenameRequest(append([]byte{3}, []byte("OLDNEW")...))
	if err != nil || rn.Src != "OLD" || rn.Dst != "NEW" {
		t.Errorf("DecodeRenameRequest = %+v, err=%v", rn, err)
	}
	op, err := DecodeOpenRequest([]byte{0x20, 0x00, 0x01, 0x00, 'F', 'O', 'O'}, true)
	if err != nil || op.Attr != 0x20 || op.Action != 1 || op.Path != "FOO" {
		t.Errorf("DecodeOpenRequest = %+v, err=%v", op, err)
	}
	if _, err := DecodeReadRequest([]byte{1, 2, 3}); err != ErrBadRequest {
		t.Errorf("short read request err = %v, want ErrBadRequest", err)
	}
}
