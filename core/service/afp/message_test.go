package afp

// message_test.go covers the server-message surface: FPGetSrvrMsg content
// (login greeting / pending operator message, MacRoman + length cap), the
// SendMessage / Disconnect operator actions with their attention + CloseSession
// wire sequences, the Sessions snapshot, and Stop's announce-then-close flow.
// The reply layout and attention words are held to the values an observed
// AppleShare server produces.

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/asp"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/atp"
)

// shortGrace shrinks the message-fetch grace for the duration of a test so the
// disconnect/stop sequences complete quickly.
func shortGrace(t *testing.T) {
	t.Helper()
	old := messageFetchGrace
	messageFetchGrace = 10 * time.Millisecond
	t.Cleanup(func() { messageFetchGrace = old })
}

// getSrvrMsg drives one FPGetSrvrMsg through the dispatch spine and returns the
// reply payload.
func getSrvrMsg(t *testing.T, svc *Service, r *fakeRouter, sessID uint8, msgType uint16) []byte {
	t.Helper()
	r.reset()
	block := []byte{cmdGetSrvrMsg, 0}
	block = bp.AppendBE16(block, msgType)
	block = bp.AppendBE16(block, 0x0001)
	svc.Inbound(ddpTo(svc.Socket(), atpTReq(aspUserData(asp.SPFuncCommand, sessID, 9), block)), fakePort{})
	if got := int32(respUserData(r.lastReply())); got != afpNoErr {
		t.Fatalf("FPGetSrvrMsg result = %d, want 0", got)
	}
	return respPayload(r.lastReply())
}

// aspSends decodes the server-initiated TReq frames the service routed to the
// workstation as (SPFunction, sessionID, low-16 user bytes) triples.
type aspSend struct {
	fn   uint8
	sess uint8
	word uint16
}

func (f *fakeRouter) aspSends() []aspSend {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]aspSend, 0, len(f.routed))
	for _, d := range f.routed {
		h, err := atp.Decode(d.Data)
		if err != nil || h.Control&0xC0 != atp.TREQ {
			continue
		}
		out = append(out, aspSend{
			fn:   uint8(h.UserData >> 24),
			sess: uint8(h.UserData >> 16),
			word: uint16(h.UserData),
		})
	}
	return out
}

// waitFor polls until cond is true or the deadline passes.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// TestFPGetSrvrMsg_LoginMessage pins the login-message reply to the observed
// layout: type(2)=0 bitmap(2)=0x0001 pstring(greeting).
func TestFPGetSrvrMsg_LoginMessage(t *testing.T) {
	svc, r := newRunningService(t)
	svc.SetLoginMessage("Welcome")
	sessID := login(t, svc, r)

	got := getSrvrMsg(t, svc, r, sessID, srvrMsgTypeLogin)
	want := []byte{0x00, 0x00, 0x00, 0x01, 0x07}
	want = append(want, "Welcome"...)
	if !bytes.Equal(got, want) {
		t.Fatalf("login-message reply:\n got:  %x\n want: %x", got, want)
	}
}

// TestFPGetSrvrMsg_NoMessageIsEmpty keeps the pre-message behaviour: with
// nothing configured/pending, both types answer an empty pstring.
func TestFPGetSrvrMsg_NoMessageIsEmpty(t *testing.T) {
	svc, r := newRunningService(t)
	sessID := login(t, svc, r)

	for _, msgType := range []uint16{srvrMsgTypeLogin, srvrMsgTypeServer} {
		got := getSrvrMsg(t, svc, r, sessID, msgType)
		want := bp.AppendBE16(nil, msgType)
		want = append(want, 0x00, 0x01, 0x00)
		if !bytes.Equal(got, want) {
			t.Fatalf("type-%d empty reply:\n got:  %x\n want: %x", msgType, got, want)
		}
	}
}

// TestFPGetSrvrMsg_MacRomanAndCap proves the message text is MacRoman on the
// wire (™ → 0xAA) and capped at the AFP 199-byte limit.
func TestFPGetSrvrMsg_MacRomanAndCap(t *testing.T) {
	svc, r := newRunningService(t)
	svc.SetLoginMessage("tm™")
	sessID := login(t, svc, r)

	got := getSrvrMsg(t, svc, r, sessID, srvrMsgTypeLogin)
	if want := []byte{0x00, 0x00, 0x00, 0x01, 0x03, 't', 'm', 0xAA}; !bytes.Equal(got, want) {
		t.Fatalf("MacRoman reply:\n got:  %x\n want: %x", got, want)
	}

	svc.SetLoginMessage(strings.Repeat("x", 300))
	got = getSrvrMsg(t, svc, r, sessID, srvrMsgTypeLogin)
	if got[4] != maxSrvrMsgLen || len(got) != 5+maxSrvrMsgLen {
		t.Fatalf("cap: length byte = %d payload = %d, want %d", got[4], len(got)-5, maxSrvrMsgLen)
	}
}

