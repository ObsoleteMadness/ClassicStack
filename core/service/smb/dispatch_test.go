package smb

import (
	"testing"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"

	protocol "github.com/ObsoleteMadness/ClassicStack/core/protocol/smb"
)

// smbReq builds an SMB1 request frame: a header for command cmd with the given
// flags2/TID/UID, then a WCT-prefixed word block and a BCC-prefixed byte area.
func smbReq(cmd uint8, flags2, tid, uid uint16, words []byte, bytes []byte) []byte {
	h := protocol.Header{Command: cmd, Flags2: flags2, TID: tid, UID: uid, MID: 1, PIDLow: 1}
	out := h.Encode(nil)
	if len(words)%2 != 0 {
		words = append(words, 0)
	}
	out = append(out, byte(len(words)/2)) // WordCount
	out = append(out, words...)
	out = append(out, byte(len(bytes)), byte(len(bytes)>>8)) // ByteCount
	out = append(out, bytes...)
	return out
}

// newDispatchService builds an SMB service with one memfs share named PUBLIC and
// a session to drive Dispatch against.
func newDispatchService(t *testing.T) (*Service, *smbSession) {
	t.Helper()
	sh := newTestShare(t) // share name "PUBLIC"
	svc := &Service{shares: []*Share{sh}}
	return svc, newSession()
}

// respHeader decodes the reply header and fails if the reply flag is unset.
func respHeader(t *testing.T, reply []byte) protocol.Header {
	t.Helper()
	h, err := protocol.DecodeHeader(reply)
	if err != nil {
		t.Fatalf("DecodeHeader(reply): %v", err)
	}
	if h.Flags&protocol.FlagReply == 0 {
		t.Fatalf("reply flag not set in response")
	}
	return h
}

// TestDispatch_Negotiate proves NEGOTIATE accepts NT LM 0.12 and returns the
// WCT=17 parameter block with the negotiated dialect index.
func TestDispatch_Negotiate(t *testing.T) {
	svc, sess := newDispatchService(t)

	// Dialect list: 0x02 + "NT LM 0.12\0".
	dialects := append([]byte{0x02}, []byte(protocol.DialectNTLM)...)
	dialects = append(dialects, 0)
	req := smbReq(protocol.CommandNegotiate, 0, 0, 0, nil, dialects)

	reply := svc.Dispatch(sess, req)
	if reply == nil {
		t.Fatal("Negotiate returned nil")
	}
	h := respHeader(t, reply)
	if h.Status != statusSuccess {
		t.Fatalf("Negotiate status = %#x, want success", h.Status)
	}
	wct := reply[protocol.HeaderLen]
	if wct != 17 {
		t.Fatalf("Negotiate WCT = %d, want 17", wct)
	}
	// DialectIndex (first word) must be 0 (only dialect offered).
	idx := bp.LE16(reply[protocol.HeaderLen+1 : protocol.HeaderLen+3])
	if idx != 0 {
		t.Fatalf("Negotiate DialectIndex = %d, want 0", idx)
	}
}

// TestDispatch_SessionSetupGrantsGuestUID proves SESSION_SETUP_ANDX grants a
// non-zero guest UID stamped into the response header.
func TestDispatch_SessionSetupGrantsGuestUID(t *testing.T) {
	svc, sess := newDispatchService(t)
	// SESSION_SETUP_ANDX with an empty-ish word block (the handler does not parse
	// it in the guest path) — WCT large enough to look real is unnecessary here.
	req := smbReq(protocol.CommandSessionSetupAndX, protocol.Flags2NTStatus, 0, 0, make([]byte, 26), nil)

	reply := svc.Dispatch(sess, req)
	h := respHeader(t, reply)
	if h.UID == 0 {
		t.Fatal("SessionSetup granted UID 0, want a guest UID")
	}
	if sess.uid != h.UID {
		t.Fatalf("session uid %d != response uid %d", sess.uid, h.UID)
	}
}

