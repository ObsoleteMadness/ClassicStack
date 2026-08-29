package asp

import (
	"bytes"
	"testing"
)

func TestOpenSessReplyPacket_MarshalUserData(t *testing.T) {
	t.Parallel()
	p := OpenSessReplyPacket{SSSSocket: 0xAB, SessionID: 0xCD, ErrorCode: SPErrorBadVersNum}
	// SSSSocket=0xAB << 24 | SessionID=0xCD << 16 | uint16(-1066)=0xFBD6
	const want uint32 = 0xABCDFBD6
	if got := p.MarshalUserData(); got != want {
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

func TestWriteContinuePacket(t *testing.T) {
	t.Parallel()
	p := WriteContinuePacket{SessionID: 0x07, SeqNum: 0x1234, BufferSize: 0xABCD}

	const wantUserData uint32 = uint32(SPFuncWriteContinue)<<24 | 0x07<<16 | 0x1234
	if got := p.MarshalUserData(); got != wantUserData {
		t.Fatalf("MarshalUserData = %#08x, want %#08x", got, wantUserData)
	}
	if got := p.MarshalData(); !bytes.Equal(got, []byte{0xAB, 0xCD}) {
		t.Fatalf("MarshalData = % x, want ab cd", got)
	}
}

func TestTicklePacket_MarshalUserData(t *testing.T) {
	t.Parallel()
	p := TicklePacket{SessionID: 0x42}
	const want uint32 = uint32(SPFuncTickle)<<24 | 0x42<<16
	if got := p.MarshalUserData(); got != want {
		t.Fatalf("MarshalUserData = %#08x, want %#08x", got, want)
	}
}

func TestAttentionPacket_MarshalUserData(t *testing.T) {
	t.Parallel()
	p := AttentionPacket{SessionID: 0x09, AttentionCode: AspAttnServerGoingDown}
	const want uint32 = uint32(SPFuncAttention)<<24 | 0x09<<16 | uint32(AspAttnServerGoingDown)
	if got := p.MarshalUserData(); got != want {
		t.Fatalf("MarshalUserData = %#08x, want %#08x", got, want)
	}
}

// TestAttentionCodes_ObservedValues pins the composed attention words to the
// values an observed AppleShare server sends: 0x2000 announces a server
// message, 0xB001 a shutdown in 1 minute with a message, 0xB000 the same now.
func TestAttentionCodes_ObservedValues(t *testing.T) {
	t.Parallel()
	if AspAttnMsg != 0x2000 {
		t.Fatalf("AspAttnMsg = %#04x, want 0x2000", AspAttnMsg)
	}
	warn := AspAttnServerGoingDown | AspAttnMsg | AspAttnNoReconnect | AspAttnTime(1)
	if warn != 0xB001 {
		t.Fatalf("shutdown-in-1-minute word = %#04x, want 0xB001", warn)
	}
	now := AspAttnServerGoingDown | AspAttnMsg | AspAttnNoReconnect | AspAttnTime(0)
	if now != 0xB000 {
		t.Fatalf("shutdown-now word = %#04x, want 0xB000", now)
	}
}

// TestAspAttnTime_Clamps pins the countdown clamp to the 12-bit time field.
func TestAspAttnTime_Clamps(t *testing.T) {
	t.Parallel()
	if got := AspAttnTime(-3); got != 0 {
		t.Fatalf("AspAttnTime(-3) = %#04x, want 0", got)
	}
	if got := AspAttnTime(0x5000); got != AspAttnTimeMask {
		t.Fatalf("AspAttnTime(0x5000) = %#04x, want %#04x", got, AspAttnTimeMask)
	}
}

// TestCloseSessPacket_MarshalUserData pins the server-initiated CloseSession
// TReq user bytes: [0]=SPFuncCloseSess [1]=SessionID [2:3]=0 (observed capture).
func TestCloseSessPacket_MarshalUserData(t *testing.T) {
	t.Parallel()
	p := CloseSessPacket{SessionID: 0x02}
	const want uint32 = uint32(SPFuncCloseSess)<<24 | 0x02<<16
	if got := p.MarshalUserData(); got != want {
		t.Fatalf("MarshalUserData = %#08x, want %#08x", got, want)
	}
}
