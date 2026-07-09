package runtime

// integration_test.go is the M-ng3 end-to-end harness: it drives a REAL port over
// an in-memory FrameLink pair through the REAL cross-wire (mini-router + NetBIOS
// session engine + SMB), writes a client frame into the peer end of the link, and
// asserts the reply that comes back out — with NO test doubles between the wire and
// the command core. Where transports_test.go injects frames straight into the
// delivery callback (a wiring unit test), this exercises the whole path including
// the frameport read loop, the port's own decode/encode, and the link.
//
// The inmem link (adapter/link/inmem) is the Phase-1 D4 loopback: a frame written
// to one end is readable from the other. The port opens one end (its LinkFactory),
// the test holds the peer end and plays the client.

import (
	"context"
	"testing"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/adapter/link/inmem"
	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/link"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/port"
	portipx "github.com/ObsoleteMadness/ClassicStack/core/port/ipx"
	portnetbeui "github.com/ObsoleteMadness/ClassicStack/core/port/netbeui"
	ipxproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/ipx"
	nbf "github.com/ObsoleteMadness/ClassicStack/core/protocol/netbeui"
	nbproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/netbios"
	smbproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/smb"
	"github.com/ObsoleteMadness/ClassicStack/core/service/netbios"
	"github.com/ObsoleteMadness/ClassicStack/core/service/smb"
)

const (
	// ethHdrLen + the 802.2 LLC UI header the NetBEUI port frames NBF bodies in.
	testEthHdrLen = 14
)

// testClientMAC / testServerMAC are the Ethernet endpoints the integration frames
// use; the server MAC is the port's station address.
var (
	testClientMAC = [6]byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x01}
	testServerMAC = [6]byte{0x02, 0x00, 0x00, 0x00, 0x00, 0xFF}
)

// llcUIHeader is the 802.2 LLC UI header for NetBIOS frames (DSAP=SSAP=0xF0,
// control=0x03), matching what core/port/netbeui requires on inbound.
var llcUIHeader = [3]byte{0xF0, 0xF0, 0x03}

// encodeNBFFrame wraps an NBF frame in the 802.3 + LLC UI envelope the NetBEUI port
// decodes on inbound (the inverse of portnetbeui.Port.Send): dst/src MAC, 802.3
// length, LLC UI header, then the encoded NBF body.
func encodeNBFFrame(t *testing.T, dst, src [6]byte, f *nbf.Frame) link.Frame {
	t.Helper()
	body, err := f.Encode()
	if err != nil {
		t.Fatalf("encode NBF frame: %v", err)
	}
	payloadLen := len(llcUIHeader) + len(body)
	out := make([]byte, 0, testEthHdrLen+payloadLen)
	out = append(out, dst[:]...)
	out = append(out, src[:]...)
	out = append(out, byte(payloadLen>>8), byte(payloadLen))
	out = append(out, llcUIHeader[:]...)
	out = append(out, body...)
	return out
}

// decodeNBFFrame is the inverse: strip the Ethernet + LLC UI header and decode the
// NBF body. Returns nil when the frame is not a NetBIOS UI frame.
func decodeNBFFrame(t *testing.T, frame link.Frame) *nbf.Frame {
	t.Helper()
	if len(frame) < testEthHdrLen+3 {
		return nil
	}
	body := frame[testEthHdrLen:]
	if body[0] != llcUIHeader[0] || body[1] != llcUIHeader[1] || body[2] != llcUIHeader[2] {
		return nil
	}
	decoded, err := nbf.Decode(body[3:])
	if err != nil {
		t.Fatalf("decode NBF body: %v", err)
	}
	return decoded
}

