package smb

import (
	"testing"
	"time"

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
	return svc, newSession("")
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

// dialectListBytes builds a NEGOTIATE request dialect byte-area: for each name a
// 0x02 buffer-format byte followed by the NUL-terminated ASCII string.
func dialectListBytes(names ...string) []byte {
	var out []byte
	for _, n := range names {
		out = append(out, 0x02)
		out = append(out, []byte(n)...)
		out = append(out, 0)
	}
	return out
}

// TestDispatch_Negotiate proves NEGOTIATE selects NT LM 0.12 when offered and returns
// the NT-format WCT=17 parameter block with the negotiated dialect index.
func TestDispatch_Negotiate(t *testing.T) {
	svc, sess := newDispatchService(t)

	req := smbReq(protocol.CommandNegotiate, 0, 0, 0, nil, dialectListBytes(protocol.DialectNTLM))

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
	idx := bp.LE16(reply[protocol.HeaderLen+1 : protocol.HeaderLen+3])
	if idx != 0 {
		t.Fatalf("Negotiate DialectIndex = %d, want 0", idx)
	}
}

// TestNegotiate_WordCountMatchesDialectFamily proves the response WordCount matches the
// selected dialect family ([MS-CIFS] 2.2.4.52.2): Core → 1, LANMAN → 13, NT → 17, and
// the selected DialectIndex is the most-recent dialect the client offered.
func TestNegotiate_WordCountMatchesDialectFamily(t *testing.T) {
	cases := []struct {
		name    string
		offered []string
		wantWCT byte
		wantIdx uint16
	}{
		{"core only", []string{protocol.DialectPCNetwork1}, 1, 0},
		{"lanman WfW", []string{protocol.DialectPCNetwork1, protocol.DialectWfW311}, 13, 1},
		{"lanman DOS 2.1", []string{protocol.DialectPCNetwork1, protocol.DialectMSNet30, protocol.DialectDOSLANMAN2}, 13, 2},
		{"nt among many", []string{
			protocol.DialectPCNetwork1, protocol.DialectMSNet30, protocol.DialectDOSLM12,
			protocol.DialectDOSLANMAN2, protocol.DialectWfW311, protocol.DialectNTLM,
		}, 17, 5},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			svc, sess := newDispatchService(t)
			req := smbReq(protocol.CommandNegotiate, 0, 0, 0, nil, dialectListBytes(c.offered...))
			reply := svc.Dispatch(sess, req)
			h := respHeader(t, reply)
			if h.Status != statusSuccess {
				t.Fatalf("status = %#x, want success", h.Status)
			}
			if wct := reply[protocol.HeaderLen]; wct != c.wantWCT {
				t.Fatalf("WCT = %d, want %d (dialect family mismatch)", wct, c.wantWCT)
			}
			if idx := bp.LE16(reply[protocol.HeaderLen+1 : protocol.HeaderLen+3]); idx != c.wantIdx {
				t.Fatalf("DialectIndex = %d, want %d (most-recent selection)", idx, c.wantIdx)
			}
		})
	}
}

// TestNegotiate_NoSupportedDialect proves an unrecognised dialect list yields the core
// WCT=1 shape with DialectIndex 0xFFFF ([MS-CIFS] 2.2.4.52.2).
func TestNegotiate_NoSupportedDialect(t *testing.T) {
	svc, sess := newDispatchService(t)
	req := smbReq(protocol.CommandNegotiate, 0, 0, 0, nil, dialectListBytes("SOMETHING WEIRD", "ANOTHER"))
	reply := svc.Dispatch(sess, req)
	h := respHeader(t, reply)
	if h.Status != statusSuccess {
		t.Fatalf("status = %#x, want success", h.Status)
	}
	if wct := reply[protocol.HeaderLen]; wct != 1 {
		t.Fatalf("WCT = %d, want 1 (core shape for no match)", wct)
	}
	if idx := bp.LE16(reply[protocol.HeaderLen+1 : protocol.HeaderLen+3]); idx != 0xFFFF {
		t.Fatalf("DialectIndex = %#x, want 0xFFFF", idx)
	}
}

// TestNegotiate_PreservesRequestFlags2 proves the NEGOTIATE response echoes the
// request's Flags2 unchanged — it does NOT stamp SMB_FLAGS2_KNOWS_LONG_NAMES (0x0001)
// that the general responseHeader helper adds ([smb6.0]: same Mid/Pid; legacy copies
// the request header). Verified for both the LANMAN and NT response paths.
func TestNegotiate_PreservesRequestFlags2(t *testing.T) {
	for _, tc := range []struct {
		name    string
		offered []string
	}{
		{"lanman", []string{protocol.DialectPCNetwork1, protocol.DialectWfW311}},
		{"nt", []string{protocol.DialectNTLM}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, sess := newDispatchService(t)
			// Request Flags2 = 0x0000 (a CORE/DOS-error Win9x/WfW client).
			req := smbReq(protocol.CommandNegotiate, 0x0000, 0, 0, nil, dialectListBytes(tc.offered...))
			h := respHeader(t, svc.Dispatch(sess, req))
			if h.Flags2 != 0x0000 {
				t.Fatalf("response Flags2 = %#06x, want 0x0000 (must not add KNOWS_LONG_NAMES)", h.Flags2)
			}
		})
	}
}

