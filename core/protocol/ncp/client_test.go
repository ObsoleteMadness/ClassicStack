package ncp

import (
	"bytes"
	"testing"
)

// client_test.go verifies the CLIENT-direction request builders emit exactly the wire
// framing the server-direction UnmarshalRequest parses, and that the reply parsers read
// the reply-body layouts the file service emits. These are the regression guards added
// AFTER the in-process e2e (client/ncp) confirmed the client round-trips against the
// real service.

// TestRequestHeaderFraming checks marshalRequest produces the 6-byte NCP header +
// function byte UnmarshalRequest reads back, with the sequence bumped and the
// connection split low/high.
func TestRequestHeaderFraming(t *testing.T) {
	r := &Requester{Conn: 0x0102, Task: 7}
	pkt := r.Request(fnGetServerDateTime, []byte{0xAA, 0xBB})

	h, err := UnmarshalRequest(pkt)
	if err != nil {
		t.Fatalf("UnmarshalRequest: %v", err)
	}
	if h.Type != TypeRequest {
		t.Errorf("Type = 0x%04X, want TypeRequest", h.Type)
	}
	if h.SequenceNumber != 1 {
		t.Errorf("SequenceNumber = %d, want 1 (bumped from 0)", h.SequenceNumber)
	}
	if h.ConnectionNumber() != 0x0102 {
		t.Errorf("ConnectionNumber = 0x%04X, want 0x0102", h.ConnectionNumber())
	}
	if h.TaskNumber != 7 {
		t.Errorf("TaskNumber = %d, want 7", h.TaskNumber)
	}
	if h.Function != fnGetServerDateTime {
		t.Errorf("Function = 0x%02X, want 0x%02X", h.Function, fnGetServerDateTime)
	}
	if !bytes.Equal(h.Body, []byte{0xAA, 0xBB}) {
		t.Errorf("Body = % X, want AA BB", h.Body)
	}

	// A second request bumps the sequence.
	pkt2 := r.Request(fnGetServerDateTime, nil)
	h2, _ := UnmarshalRequest(pkt2)
	if h2.SequenceNumber != 2 {
		t.Errorf("second SequenceNumber = %d, want 2", h2.SequenceNumber)
	}
}

// TestControlFraming checks CreateConnection / DestroyConnection carry the control type
// and no function/body.
func TestControlFraming(t *testing.T) {
	r := &Requester{}
	h, err := UnmarshalRequest(r.CreateConnection())
	if err != nil {
		t.Fatalf("UnmarshalRequest: %v", err)
	}
	if h.Type != TypeCreateConnection {
		t.Errorf("Type = 0x%04X, want TypeCreateConnection", h.Type)
	}
	if h.Function != 0 || h.Body != nil {
		t.Errorf("control packet carried a function/body: fn=0x%02X body=% X", h.Function, h.Body)
	}
	h2, _ := UnmarshalRequest(r.DestroyConnection())
	if h2.Type != TypeDestroyConnection {
		t.Errorf("Type = 0x%04X, want TypeDestroyConnection", h2.Type)
	}
}

// TestSubfunctionFraming checks a multiplexed (0x16/0x17) request carries the 2-byte
// big-endian subfunction-length then the subfunction byte then the args — the layout
// dispatch.go's subfunction() reads.
func TestSubfunctionFraming(t *testing.T) {
	r := &Requester{}
	pkt := r.BuildGetVolumeNumber("SYS")
	h, err := UnmarshalRequest(pkt)
	if err != nil {
		t.Fatalf("UnmarshalRequest: %v", err)
	}
	if h.Function != fnDirServices {
		t.Fatalf("Function = 0x%02X, want fnDirServices", h.Function)
	}
	// Body: sflen[2 BE], subfunction, then length-prefixed "SYS".
	if len(h.Body) < 3 {
		t.Fatalf("body too short: % X", h.Body)
	}
	sflen := int(h.Body[0])<<8 | int(h.Body[1])
	if sflen != 1+1+len("SYS") { // subfunction + name-length byte + "SYS"
		t.Errorf("subfunction length = %d, want %d", sflen, 1+1+len("SYS"))
	}
	if h.Body[2] != sf16GetVolumeNumber {
		t.Errorf("subfunction = 0x%02X, want 0x%02X", h.Body[2], sf16GetVolumeNumber)
	}
	if h.Body[3] != byte(len("SYS")) || string(h.Body[4:7]) != "SYS" {
		t.Errorf("name field = % X, want length-prefixed SYS", h.Body[3:])
	}
}