// TestIntegration_NetBEUICallOverInmemLink drives a NetBIOS session CALL end-to-end:
// a real NetBEUI port over an inmem link, cross-wired to a real NetBIOS+SMB stack,
// answers a NAME_QUERY (CALL) for its file-server name with NAME_RECOGNIZED — the
// frame travelling client → peer link → port read loop → mini-router → NBF engine →
// port.Send → peer link → client, with no doubles in the path.
func TestIntegration_NetBEUICallOverInmemLink(t *testing.T) {
	// inmem pair: portEnd is opened by the port's LinkFactory; clientEnd is the
	// test's wire. Buffer 4 so a reply can be queued without a reader racing.
	portEnd, clientEnd := inmem.Pair(4)

	sec := &port.Section{SKey: portnetbeui.Name, Name: "nb0", IsEnabled: true}
	logger := log.New("nb0", log.NewStderrSink(log.NewLevelVar(log.Warn)))
	open := func() (link.FrameLink, error) { return portEnd, nil }
	comp, err := portnetbeui.NewInstanceFromOpener(sec, open, testServerMAC, logger)
	if err != nil {
		t.Fatalf("build NetBEUI port: %v", err)
	}
	if comp == nil {
		t.Fatal("NetBEUI port built nil for an enabled section")
	}

	nb := netbios.NewService(nil, "CLASSICSTACK")
	sm := smb.New(nil)
	comps := map[string]component.Component{
		netbios.Name:     nb,
		smb.Name:         sm,
		portnetbeui.Name: comp,
	}

	// The real cross-wire: builds the NetBEUI mini-router, attaches the port, and
	// registers the NBF engine for CLASSICSTACK.
	crossWireTransports(comps, nil)

	ctx := context.Background()
	if err := comp.Start(ctx); err != nil {
		t.Fatalf("start port: %v", err)
	}
	defer comp.Stop(ctx)

	// Play the client: a NAME_QUERY (CALL) for our file-server name.
	name := nbproto.NewName("CLASSICSTACK", nbproto.NameTypeFileServer)
	clientName := nbproto.NewName("CLIENT", nbproto.NameTypeWorkstation)
	call := &nbf.Frame{Command: nbf.CmdNameQuery, Data2: 5, RspCorrelator: 0x1234}
	copy(call.DestinationName[:], name[:])
	copy(call.SourceName[:], clientName[:])

	if err := clientEnd.Write(encodeNBFFrame(t, nbf.NetBIOSMulticastMAC, testClientMAC, call)); err != nil {
		t.Fatalf("client write CALL: %v", err)
	}

	// Read the reply off the wire and assert it is NAME_RECOGNIZED. Read in a
	// goroutine with a timeout so a wiring failure fails fast rather than hanging.
	got := readNBFWithTimeout(t, clientEnd, 2*time.Second)
	if got == nil {
		t.Fatal("no reply frame received for CALL within timeout")
	}
	if got.Command != nbf.CmdNameRecognized {
		t.Fatalf("reply command = 0x%02X, want NAME_RECOGNIZED (0x%02X)", got.Command, nbf.CmdNameRecognized)
	}
}

// smbEtherType is the Ethernet II type for IPX (0x8137) the IPX port frames in.
const smbEtherType = 0x8137

// buildNegotiate builds a minimal SMB1 NEGOTIATE request offering NT LM 0.12, using
// the exported core/protocol/smb header encoder + the wire layout (WCT, words, BCC,
// bytes) — the same frame core/service/smb's own dispatch test sends, rebuilt here
// from exported types so the integration test owns no smb-internal helper.
func buildNegotiate() []byte {
	h := smbproto.Header{Command: smbproto.CommandNegotiate, MID: 1, PIDLow: 1}
	out := h.Encode(nil)
	out = append(out, 0) // WordCount = 0 (no parameter words)
	dialects := append([]byte{0x02}, []byte(smbproto.DialectNTLM)...)
	dialects = append(dialects, 0)
	out = append(out, byte(len(dialects)), byte(len(dialects)>>8)) // ByteCount (LE)
	out = append(out, dialects...)
	return out
}

// encodeIPXFrame wraps an IPX datagram in an Ethernet II (0x8137) frame the IPX port
// decodes on inbound (the inverse of portipx.Port.Send).
func encodeIPXFrame(t *testing.T, dst, src [6]byte, d *ipxproto.Datagram) link.Frame {
	t.Helper()
	ipxBytes, err := d.Encode(nil)
	if err != nil {
		t.Fatalf("encode IPX datagram: %v", err)
	}
	out := make([]byte, 0, testEthHdrLen+len(ipxBytes))
	out = append(out, dst[:]...)
	out = append(out, src[:]...)
	out = append(out, byte(smbEtherType>>8), byte(smbEtherType&0xFF))
	out = append(out, ipxBytes...)
	return out
}

// decodeIPXFrame strips the Ethernet II header and decodes the IPX datagram, or nil
// when the frame is not Ethernet II IPX.
func decodeIPXFrame(t *testing.T, frame link.Frame) *ipxproto.Datagram {
	t.Helper()
	if len(frame) < testEthHdrLen {
		return nil
	}
	if uint16(frame[12])<<8|uint16(frame[13]) != smbEtherType {
		return nil
	}
	d, err := ipxproto.Decode(frame[testEthHdrLen:])
	if err != nil {
		t.Fatalf("decode IPX datagram: %v", err)
	}
	return d
}