// TestNegotiate_LanManFieldWidths proves the LANMAN WCT=13 response uses 16-bit
// SecurityMode and 16-bit MaxBufferSize (they are 8-bit / 32-bit in the NT form), and
// carries the DOS SMB_TIME/SMB_DATE + EncryptionKeyLength=0 + a NUL-terminated
// PrimaryDomain for a LANMAN2.1 dialect ([smb6.0] 1112-1127).
func TestNegotiate_LanManFieldWidths(t *testing.T) {
	svc, sess := newDispatchService(t)
	svc.SetWorkgroup("WORKGROUP")
	// DOS LANMAN2.1 is the one LANMAN-family dialect whose response includes the
	// PrimaryDomain ([smb6.0] 1127).
	req := smbReq(protocol.CommandNegotiate, 0, 0, 0, nil, dialectListBytes(protocol.DialectDOSLANMAN2))
	reply := svc.Dispatch(sess, req)

	w := reply[protocol.HeaderLen+1:] // word block starts after WCT byte
	if sm := bp.LE16(w[2:4]); sm != negotiateSecurityModeShare {
		t.Errorf("SecurityMode(16-bit) = %#x, want %#x (share-level, no store wired)", sm, negotiateSecurityModeShare)
	}
	if mb := bp.LE16(w[4:6]); mb != uint16(negotiateMaxBufferSize) {
		t.Errorf("MaxBufferSize(16-bit) = %d, want %d", mb, negotiateMaxBufferSize)
	}
	if kl := bp.LE16(w[22:24]); kl != 0 {
		t.Errorf("EncryptionKeyLength = %d, want 0", kl)
	}
	// ByteArea after WCT(1)+words(26)+BCC(2): the PrimaryDomain string.
	bccOff := protocol.HeaderLen + 1 + 26
	bcc := int(bp.LE16(reply[bccOff : bccOff+2]))
	area := reply[bccOff+2 : bccOff+2+bcc]
	if got := string(trimNul(area)); got != "WORKGROUP" {
		t.Errorf("PrimaryDomain = %q, want WORKGROUP", got)
	}
}

// TestNegotiate_LanManPrimaryDomainOnlyForLanMan21 proves the WCT=13 response includes
// the PrimaryDomain ONLY for DOS LANMAN2.1 / LANMAN2.1 dialects ([smb6.0] 1127). For an
// earlier LANMAN-family dialect (Windows for Workgroups 3.1a) the byte area MUST be
// empty (ByteCount=0) — appending WORKGROUP\0 there is trailing "Unknown Data" a client
// does not parse (captures/ipx.pcap frames 336-337, Win3.11 selecting WfW 3.1a).
func TestNegotiate_LanManPrimaryDomainOnlyForLanMan21(t *testing.T) {
	svc, sess := newDispatchService(t)
	svc.SetWorkgroup("WORKGROUP")

	bccOff := protocol.HeaderLen + 1 + 26 // WCT(1) + 13 words
	cases := []struct {
		name       string
		offered    []string
		wantDomain bool
	}{
		{"WfW 3.1a → no domain", []string{protocol.DialectPCNetwork1, protocol.DialectWfW311}, false},
		{"MSNET 3.0 → no domain", []string{protocol.DialectPCNetwork1, protocol.DialectMSNet30}, false},
		{"DOS LANMAN2.1 → domain", []string{protocol.DialectPCNetwork1, protocol.DialectDOSLANMAN2}, true},
		{"LANMAN2.1 → domain", []string{protocol.DialectPCNetwork1, protocol.DialectLANMAN21}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := smbReq(protocol.CommandNegotiate, 0, 0, 0, nil, dialectListBytes(c.offered...))
			reply := svc.Dispatch(sess, req)
			if wct := reply[protocol.HeaderLen]; wct != 13 {
				t.Fatalf("WordCount = %d, want 13", wct)
			}
			bcc := int(bp.LE16(reply[bccOff : bccOff+2]))
			if c.wantDomain {
				area := reply[bccOff+2 : bccOff+2+bcc]
				if got := string(trimNul(area)); got != "WORKGROUP" {
					t.Errorf("PrimaryDomain = %q, want WORKGROUP", got)
				}
			} else if bcc != 0 {
				t.Errorf("ByteCount = %d, want 0 (no PrimaryDomain for this dialect)", bcc)
			}
		})
	}
}

