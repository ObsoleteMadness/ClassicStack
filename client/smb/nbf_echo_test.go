package smb

import (
	"testing"

	nbfproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/netbeui"
)

// nbfIFrame builds a same-MAC LLC2 I-frame (the self-talk pcap case) carrying nbfBody.
func nbfIFrame(mac [6]byte, nS, nR uint8, poll bool, nbfBody []byte) []byte {
	const llcLen = 4
	payloadLen := llcLen + len(nbfBody)
	out := make([]byte, nbfEthHdrLen+payloadLen)
	copy(out[0:6], mac[:])
	copy(out[6:12], mac[:])
	out[12], out[13] = byte(payloadLen>>8), byte(payloadLen)
	out[14], out[15] = nbfLLCDSAP, nbfLLCSSAPCmd
	out[16] = nS << 1
	out[17] = nR << 1
	if poll {
		out[17] |= 0x01
	}
	copy(out[18:], nbfBody)
	return nbfPad(out)
}

func encodeNBF(t *testing.T, f *nbfproto.Frame) []byte {
	t.Helper()
	body, err := f.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	return body
}

// TestNBFOwnInitializeEchoDoesNotAdvanceNR reproduces the self-MAC pcap failure:
// the client's SESSION_INITIALIZE I-frame is read back (src==dst), and consuming it
// would advance N(R) so the server's SESSION_CONFIRM (also N(S)=0) is discarded.
func TestNBFOwnInitializeEchoDoesNotAdvanceNR(t *testing.T) {
	l := newCaptureLink()
	tr := &nbfTransport{
		fl:         l,
		srcMAC:     testMAC,
		serverMAC:  testMAC,
		haveServer: true,
		localNum:   nbfClientSessionNum,
		remoteNum:  1,
		confirmCh:  make(chan struct{}, 1),
		stop:       make(chan struct{}),
	}

	initBody := encodeNBF(t, &nbfproto.Frame{
		Command:      nbfproto.CmdSessionInitialize,
		DestNumber:   1,
		SourceNumber: nbfClientSessionNum,
	})
	tr.handleFrame(nbfIFrame(testMAC, 0, 0, true, initBody))
	if tr.nR != 0 {
		t.Fatalf("nR = %d after own SESSION_INITIALIZE echo, want 0", tr.nR)
	}

	confirmBody := encodeNBF(t, &nbfproto.Frame{
		Command:      nbfproto.CmdSessionConfirm,
		DestNumber:   nbfClientSessionNum,
		SourceNumber: 1,
	})
	tr.handleFrame(nbfIFrame(testMAC, 0, 1, false, confirmBody))
	select {
	case <-tr.confirmCh:
	default:
		t.Fatal("SESSION_CONFIRM was not delivered (own Initialize echo consumed N(S)=0)")
	}
	if tr.nR != 1 {
		t.Fatalf("nR = %d after SESSION_CONFIRM, want 1", tr.nR)
	}
	l.Close()
}
