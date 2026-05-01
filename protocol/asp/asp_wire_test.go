package asp

import (
	"bytes"
	"testing"
)

func TestOpenSessReplyPacket_MarshalUserData(t *testing.T) {
	t.Parallel()
	p := OpenSessReplyPacket{SSSSocket: 0xAB, SessionID: 0xCD, ErrorCode: SPErrorBadVersNum}
	got := p.MarshalUserData()
	// SSSSocket=0xAB << 24 | SessionID=0xCD << 16 | uint16(-1066)=0xFBD6
	const want uint32 = 0xABCDFBD6
	if got != want {
		t.Fatalf("MarshalUserData = %#08x, want %#08x", got, want)
	}
}

func TestParseOpenSessPacket(t *testing.T) {
	t.Parallel()
	got := ParseOpenSessPacket(0xAA112233)
	if got.WSSSocket != 0x11 || got.VersionNum != 0x2233 {
		t.Fatalf("ParseOpenSessPacket = %+v, want WSSSocket=0x11 VersionNum=0x2233", got)
	}
}

func TestParseCommandPacket(t *testing.T) {
	t.Parallel()
	payload := []byte{1, 2, 3}
	got := ParseCommandPacket(0xAA071234, payload)
	if got.SessionID != 0x07 || got.SeqNum != 0x1234 || !bytes.Equal(got.CmdBlock, payload) {
		t.Fatalf("ParseCommandPacket = %+v, want SessionID=7 SeqNum=0x1234 CmdBlock=%v", got, payload)
	}
}

func TestWriteContinuePacket_WireRoundTrip(t *testing.T) {
	t.Parallel()
	p := WriteContinuePacket{SessionID: 0x07, SeqNum: 0x1234, BufferSize: 0xABCD}

	const wantUserData uint32 = uint32(SPFuncWriteContinue)<<24 | 0x07<<16 | 0x1234
	if got := p.MarshalUserData(); got != wantUserData {
		t.Fatalf("MarshalUserData = %#08x, want %#08x", got, wantUserData)
	}

	if p.WireSize() != 2 {
		t.Fatalf("WireSize = %d, want 2", p.WireSize())
	}

	buf := make([]byte, p.WireSize())
	n, err := p.MarshalWire(buf)
	if err != nil {
		t.Fatalf("MarshalWire: %v", err)
	}
	if n != 2 || !bytes.Equal(buf, []byte{0xAB, 0xCD}) {
		t.Fatalf("MarshalWire buf = % x (n=%d), want ab cd", buf, n)
	}

	var out WriteContinuePacket
	if _, err := out.UnmarshalWire(buf); err != nil {
		t.Fatalf("UnmarshalWire: %v", err)
	}
	if out.BufferSize != p.BufferSize {
		t.Fatalf("round-trip BufferSize = %#x, want %#x", out.BufferSize, p.BufferSize)
	}
}

func TestTicklePacket_MarshalUserData(t *testing.T) {
	t.Parallel()
	p := TicklePacket{SessionID: 0x42}
	got := p.MarshalUserData()
	const want uint32 = uint32(SPFuncTickle)<<24 | 0x42<<16
	if got != want {
		t.Fatalf("MarshalUserData = %#08x, want %#08x", got, want)
	}
}

func TestAttentionPacket_MarshalUserData(t *testing.T) {
	t.Parallel()
	p := AttentionPacket{SessionID: 0x09, AttentionCode: AspAttnServerGoingDown}
	got := p.MarshalUserData()
	const want uint32 = uint32(SPFuncAttention)<<24 | 0x09<<16 | uint32(AspAttnServerGoingDown)
	if got != want {
		t.Fatalf("MarshalUserData = %#08x, want %#08x", got, want)
	}
}
