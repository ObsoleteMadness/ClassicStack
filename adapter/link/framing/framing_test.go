package framing

import (
	"errors"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/adapter/link/inmem"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/ddp"
)

// sampleDatagram is a hand-built DDP datagram used across the round-trip tests.
func sampleDatagram() ddp.Datagram {
	return ddp.Datagram{
		Hops:        0,
		DestNetwork: 0x1234,
		SrcNetwork:  0x5678,
		DestNode:    0x10,
		SrcNode:     0x20,
		DestSocket:  253,
		SrcSocket:   254,
		DDPType:     2,
		Data:        []byte("hello-ddp"),
	}
}

// TestEncodeDecode_RoundTrip asserts the Ethernet/SNAP framing is reversible at
// the byte level: decode(encode(d)) == d.
func TestEncodeDecode_RoundTrip(t *testing.T) {
	src := []byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55}
	dst := []byte{0x09, 0x00, 0x07, 0xFF, 0xFF, 0xFF}
	in := sampleDatagram()

	frame, err := encode(nil, src, dst, in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	// Verify the SNAP/LLC header is present at the expected offset.
	if !equal(frame[14:17], llcSNAP) || !equal(frame[17:22], snapAppleTalk) {
		t.Fatalf("encoded frame missing LLC/SNAP AppleTalk header: % x", frame[14:22])
	}

	out, err := decode(frame)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	assertDatagramEqual(t, in, out)
}

// TestFramer_DatagramLinkRoundTrip drives a ddp.Datagram through the Framer over
// an in-memory FrameLink and back, proving the FrameLink->DatagramLink seam.
func TestFramer_DatagramLinkRoundTrip(t *testing.T) {
	fl := inmem.Loopback(4)
	defer fl.Close()

	framer := &EtherTalk{SrcMAC: []byte{1, 2, 3, 4, 5, 6}}
	dl, err := framer.Framing(fl)
	if err != nil {
		t.Fatalf("Framing: %v", err)
	}

	in := sampleDatagram()
	if err := dl.WriteDatagram(in); err != nil {
		t.Fatalf("WriteDatagram: %v", err)
	}
	out, err := dl.ReadDatagram()
	if err != nil {
		t.Fatalf("ReadDatagram: %v", err)
	}
	assertDatagramEqual(t, in, out)
}

// TestDecode_SkipsNonAppleTalk verifies a non-AppleTalk SNAP frame (e.g. AARP
// PID 0x000080F3) is rejected as ErrNotAppleTalk rather than mis-parsed.
func TestDecode_SkipsNonAppleTalk(t *testing.T) {
	// Build a minimal Ethernet/SNAP frame with the AARP PID.
	frame := make([]byte, 0, 32)
	frame = append(frame, 0x09, 0x00, 0x07, 0xFF, 0xFF, 0xFF) // dst
	frame = append(frame, 0, 0, 0, 0, 0, 0)                   // src
	frame = append(frame, 0x00, 0x1E)                         // length (arbitrary >= 8)
	frame = append(frame, llcSNAP...)
	frame = append(frame, 0x00, 0x00, 0x00, 0x80, 0xF3) // AARP SNAP PID
	frame = append(frame, make([]byte, 18)...)          // filler payload

	if _, err := decode(frame); !errors.Is(err, ErrNotAppleTalk) {
		t.Fatalf("decode of AARP frame: got %v, want ErrNotAppleTalk", err)
	}
}

// TestReadDatagram_DropsForeignUnicast proves the inbound destination filter: a
// DDP frame addressed to another node's unicast MAC (not ours, not broadcast, not
// multicast) is skipped, not handed up. Without this a router that forwards a
// datagram out this port re-ingests its own echoed frame and forwards it again,
// looping until the DDP 15-hop limit — the regression this guards against.
func TestReadDatagram_DropsForeignUnicast(t *testing.T) {
	fl := inmem.Loopback(4)
	defer fl.Close()

	station := []byte{1, 2, 3, 4, 5, 6}
	framer := &EtherTalk{SrcMAC: station}
	dl, err := framer.Framing(fl)
	if err != nil {
		t.Fatalf("Framing: %v", err)
	}

	// A frame destined for a DIFFERENT unicast MAC (a foreign node on the wire),
	// tagged with a distinguishing destination socket so we can prove it is the one
	// dropped rather than returned.
	foreign := []byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF}
	foreignDG := sampleDatagram()
	foreignDG.DestSocket = 0x42
	frame, err := encode(nil, station, foreign, foreignDG)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if err := fl.Write(frame); err != nil {
		t.Fatalf("Write: %v", err)
	}
	// Follow it with a frame addressed to us so ReadDatagram has something valid to
	// return once it has skipped the foreign one.
	oursDG := sampleDatagram() // its own (different) DestSocket
	ours, err := encode(nil, station, station, oursDG)
	if err != nil {
		t.Fatalf("encode ours: %v", err)
	}
	if err := fl.Write(ours); err != nil {
		t.Fatalf("Write ours: %v", err)
	}

	out, err := dl.ReadDatagram()
	if err != nil {
		t.Fatalf("ReadDatagram: %v", err)
	}
	if out.DestSocket == foreignDG.DestSocket {
		t.Fatalf("ReadDatagram returned the foreign-unicast frame (socket %#x); it must be dropped", out.DestSocket)
	}
	assertDatagramEqual(t, oursDG, out)
}

func assertDatagramEqual(t *testing.T, want, got ddp.Datagram) {
	t.Helper()
	if want.Hops != got.Hops || want.DestNetwork != got.DestNetwork ||
		want.SrcNetwork != got.SrcNetwork || want.DestNode != got.DestNode ||
		want.SrcNode != got.SrcNode || want.DestSocket != got.DestSocket ||
		want.SrcSocket != got.SrcSocket || want.DDPType != got.DDPType {
		t.Fatalf("datagram header mismatch:\n want %+v\n  got %+v", want, got)
	}
	if string(want.Data) != string(got.Data) {
		t.Fatalf("datagram data mismatch: want %q got %q", want.Data, got.Data)
	}
}