// TestSendMessage_AttentionThenFetch drives the operator flow end-to-end: the
// service sends the AspAttnMsg attention and the client's FPGetSrvrMsg type 1
// then returns the text.
func TestSendMessage_AttentionThenFetch(t *testing.T) {
	svc, r := newRunningService(t)
	sessID := login(t, svc, r)

	r.reset()
	if err := svc.SendMessage(0, "hello there"); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	sends := r.aspSends()
	if len(sends) != 1 || sends[0].fn != asp.SPFuncAttention || sends[0].sess != sessID || sends[0].word != asp.AspAttnMsg {
		t.Fatalf("SendMessage attention = %+v, want fn=%d sess=%d word=%#04x", sends, asp.SPFuncAttention, sessID, asp.AspAttnMsg)
	}

	got := getSrvrMsg(t, svc, r, sessID, srvrMsgTypeServer)
	want := []byte{0x00, 0x01, 0x00, 0x01, byte(len("hello there"))}
	want = append(want, "hello there"...)
	if !bytes.Equal(got, want) {
		t.Fatalf("server-message reply:\n got:  %x\n want: %x", got, want)
	}
}

// TestSendMessage_UnknownSession rejects an id with no live session.
func TestSendMessage_UnknownSession(t *testing.T) {
	svc, r := newRunningService(t)
	login(t, svc, r)
	if err := svc.SendMessage(99, "x"); !errors.Is(err, ErrNoSuchSession) {
		t.Fatalf("SendMessage(99) = %v, want ErrNoSuchSession", err)
	}
}

// TestSessions_SnapshotsIdentity proves the management snapshot carries the
// session id, client address, and login identity.
func TestSessions_SnapshotsIdentity(t *testing.T) {
	svc, r := newRunningService(t)
	sessID := login(t, svc, r)

	got := svc.Sessions()
	if len(got) != 1 {
		t.Fatalf("Sessions len = %d, want 1", len(got))
	}
	s := got[0]
	if s.ID != sessID || s.Network != 1 || s.Node != 10 || !s.LoggedIn || s.User != "" {
		t.Fatalf("Sessions[0] = %+v, want id=%d net=1 node=10 loggedIn guest", s, sessID)
	}
	if s.LastSeen.IsZero() {
		t.Fatal("Sessions[0].LastSeen is zero")
	}
}

// TestDisconnect_ImmediateSequence pins the minutes=0 disconnect wire sequence
// an observed AppleShare server produces: one shutdown attention with the
// message+no-reconnect bits (time 0), then a server-initiated CloseSession, and
// the session is gone.
func TestDisconnect_ImmediateSequence(t *testing.T) {
	shortGrace(t)
	svc, r := newRunningService(t)
	sessID := login(t, svc, r)

	r.reset()
	if err := svc.Disconnect(sessID, "bye now", 0); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}

	waitFor(t, "session teardown", func() bool { return svc.sessions.Count() == 0 })
	sends := r.aspSends()
	if len(sends) != 2 {
		t.Fatalf("routed sends = %+v, want [attention, closeSession]", sends)
	}
	wantWord := asp.AspAttnServerGoingDown | asp.AspAttnNoReconnect | asp.AspAttnMsg
	if sends[0].fn != asp.SPFuncAttention || sends[0].word != wantWord {
		t.Fatalf("attention = %+v, want fn=%d word=%#04x", sends[0], asp.SPFuncAttention, wantWord)
	}
	if sends[1].fn != asp.SPFuncCloseSess || sends[1].sess != sessID {
		t.Fatalf("close = %+v, want fn=%d sess=%d", sends[1], asp.SPFuncCloseSess, sessID)
	}
}

// TestStop_AnnouncesThenClosesSessions proves Stop's client-notice flow: the
// shutdown+message attention goes out, the pending message is set, and every
// session is ended with a server-initiated CloseSession before teardown.
func TestStop_AnnouncesThenClosesSessions(t *testing.T) {
	shortGrace(t)
	svc, r := newRunningService(t)
	sessID := login(t, svc, r)

	// The session's conn must carry the shutdown text for a client that fetches
	// during the grace window.
	sess, ok := svc.sessions.get(sessID)
	if !ok {
		t.Fatal("session not live before Stop")
	}

	r.reset()
	if err := svc.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if got := sess.conn.afp.serverMessage(); got != defaultShutdownMessage {
		t.Fatalf("shutdown message = %q, want %q", got, defaultShutdownMessage)
	}
	sends := r.aspSends()
	if len(sends) != 2 {
		t.Fatalf("routed sends = %+v, want [attention, closeSession]", sends)
	}
	if wantWord := asp.AspAttnServerGoingDown | asp.AspAttnMsg; sends[0].fn != asp.SPFuncAttention || sends[0].word != wantWord {
		t.Fatalf("attention = %+v, want fn=%d word=%#04x", sends[0], asp.SPFuncAttention, wantWord)
	}
	if sends[1].fn != asp.SPFuncCloseSess || sends[1].sess != sessID {
		t.Fatalf("close = %+v, want fn=%d sess=%d", sends[1], asp.SPFuncCloseSess, sessID)
	}
	if svc.sessions.Count() != 0 {
		t.Fatalf("sessions after Stop = %d, want 0", svc.sessions.Count())
	}
}

// TestServerInfo_AdvertisesSrvrMsg proves FPGetSrvrInfo always carries the
// SupportsSrvrMsg capability bit (clients ignore message attentions without it).
func TestServerInfo_AdvertisesSrvrMsg(t *testing.T) {
	svc, _ := newRunningService(t)
	block := svc.serverInfoBlock()
	flags := bp.BE16(block[8:10])
	if flags&srvrInfoSupportsSrvrMsg == 0 {
		t.Fatalf("FPGetSrvrInfo flags = %#04x, want SupportsSrvrMsg (%#04x) set", flags, srvrInfoSupportsSrvrMsg)
	}
}
