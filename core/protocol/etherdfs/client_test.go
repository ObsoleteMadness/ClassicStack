package etherdfs

import (
	"bytes"
	"testing"
)

// client_test.go verifies the CLIENT-direction encoders/decoders round-trip against the
// server-direction Decode*Request / *Reply.Encode in the same package — the regression
// guards added AFTER the in-process e2e (client/etherdfs) confirmed the client
// round-trips against the real service.

// TestOpenRequestRoundTrip: EncodeOpenRequest → DecodeOpenRequest preserves the SS/CC/MM
// prefix and path, for the always-3-word open-family layout.
func TestOpenRequestRoundTrip(t *testing.T) {
	in := OpenRequest{Attr: 0x0021, Action: 0x0002, OpenMode: 0x0042, Path: "\\DIR\\FILE.TXT"}
	got, err := DecodeOpenRequest(EncodeOpenRequest(in))
	if err != nil {
		t.Fatalf("DecodeOpenRequest: %v", err)
	}
	if got != in {
		t.Errorf("round trip = %+v, want %+v", got, in)
	}
}

// TestReadWriteRequestRoundTrip: the read/write request bodies survive encode→decode.
func TestReadWriteRequestRoundTrip(t *testing.T) {
	r := ReadRequest{Offset: 0x12345678, FileID: 0xABCD, Length: 512}
	gotR, err := DecodeReadRequest(EncodeReadRequest(r))
	if err != nil {
		t.Fatalf("DecodeReadRequest: %v", err)
	}
	if gotR != r {
		t.Errorf("read round trip = %+v, want %+v", gotR, r)
	}

	w := WriteRequest{Offset: 0x00ABCDEF, FileID: 0x1234, Data: []byte("payload")}
	gotW, err := DecodeWriteRequest(EncodeWriteRequest(w))
	if err != nil {
		t.Fatalf("DecodeWriteRequest: %v", err)
	}
	if gotW.Offset != w.Offset || gotW.FileID != w.FileID || !bytes.Equal(gotW.Data, w.Data) {
		t.Errorf("write round trip = %+v, want %+v", gotW, w)
	}
}

// TestRenameRequestRoundTrip: the length-prefixed source + destination survive.
func TestRenameRequestRoundTrip(t *testing.T) {
	in := RenameRequest{Src: "\\OLD.TXT", Dst: "\\NEW.TXT"}
	got, err := DecodeRenameRequest(EncodeRenameRequest(in))
	if err != nil {
		t.Fatalf("DecodeRenameRequest: %v", err)
	}
	if got != in {
		t.Errorf("rename round trip = %+v, want %+v", got, in)
	}
}

// TestFindNextRequestRoundTrip: dir ID/position/attr/mask survive.
func TestFindNextRequestRoundTrip(t *testing.T) {
	in := FindNextRequest{DirID: 0x0102, Position: 0x0304, Attr: 0x16, Mask: FilenameToFCB("*.TXT")}
	got, err := DecodeFindNextRequest(EncodeFindNextRequest(in))
	if err != nil {
		t.Fatalf("DecodeFindNextRequest: %v", err)
	}
	if got != in {
		t.Errorf("findnext round trip = %+v, want %+v", got, in)
	}
}

// TestOpenReplyRoundTrip: server OpenReply.Encode → client DecodeOpenReply.
func TestOpenReplyRoundTrip(t *testing.T) {
	in := OpenReply{
		Attr:   AttrArchive,
		FCB:    FilenameToFCB("REPORT.TXT"),
		Time:   0xDEADBEEF,
		Size:   4096,
		FileID: 0x0042,
		Action: 1,
		Mode:   2,
	}
	got, err := DecodeOpenReply(in.Encode(nil))
	if err != nil {
		t.Fatalf("DecodeOpenReply: %v", err)
	}
	if got != in {
		t.Errorf("open reply round trip = %+v, want %+v", got, in)
	}
}

// TestFindReplyRoundTrip: server FindReply.Encode → client DecodeFindReply.
func TestFindReplyRoundTrip(t *testing.T) {
	in := FindReply{
		Attr:     AttrDirectory,
		FCB:      FilenameToFCB("SUBDIR"),
		Time:     0x11223344,
		Size:     0,
		DirID:    0x0007,
		Position: 0x0003,
	}
	got, err := DecodeFindReply(in.Encode(nil))
	if err != nil {
		t.Fatalf("DecodeFindReply: %v", err)
	}
	if got != in {
		t.Errorf("find reply round trip = %+v, want %+v", got, in)
	}
}

// TestGetAttrReplyRoundTrip: server GetAttrReply.Encode → client DecodeGetAttrReply.
func TestGetAttrReplyRoundTrip(t *testing.T) {
	in := GetAttrReply{Time: 0xCAFEBABE, Size: 123456, Attr: AttrReadOnly | AttrArchive}
	got, err := DecodeGetAttrReply(in.Encode(nil))
	if err != nil {
		t.Fatalf("DecodeGetAttrReply: %v", err)
	}
	if got != in {
		t.Errorf("getattr reply round trip = %+v, want %+v", got, in)
	}
}

// TestDiskSpaceReplyRoundTrip: server DiskSpaceReply.Encode → client DecodeDiskSpaceReply.
func TestDiskSpaceReplyRoundTrip(t *testing.T) {
	in := DiskSpaceReply{TotalClusters: 1000, FreeClusters: 250}
	total, bps, free, err := DecodeDiskSpaceReply(in.Encode(nil))
	if err != nil {
		t.Fatalf("DecodeDiskSpaceReply: %v", err)
	}
	if total != 1000 || free != 250 || bps != diskSpaceBytesPerSector {
		t.Errorf("diskspace = total %d bps %d free %d, want 1000/%d/250", total, bps, free, diskSpaceBytesPerSector)
	}
}

// TestWriteSeekReplyRoundTrip: the write/seek reply words survive.
func TestWriteSeekReplyRoundTrip(t *testing.T) {
	n, err := DecodeWriteReply(WriteReply(777))
	if err != nil || n != 777 {
		t.Fatalf("write reply = %d err %v, want 777", n, err)
	}
	off, err := DecodeSeekReply(SeekReply(0x89ABCDEF))
	if err != nil || off != 0x89ABCDEF {
		t.Fatalf("seek reply = 0x%X err %v, want 0x89ABCDEF", off, err)
	}
}

// TestRequestFrameRoundTrip: a client-built request frame encodes and parses back with
// the drive/opcode/sequence/payload intact (the header codec is shared, but this pins
// the client's use of it).
func TestRequestFrameRoundTrip(t *testing.T) {
	body := EncodePathRequest("\\DIR")
	f := Frame{
		DstMAC:   [6]byte{0x02, 0, 0, 0, 0, 0xED},
		SrcMAC:   [6]byte{0x02, 0, 0, 0, 0, 0x01},
		Sequence: 42,
		Drive:    2, // C:
		Opcode:   OpChdir,
		Payload:  body,
	}
	got, err := ParseFrame(f.Encode(nil))
	if err != nil {
		t.Fatalf("ParseFrame: %v", err)
	}
	if got.Sequence != 42 || got.Drive != 2 || got.Opcode != OpChdir {
		t.Errorf("frame header = seq %d drive %d op 0x%02X, want 42/2/0x%02X",
			got.Sequence, got.Drive, got.Opcode, OpChdir)
	}
	if !bytes.Equal(got.Payload, body) {
		t.Errorf("frame payload = % X, want % X", got.Payload, body)
	}
}