// TestNegotiate_NTFieldWidths proves the NT WCT=17 response uses an 8-bit SecurityMode
// and 32-bit MaxBufferSize and includes the Capabilities field ([smb6.0] NT LM 0.12).
func TestNegotiate_NTFieldWidths(t *testing.T) {
	svc, sess := newDispatchService(t)
	req := smbReq(protocol.CommandNegotiate, 0, 0, 0, nil, dialectListBytes(protocol.DialectNTLM))
	reply := svc.Dispatch(sess, req)

	w := reply[protocol.HeaderLen+1:]
	if sm := w[2]; sm != negotiateSecurityModeShare { // SecurityMode is 1 byte here
		t.Errorf("SecurityMode(8-bit) = %#x, want %#x (share-level, no store wired)", sm, negotiateSecurityModeShare)
	}
	if mb := bp.LE32(w[7:11]); mb != negotiateMaxBufferSize { // MaxBufferSize is 4 bytes here
		t.Errorf("MaxBufferSize(32-bit) = %d, want %d", mb, negotiateMaxBufferSize)
	}
	if caps := bp.LE32(w[19:23]); caps != negotiateCapabilities {
		t.Errorf("Capabilities = %#x, want %#x", caps, negotiateCapabilities)
	}
}

// TestNegotiate_SecurityModeFollowsUserStore proves the advertised SecurityMode
// ([MS-CIFS] 2.2.4.52.2 bit 0) is SHARE-level when no user store is wired and
// USER-level once one is. A user-level server that offers no challenge is
// unusable by NT-family redirectors — they refuse to send plaintext passwords
// and abort right after NEGOTIATE (netbeui.pcap frames 51–61, NT 3.51 `net
// view` → Session End + DISC → "access denied") — so a guest-only server must
// advertise share-level.
func TestNegotiate_SecurityModeFollowsUserStore(t *testing.T) {
	svc, sess := newDispatchService(t)
	req := smbReq(protocol.CommandNegotiate, 0, 0, 0, nil, dialectListBytes(protocol.DialectNTLM))

	reply := svc.Dispatch(sess, req)
	if sm := reply[protocol.HeaderLen+1+2]; sm != negotiateSecurityModeShare {
		t.Errorf("no store: SecurityMode = %#x, want %#x (share-level)", sm, negotiateSecurityModeShare)
	}

	// A wired store that reports NO named users (the compose root wires the
	// built-in store even when empty) stays share-level.
	svc.SetAuthenticator(emptyStoreAuth{})
	reply = svc.Dispatch(sess, req)
	if sm := reply[protocol.HeaderLen+1+2]; sm != negotiateSecurityModeShare {
		t.Errorf("empty store: SecurityMode = %#x, want %#x (share-level)", sm, negotiateSecurityModeShare)
	}

	// An authenticator that cannot report its user set is taken as user-level.
	svc.SetAuthenticator(fakeAuth{user: "alice", pass: "secret"})
	reply = svc.Dispatch(sess, req)
	if sm := reply[protocol.HeaderLen+1+2]; sm != negotiateSecurityModeUser {
		t.Errorf("store wired: SecurityMode = %#x, want %#x (user-level)", sm, negotiateSecurityModeUser)
	}
}

// emptyStoreAuth mimics the built-in user store with zero records: it can
// report HasUsers (false), so NEGOTIATE stays share-level.
type emptyStoreAuth struct{}

func (emptyStoreAuth) Authenticate(string, string) (bool, error) { return false, nil }
func (emptyStoreAuth) HasUsers() bool                            { return false }

// TestSMBServerTimeDate proves the SMB_TIME/SMB_DATE packer encodes a known timestamp
// into the DOS 16-bit fields the LANMAN NEGOTIATE response carries.
func TestSMBServerTimeDate(t *testing.T) {
	tm, dt := smbServerTimeDate(time.Date(2021, 7, 8, 14, 30, 52, 0, time.UTC))
	wantTime := uint16(26) | uint16(30)<<5 | uint16(14)<<11 // sec/2=26, min=30, hour=14
	wantDate := uint16(8) | uint16(7)<<5 | uint16(41)<<9    // day=8, month=7, year-1980=41
	if tm != wantTime || dt != wantDate {
		t.Fatalf("smbServerTimeDate = (%#x,%#x), want (%#x,%#x)", tm, dt, wantTime, wantDate)
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
// command (NT_CANCEL — no dispatch entry) is answered with STATUS_NOT_SUPPORTED
// rather than dropped or panicked.
func TestDispatch_FilesystemCommandNotSupported(t *testing.T) {
	svc, sess := newDispatchService(t)
	req := smbReq(protocol.CommandNtCancel, protocol.Flags2NTStatus, 1, 1, make([]byte, 16), nil)
	reply := svc.Dispatch(sess, req)
	h := respHeader(t, reply)
	if h.Status != statusNotSupported {
		t.Fatalf("NtCancel status = %#x, want STATUS_NOT_SUPPORTED", h.Status)
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
