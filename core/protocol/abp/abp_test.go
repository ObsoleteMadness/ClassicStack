package abp

import (
	"bytes"
	"errors"
	"testing"
)

// TestBootPktRplyFixture pins the wire layout to the reference server's
// (NetBoot.py) struct.pack('>BBHLHHhL', 2, 1, osID, userData, blockSize,
// imageID, result, imageSize).ljust(586, b'\0').
func TestBootPktRplyFixture(t *testing.T) {
	got := BootPktRply{
		OSID:      0x1234,
		UserData:  0xDEADBEEF,
		BlockSize: 512,
		ImageID:   7,
		Result:    -1,
		ImageSize: 0x00010203,
	}.Marshal()

	want := []byte{
		2, 1, // Command, pversion
		0x12, 0x34, // osID
		0xDE, 0xAD, 0xBE, 0xEF, // userData
		0x02, 0x00, // blockSize 512
		0x00, 0x07, // imageID
		0xFF, 0xFF, // result -1
		0x00, 0x01, 0x02, 0x03, // imageSize
	}
	if len(got) != DDPMaxData {
		t.Fatalf("reply length = %d, want %d", len(got), DDPMaxData)
	}
	if !bytes.Equal(got[:len(want)], want) {
		t.Fatalf("reply header = % X, want % X", got[:len(want)], want)
	}
	for i, b := range got[len(want):] {
		if b != 0 {
			t.Fatalf("userRecord byte %d = %#x, want zero fill", len(want)+i, b)
		}
	}

	var back BootPktRply
	if err := back.Unmarshal(got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if back.OSID != 0x1234 || back.UserData != 0xDEADBEEF || back.BlockSize != 512 ||
		back.ImageID != 7 || back.Result != -1 || back.ImageSize != 0x00010203 {
		t.Fatalf("round-trip mismatch: %+v", back)
	}
}

func TestUserRecordRequestRoundTrip(t *testing.T) {
	in := UserRecordRequest{MachineID: 1, Timestamp: 0xCAFEF00D, UserName: []byte("Patrick")}
	wire := in.Marshal()
	if len(wire) != 42 {
		t.Fatalf("request length = %d, want 42", len(wire))
	}
	// Header fixture: >BBHL then 34-byte pascal userName field.
	want := []byte{1, 1, 0x00, 0x01, 0xCA, 0xFE, 0xF0, 0x0D, 7, 'P', 'a', 't', 'r', 'i', 'c', 'k'}
	if !bytes.Equal(wire[:len(want)], want) {
		t.Fatalf("request prefix = % X, want % X", wire[:len(want)], want)
	}
	var out UserRecordRequest
	if err := out.Unmarshal(wire); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if out.MachineID != in.MachineID || out.Timestamp != in.Timestamp || !bytes.Equal(out.UserName, in.UserName) {
		t.Fatalf("round-trip mismatch: %+v", out)
	}
}

func TestUserRecordRequestVersionGate(t *testing.T) {
	wire := UserRecordRequest{}.Marshal()
	wire[1] = 2 // clients/servers trash version > 1
	var out UserRecordRequest
	if err := out.Unmarshal(wire); !errors.Is(err, ErrVersion) {
		t.Fatalf("version 2 err = %v, want ErrVersion", err)
	}
}

func TestBootImageRequestRoundTrip(t *testing.T) {
	in := BootImageRequest{ImageID: 3, Section: 0, Flags: 0x80, ReplyDelay: 9, Bitmap: []byte{0xFF, 0x01}}
	var out BootImageRequest
	if err := out.Unmarshal(in.Marshal()); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if out.ImageID != 3 || out.Flags != 0x80 || out.ReplyDelay != 9 || !bytes.Equal(out.Bitmap, in.Bitmap) {
		t.Fatalf("round-trip mismatch: %+v", out)
	}

	// The real client can send an empty (buggy) bitmap — must parse fine.
	var empty BootImageRequest
	if err := empty.Unmarshal([]byte{3, 1, 0, 3, 0, 0, 0, 9}); err != nil {
		t.Fatalf("empty-bitmap Unmarshal: %v", err)
	}
	if len(empty.Bitmap) != 0 {
		t.Fatalf("empty bitmap parsed as %d bytes", len(empty.Bitmap))
	}
}

func TestBootBlockRoundTrip(t *testing.T) {
	in := BootBlock{ImageID: 0, BlockNo: 4087, Data: bytes.Repeat([]byte{0xAB}, DiskSector)}
	wire := in.Marshal()
	if len(wire) != 6+DiskSector {
		t.Fatalf("block length = %d, want %d", len(wire), 6+DiskSector)
	}
	// blockNo is 0-based on the wire (spec/19 errata).
	if wire[4] != 0x0F || wire[5] != 0xF7 {
		t.Fatalf("blockNo bytes = %#x %#x, want 0x0f 0xf7", wire[4], wire[5])
	}
	var out BootBlock
	if err := out.Unmarshal(wire); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if out.BlockNo != 4087 || !bytes.Equal(out.Data, in.Data) {
		t.Fatalf("round-trip mismatch")
	}
}

// TestChainReadRequestFixture pins the layout to the live ChainLoader packet
// (ltoudp-netboot capture 2026-07-16, frame 54):
// 80 00 0001 00000000 00000000 00000002 — 16 bytes exactly.
func TestChainReadRequestFixture(t *testing.T) {
	wire := []byte{
		0x80, 0x00,
		0x00, 0x01, // seq 1
		0x00, 0x00, 0x00, 0x00, // imageNum 0 ("configuration mode")
		0x00, 0x00, 0x00, 0x00, // blockOffset 0
		0x00, 0x00, 0x00, 0x02, // blockCount 2 (the disk's boot blocks)
	}
	var out ChainReadRequest
	if err := out.Unmarshal(wire); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if out.Seq != 1 || out.ImageNum != 0 || out.BlockOffset != 0 || out.BlockCount != 2 {
		t.Fatalf("parse mismatch: %+v", out)
	}
	if !bytes.Equal(ChainReadRequest{Seq: 1, BlockCount: 2}.Marshal(), wire) {
		t.Fatalf("Marshal mismatch")
	}
}

// TestChainReadDataFixture pins the layout to ChainBoot.py's build:
// struct.pack('>BBH', 129, blk-boot_blkoffset, boot_seq) + thisblk.
func TestChainReadDataFixture(t *testing.T) {
	in := ChainReadData{BlkIndex: 31, Seq: 42, Data: bytes.Repeat([]byte{0x5A}, ChainBlockSize)}
	wire := in.Marshal()
	if wire[0] != 129 || wire[1] != 31 || wire[2] != 0 || wire[3] != 42 {
		t.Fatalf("header = % X", wire[:4])
	}
	var out ChainReadData
	if err := out.Unmarshal(wire); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if out.BlkIndex != 31 || out.Seq != 42 || !bytes.Equal(out.Data, in.Data) {
		t.Fatalf("round-trip mismatch")
	}
}

// TestChainWriteBlockFixture pins the layout to ChainBoot.py's parse:
// boot_type, blk, seq, boot_imgnum, hunk_start = struct.unpack_from('>BBHLL', whole_data)
// with the data payload at whole_data[8:]... which is offset 12 of the packet
// (BBHLL = 12 bytes).
func TestChainWriteBlockFixture(t *testing.T) {
	data := bytes.Repeat([]byte{0x77}, 512)
	in := ChainWriteBlock{BlkIndex: 5, Seq: 9, ImageNum: 1, HunkStart: 64, Data: data}
	wire := in.Marshal()
	want := []byte{130, 5, 0, 9, 0, 0, 0, 1, 0, 0, 0, 64}
	if !bytes.Equal(wire[:12], want) {
		t.Fatalf("header = % X, want % X", wire[:12], want)
	}
	var out ChainWriteBlock
	if err := out.Unmarshal(wire); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if out.BlkIndex != 5 || out.Seq != 9 || out.ImageNum != 1 || out.HunkStart != 64 || !bytes.Equal(out.Data, data) {
		t.Fatalf("round-trip mismatch: %+v", out)
	}
}

// TestChainWriteAckFixture pins the layout to ChainBoot.py's build:
// struct.pack('>BBH', 131, 0, seq).
func TestChainWriteAckFixture(t *testing.T) {
	wire := ChainWriteAck{Seq: 9}.Marshal()
	if !bytes.Equal(wire, []byte{131, 0, 0, 9}) {
		t.Fatalf("ack = % X", wire)
	}
	var out ChainWriteAck
	if err := out.Unmarshal(wire); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if out.Seq != 9 {
		t.Fatalf("seq = %d", out.Seq)
	}
}

func TestShortAndWrongCommand(t *testing.T) {
	var r UserRecordRequest
	if err := r.Unmarshal([]byte{1}); !errors.Is(err, ErrShort) {
		t.Fatalf("short err = %v", err)
	}
	if err := r.Unmarshal(BootImageRequest{}.Marshal()); !errors.Is(err, ErrCommand) {
		t.Fatalf("wrong-cmd err = %v", err)
	}
}