// TestDispatch_TreeConnectBindsShare proves TREE_CONNECT_ANDX to \\server\PUBLIC
// binds a TID to the share, and an unknown share is refused with
// STATUS_BAD_NETWORK_NAME.
func TestDispatch_TreeConnectBindsShare(t *testing.T) {
	svc, sess := newDispatchService(t)
	flags2 := protocol.Flags2NTStatus

	// TREE_CONNECT_ANDX word block: AndXCommand(1) AndXReserved(1) AndXOffset(2)
	// Flags(2) PasswordLength(2) — 4 words. Password length 1 (a single NUL).
	words := make([]byte, 8)
	words[0] = protocol.CommandNoAndXCommand
	bp.PutLE16(words[6:8], 1) // PasswordLength = 1
	// Byte area: password(1 NUL) + "\\SERVER\PUBLIC\0" + service "?????\0".
	area := []byte{0x00}
	area = append(area, []byte("\\\\SERVER\\PUBLIC")...)
	area = append(area, 0)
	area = append(area, []byte("?????")...)
	area = append(area, 0)
	req := smbReq(protocol.CommandTreeConnectAndX, flags2, 0, 1, words, area)

	reply := svc.Dispatch(sess, req)
	h := respHeader(t, reply)
	if h.Status != statusSuccess {
		t.Fatalf("TreeConnect status = %#x, want success", h.Status)
	}
	if h.TID == 0 {
		t.Fatal("TreeConnect granted TID 0")
	}
	tc, ok := sess.tree(h.TID)
	if !ok || tc.share == nil || tc.share.Name() != "PUBLIC" {
		t.Fatalf("TID %d not bound to PUBLIC (tc=%+v ok=%v)", h.TID, tc, ok)
	}

	// Unknown share → STATUS_BAD_NETWORK_NAME.
	area2 := []byte{0x00}
	area2 = append(area2, []byte("\\\\SERVER\\NOPE")...)
	area2 = append(area2, 0)
	area2 = append(area2, []byte("?????")...)
	area2 = append(area2, 0)
	req2 := smbReq(protocol.CommandTreeConnectAndX, flags2, 0, 1, words, area2)
	reply2 := svc.Dispatch(sess, req2)
	h2 := respHeader(t, reply2)
	if h2.Status != statusBadNetworkName {
		t.Fatalf("unknown-share status = %#x, want STATUS_BAD_NETWORK_NAME", h2.Status)
	}
}

// TestDispatch_TreeDisconnectReleasesTID proves TREE_DISCONNECT drops the bound
// TID so a later lookup misses.
func TestDispatch_TreeDisconnectReleasesTID(t *testing.T) {
	svc, sess := newDispatchService(t)
	tid := sess.allocTID(&treeConnect{share: svc.shares[0]})

	req := smbReq(protocol.CommandTreeDisconnect, protocol.Flags2NTStatus, tid, 1, nil, nil)
	reply := svc.Dispatch(sess, req)
	h := respHeader(t, reply)
	if h.Status != statusSuccess {
		t.Fatalf("TreeDisconnect status = %#x, want success", h.Status)
	}
	if _, ok := sess.tree(tid); ok {
		t.Fatalf("TID %d still bound after disconnect", tid)
	}
}

// TestDispatch_FilesystemCommandNotSupported proves a recognised-but-unimplemented
// command (LOCKING_ANDX — the byte-range lock path is deliberately out of M7
// scope) is answered with STATUS_NOT_SUPPORTED rather than dropped or panicked.
func TestDispatch_FilesystemCommandNotSupported(t *testing.T) {
	svc, sess := newDispatchService(t)
	req := smbReq(protocol.CommandLockingAndX, protocol.Flags2NTStatus, 1, 1, make([]byte, 16), nil)
	reply := svc.Dispatch(sess, req)
	h := respHeader(t, reply)
	if h.Status != statusNotSupported {
		t.Fatalf("LockingAndX status = %#x, want STATUS_NOT_SUPPORTED", h.Status)
	}
}

// TestDispatch_NonSMBDropped proves a frame without the \xffSMB magic is dropped
// (nil) rather than mis-decoded.
func TestDispatch_NonSMBDropped(t *testing.T) {
	svc, sess := newDispatchService(t)
	if reply := svc.Dispatch(sess, []byte("not an smb frame at all............")); reply != nil {
		t.Fatalf("non-SMB frame produced a reply: %x", reply)
	}
}