// TestIntegration_DirectIPXNegotiateOverInmemLink drives an SMB direct-hosted-over-
// IPX (NWLink direct hosting, socket 0x0550, NetBIOS-LESS) NEGOTIATE end-to-end: a
// real IPX port over an inmem link, cross-wired to a real SMB stack with no NetBIOS,
// answers a NEGOTIATE with an SMB reply (FlagReply set). The frame travels client →
// peer link → IPX port read loop → IPX mini-router → direct-IPX transport → SMB
// command core → reply → port.Send → peer link → client, no doubles in the path.
func TestIntegration_DirectIPXNegotiateOverInmemLink(t *testing.T) {
	portEnd, clientEnd := inmem.Pair(4)

	sec := &port.Section{SKey: portipx.Name, Name: "ipx0", IsEnabled: true}
	logger := log.New("ipx0", log.NewStderrSink(log.NewLevelVar(log.Warn)))
	open := func() (link.FrameLink, error) { return portEnd, nil }
	comp, err := portipx.NewInstanceFromOpener(sec, open, testServerMAC, logger)
	if err != nil {
		t.Fatalf("build IPX port: %v", err)
	}
	if comp == nil {
		t.Fatal("IPX port built nil for an enabled section")
	}

	// SMB only — NO NetBIOS, so the IPX mini-router carries direct-IPX (0x0550) alone.
	sm := smb.New(nil)
	comps := map[string]component.Component{
		smb.Name:     sm,
		portipx.Name: comp,
	}
	crossWireTransports(comps, nil)

	ctx := context.Background()
	if err := comp.Start(ctx); err != nil {
		t.Fatalf("start port: %v", err)
	}
	defer comp.Stop(ctx)

	// Address the NEGOTIATE to the IPX router (effective identity: network 0, node testServerMAC)
	// on the direct-SMB socket, IPX packet type 4 (PEP).
	dg := &ipxproto.Datagram{
		Type:    0x04,
		DstNode: testServerMAC,
		DstSock: smb.DirectSMBSocket,
		SrcNode: testClientMAC,
		SrcSock: [2]byte{0x40, 0x00},
		Payload: buildNegotiate(),
	}
	if err := clientEnd.Write(encodeIPXFrame(t, testServerMAC, testClientMAC, dg)); err != nil {
		t.Fatalf("client write NEGOTIATE: %v", err)
	}

	reply := readIPXWithTimeout(t, clientEnd, 2*time.Second)
	if reply == nil {
		t.Fatal("no reply datagram received for NEGOTIATE within timeout")
	}
	h, err := smbproto.DecodeHeader(reply.Payload)
	if err != nil {
		t.Fatalf("decode SMB reply header: %v", err)
	}
	if h.Flags&smbproto.FlagReply == 0 {
		t.Fatal("SMB reply flag not set — NEGOTIATE not answered by the command core")
	}
	if h.Command != smbproto.CommandNegotiate {
		t.Fatalf("reply command = 0x%02X, want NEGOTIATE (0x%02X)", h.Command, smbproto.CommandNegotiate)
	}
}

// readIPXWithTimeout reads frames off end until one decodes to an IPX datagram or
// the timeout elapses, skipping non-IPX frames.
func readIPXWithTimeout(t *testing.T, end *inmem.Link, timeout time.Duration) *ipxproto.Datagram {
	t.Helper()
	ch := make(chan *ipxproto.Datagram, 1)
	go func() {
		for {
			frame, err := end.Read()
			if err != nil {
				ch <- nil
				return
			}
			if d := decodeIPXFrame(t, frame); d != nil {
				ch <- d
				return
			}
		}
	}()
	select {
	case d := <-ch:
		return d
	case <-time.After(timeout):
		return nil
	}
}

// readNBFWithTimeout reads frames off end until one decodes to an NBF frame or the
// timeout elapses, returning the decoded frame (or nil on timeout). Non-NBF frames
// are skipped.
func readNBFWithTimeout(t *testing.T, end *inmem.Link, timeout time.Duration) *nbf.Frame {
	t.Helper()
	type result struct{ f *nbf.Frame }
	ch := make(chan result, 1)
	go func() {
		for {
			frame, err := end.Read()
			if err != nil {
				ch <- result{nil}
				return
			}
			if f := decodeNBFFrame(t, frame); f != nil {
				ch <- result{f}
				return
			}
		}
	}()
	select {
	case r := <-ch:
		return r.f
	case <-time.After(timeout):
		return nil
	}
}