// TestReadReplyPadOnOddOffset checks ParseReadReply skips the alignment pad byte the
// server inserts when the read offset is odd, and returns the data unpadded.
func TestReadReplyPadOnOddOffset(t *testing.T) {
	data := []byte("hello")
	// Reply body: size[2 BE], pad byte (odd offset), data.
	body := append([]byte{0x00, byte(len(data)), 0x00}, data...)
	got, err := ParseReadReply(body, 1) // odd offset → pad present
	if err != nil {
		t.Fatalf("ParseReadReply: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("data = %q, want %q", got, data)
	}

	// Even offset → no pad byte.
	bodyEven := append([]byte{0x00, byte(len(data))}, data...)
	got, err = ParseReadReply(bodyEven, 0)
	if err != nil {
		t.Fatalf("ParseReadReply even: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("even-offset data = %q, want %q", got, data)
	}
}

// TestOpenReplyRoundTrip checks ParseOpenReply reads the file-handle, name, and size
// from an open/create reply body shaped like fileio.go openFile emits.
func TestOpenReplyRoundTrip(t *testing.T) {
	var body []byte
	body = append(body, 0, 0, 0, 0, 0x12, 0x34) // ext_fhandle[2] + fhandle[4] (id 0x1234 low)
	body = append(body, 0, 0)                   // reserved[2]
	var name [14]byte
	copy(name[:], "REPORT.TXT")
	body = append(body, name[:]...)
	body = appendBE32(body, 4096) // size

	o, err := ParseOpenReply(body)
	if err != nil {
		t.Fatalf("ParseOpenReply: %v", err)
	}
	if o.FileHandle != [6]byte{0, 0, 0, 0, 0x12, 0x34} {
		t.Errorf("FileHandle = % X, want ...12 34", o.FileHandle)
	}
	if o.Name != "REPORT.TXT" {
		t.Errorf("Name = %q, want REPORT.TXT", o.Name)
	}
	if o.Size != 4096 {
		t.Errorf("Size = %d, want 4096", o.Size)
	}
}

// TestVolumeInfoBytes checks ParseVolumeInfo reads the block counts and computes
// total/free bytes.
func TestVolumeInfoBytes(t *testing.T) {
	var body []byte
	body = appendBE16(body, 1)    // sectors per block
	body = appendBE16(body, 1000) // total blocks
	body = appendBE16(body, 400)  // avail blocks
	body = appendBE16(body, 0xFFFF)
	body = appendBE16(body, 0xFFFF)
	var name [16]byte
	copy(name[:], "SYS")
	body = append(body, name[:]...)
	body = appendBE16(body, 0) // removable

	vi, err := ParseVolumeInfo(body)
	if err != nil {
		t.Fatalf("ParseVolumeInfo: %v", err)
	}
	if vi.Name != "SYS" {
		t.Errorf("Name = %q, want SYS", vi.Name)
	}
	if vi.TotalBytes() != 1000*blockSize {
		t.Errorf("TotalBytes = %d, want %d", vi.TotalBytes(), 1000*blockSize)
	}
	if vi.FreeBytes() != 400*blockSize {
		t.Errorf("FreeBytes = %d, want %d", vi.FreeBytes(), 400*blockSize)
	}
}
