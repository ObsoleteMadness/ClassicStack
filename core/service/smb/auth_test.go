package smb

import (
	"strings"
	"testing"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	protocol "github.com/ObsoleteMadness/ClassicStack/core/protocol/smb"
)

// fakeAuth admits exactly one (user, pass) pair.
type fakeAuth struct{ user, pass string }

func (f fakeAuth) Authenticate(user, pass string) (bool, error) {
	return user == f.user && pass == f.pass, nil
}

// sessionSetupNT builds an NT LM 0.12 SESSION_SETUP_ANDX (WCT=13) request carrying
// a cleartext case-insensitive password and an OEM AccountName.
func sessionSetupNT(user, pass string) []byte {
	words := make([]byte, 26)
	words[0] = protocol.CommandNoAndXCommand
	bp.PutLE16(words[14:16], uint16(len(pass)+1)) // CaseInsensitivePasswordLength (incl NUL)
	bp.PutLE16(words[16:18], 0)                   // CaseSensitivePasswordLength = 0 (cleartext)

	area := append([]byte(pass), 0) // case-insensitive password + NUL
	area = append(area, []byte(user)...)
	area = append(area, 0) // AccountName NUL
	area = append(area, 0) // PrimaryDomain NUL
	return smbReq(protocol.CommandSessionSetupAndX, protocol.Flags2NTStatus, 0, 0, words, area)
}

// sessionSetupDOS builds an NT LM 0.12 SESSION_SETUP_ANDX with the NT-status bit
// CLEAR (Flags2=0), i.e. a CORE/DOS-error client such as Win9x/WfW, carrying a
// cleartext case-insensitive password (len 0 = none) and an OEM AccountName —
// the exact shape of the WIN98USER setup in captures/ipx.pcap.
func sessionSetupDOS(user, pass string) []byte {
	words := make([]byte, 26)
	words[0] = protocol.CommandNoAndXCommand
	if pass != "" {
		bp.PutLE16(words[14:16], uint16(len(pass)+1)) // CaseInsensitivePasswordLength (incl NUL)
	}
	bp.PutLE16(words[16:18], 0) // CaseSensitivePasswordLength = 0 (cleartext)

	var area []byte
	if pass != "" {
		area = append([]byte(pass), 0)
	}
	area = append(area, []byte(user)...)
	area = append(area, 0) // AccountName NUL
	area = append(area, 0) // PrimaryDomain NUL
	return smbReq(protocol.CommandSessionSetupAndX, 0, 0, 0, words, area)
}

// treeConnectReq builds a TREE_CONNECT_ANDX for \\SERVER\<share>.
func treeConnectReq(share string) []byte {
	words := make([]byte, 8)
	words[0] = protocol.CommandNoAndXCommand
	bp.PutLE16(words[6:8], 1) // PasswordLength = 1
	area := []byte{0x00}
	area = append(area, []byte("\\\\SERVER\\"+share)...)
	area = append(area, 0)
	area = append(area, []byte("?????")...)
	area = append(area, 0)
	return smbReq(protocol.CommandTreeConnectAndX, protocol.Flags2NTStatus, 0, 1, words, area)
}

func restrictedService(t *testing.T) *Service {
	t.Helper()
	pub, err := NewShare(ShareSpec{Name: "PUBLIC", Share: fs.ShareSpec{FSType: "memfs"}})
	if err != nil {
		t.Fatal(err)
	}
	priv, err := NewShare(ShareSpec{Name: "PRIVATE", Share: fs.ShareSpec{FSType: "memfs", AllowedUsers: []string{"alice"}}})
	if err != nil {
		t.Fatal(err)
	}
	return &Service{shares: []*Share{pub, priv}}
}

func TestSMBSessionSetup_GuestWhenNoStore(t *testing.T) {
	svc, sess := newDispatchService(t)
	reply := svc.Dispatch(sess, sessionSetupNT("alice", "pw"))
	h := respHeader(t, reply)
	if h.Status != statusSuccess {
		t.Fatalf("status = %#x, want success", h.Status)
	}
	// Action word (after the 3 AndX words) = 0x0001 guest.
	action := bp.LE16(reply[protocol.HeaderLen+1+4 : protocol.HeaderLen+1+6])
	if action != 0x0001 {
		t.Fatalf("Action = %#x, want guest (0x0001) with no store wired", action)
	}
	if sess.user != "" {
		t.Fatalf("identity = %q, want guest", sess.user)
	}
}

func TestSMBSessionSetup_AuthedAndDenied(t *testing.T) {
	svc, sess := newDispatchService(t)
	svc.SetAuthenticator(fakeAuth{user: "alice", pass: "secret"})

	// Correct credential → non-guest (Action 0x0000), identity recorded.
	reply := svc.Dispatch(sess, sessionSetupNT("alice", "secret"))
	h := respHeader(t, reply)
	if h.Status != statusSuccess {
		t.Fatalf("good-login status = %#x, want success", h.Status)
	}
	if action := bp.LE16(reply[protocol.HeaderLen+1+4 : protocol.HeaderLen+1+6]); action != 0x0000 {
		t.Fatalf("Action = %#x, want non-guest (0x0000)", action)
	}
	if sess.user != "alice" {
		t.Fatalf("identity = %q, want alice", sess.user)
	}

	// Wrong password → STATUS_LOGON_FAILURE, no identity.
	bad := newSession("")
	hb := respHeader(t, svc.Dispatch(bad, sessionSetupNT("alice", "wrong")))
	if hb.Status != statusLogonFailure {
		t.Fatalf("bad-login status = %#x, want LOGON_FAILURE", hb.Status)
	}
	if bad.user != "" {
		t.Fatalf("failed login left identity %q", bad.user)
	}
}

// TestSMBSessionSetup_NamedNoPasswordIsGuest reproduces captures/ipx.pcap: a
// WfW/Win9x client (WIN98USER) sends its logon name with an EMPTY password to a
// guest-open server. Even with a store wired this must NOT be treated as a failed
// authentication — the client presented no credential — and must grant a guest
// session (Action=0x0001), exactly as the legacy service always did. The refactor
// had authenticated ""-password named setups and returned STATUS_LOGON_FAILURE.
func TestSMBSessionSetup_NamedNoPasswordIsGuest(t *testing.T) {
	svc, sess := newDispatchService(t)
	svc.SetAuthenticator(fakeAuth{user: "alice", pass: "secret"})

	resp := svc.Dispatch(sess, sessionSetupDOS("WIN98USER", ""))
	h := respHeader(t, resp)
	if h.Status != statusSuccess {
		t.Fatalf("no-password setup status = %#x, want success (guest)", h.Status)
	}
	if action := bp.LE16(resp[protocol.HeaderLen+1+4 : protocol.HeaderLen+1+6]); action != 0x0001 {
		t.Fatalf("no-password setup Action = %#x, want guest (0x0001)", action)
	}
	if sess.user != "" {
		t.Fatalf("no-password setup identity = %q, want guest", sess.user)
	}
}

// TestSMBSessionSetup_DOSClientLogonFailureWireForm proves a genuine logon
// failure returned to a CORE/DOS-error client (Flags2 NT-status bit clear) is
// encoded as a DOS class/code the client can parse (ERRSRV/ERRbadpw), NOT the raw
// NTSTATUS 0xC000006D — which decodes as bogus error class 0x6d on the wire (the
// captures/ipx.pcap symptom). The status field's low byte is the ErrorClass.
func TestSMBSessionSetup_DOSClientLogonFailureWireForm(t *testing.T) {
	svc, sess := newDispatchService(t)
	svc.SetAuthenticator(fakeAuth{user: "alice", pass: "secret"})

	resp := svc.Dispatch(sess, sessionSetupDOS("alice", "wrong"))
	h := respHeader(t, resp)
	// DOS wire form: ERRSRV(class 2)/ERRbadpw(code 2) = 0x00020002.
	if h.Status != 0x00020002 {
		t.Fatalf("DOS logon-failure status = %#x, want 0x00020002 (ERRSRV/ERRbadpw)", h.Status)
	}
	if h.Status&0xFF000000 != 0 {
		t.Fatalf("status %#x is a raw NTSTATUS on a DOS-codes client", h.Status)
	}
}

func TestSMBShareGatedByIdentity(t *testing.T) {
	svc := restrictedService(t)

	// Guest session: PRIVATE is hidden from NetShareEnum and refused at tree-connect.
	guest := newSession("")
	names := enumShareNames(svc, guest)
	if !names["PUBLIC"] || names["PRIVATE"] {
		t.Fatalf("guest share list = %v, want PUBLIC only", names)
	}
	if h := respHeader(t, svc.Dispatch(guest, treeConnectReq("PRIVATE"))); h.Status != statusBadNetworkName {
		t.Fatalf("guest tree-connect PRIVATE status = %#x, want BAD_NETWORK_NAME", h.Status)
	}

	// alice session: PRIVATE listed and bindable.
	alice := newSession("")
	alice.user = "alice"
	names = enumShareNames(svc, alice)
	if !names["PUBLIC"] || !names["PRIVATE"] {
		t.Fatalf("alice share list = %v, want both", names)
	}
	if h := respHeader(t, svc.Dispatch(alice, treeConnectReq("PRIVATE"))); h.Status != statusSuccess {
		t.Fatalf("alice tree-connect PRIVATE status = %#x, want success", h.Status)
	}
}

// enumShareNames returns the set of disk-share names shareEntries lists for user.
func enumShareNames(svc *Service, sess *smbSession) map[string]bool {
	out := map[string]bool{}
	for _, e := range svc.shareEntries(sess.user) {
		if e.Type == shareTypeDisktree {
			out[e.Name] = true
		}
	}
	return out
}

func TestSMBSessionSetup_UnicodeAndASCIIFields(t *testing.T) {
	svc, sess := newDispatchService(t)
	svc.SetServerName("MYSERVER")
	svc.SetWorkgroup("MYWORKGROUP")

	// 1. Test ASCII/OEM response (Flags2Unicode clear)
	reqASCII := sessionSetupDOS("alice", "") // Flags2 = 0
	respASCII := svc.Dispatch(sess, reqASCII)

	hASCII := respHeader(t, respASCII)
	if hASCII.Status != statusSuccess {
		t.Fatalf("ASCII status = %#x, want success", hASCII.Status)
	}

	// Calculate offset of byte area
	// HeaderLen(32) + 1 (WCT) + 6 (Words) = 39. BCC starts at 39 (2 bytes). Byte area starts at 41.
	bccASCII := bp.LE16(respASCII[39:41])
	byteAreaASCII := respASCII[41:]
	if int(bccASCII) != len(byteAreaASCII) {
		t.Fatalf("ASCII BCC mismatch: got %d, bytes area len %d", bccASCII, len(byteAreaASCII))
	}

	// Split ASCII byte area on NUL bytes
	partsASCII := strings.Split(string(byteAreaASCII), "\x00")
	if len(partsASCII) < 4 { // three strings + trailing empty part from final NUL
		t.Fatalf("ASCII expected 3 NUL-terminated fields, got: %q", partsASCII)
	}
	if partsASCII[0] != "MYSERVER" || partsASCII[1] != "MYSERVER" || partsASCII[2] != "MYWORKGROUP" {
		t.Fatalf("ASCII fields mismatch: got %q, want %q, %q, %q", partsASCII[:3], "MYSERVER", "MYSERVER", "MYWORKGROUP")
	}

	// 2. Test Unicode response (Flags2Unicode set)
	reqUnicodeHeader := sessionSetupNT("alice", "")
	// Header is 32 bytes. Flags2 is at offset 10 (2 bytes).
	// Let's modify the Flags2 field of reqUnicodeHeader to set Flags2Unicode.
	flags2 := bp.LE16(reqUnicodeHeader[10:12])
	flags2 |= protocol.Flags2Unicode
	bp.PutLE16(reqUnicodeHeader[10:12], flags2)

	respUnicode := svc.Dispatch(sess, reqUnicodeHeader)
	hUnicode := respHeader(t, respUnicode)
	if hUnicode.Status != statusSuccess {
		t.Fatalf("Unicode status = %#x, want success", hUnicode.Status)
	}

	bccUnicode := bp.LE16(respUnicode[39:41])
	byteAreaUnicode := respUnicode[41:]
	if int(bccUnicode) != len(byteAreaUnicode) {
		t.Fatalf("Unicode BCC mismatch: got %d, bytes area len %d", bccUnicode, len(byteAreaUnicode))
	}

	// The first byte of the Unicode byte area must be a padding byte (0x00)
	if byteAreaUnicode[0] != 0x00 {
		t.Fatalf("Unicode expected padding byte 0x00 at start of byte area, got 0x%02x", byteAreaUnicode[0])
	}

	// Decode UTF-16LE strings from byte area after padding
	decodeUTF16LE := func(b []byte) string {
		var runes []rune
		for i := 0; i+1 < len(b); i += 2 {
			r := rune(b[i]) | rune(b[i+1])<<8
			if r == 0 {
				break
			}
			runes = append(runes, r)
		}
		return string(runes)
	}

	var decoded []string
	rest := byteAreaUnicode[1:]
	for len(rest) > 0 {
		nulIdx := -1
		for i := 0; i+1 < len(rest); i += 2 {
			if rest[i] == 0 && rest[i+1] == 0 {
				nulIdx = i
				break
			}
		}
		if nulIdx == -1 {
			break
		}
		s := decodeUTF16LE(rest[:nulIdx])
		decoded = append(decoded, s)
		rest = rest[nulIdx+2:]
	}

	if len(decoded) < 3 {
		t.Fatalf("Unicode expected at least 3 fields, got %d: %q", len(decoded), decoded)
	}
	if decoded[0] != "MYSERVER" || decoded[1] != "MYSERVER" || decoded[2] != "MYWORKGROUP" {
		t.Fatalf("Unicode fields mismatch: got %q, want %q, %q, %q", decoded[:3], "MYSERVER", "MYSERVER", "MYWORKGROUP")
	}
}

// sessionSetupNTWithMaxBufferSize is sessionSetupNT with the client's
// MaxBufferSize field (word offset 4, [MS-CIFS] §2.2.4.53.1) set explicitly.
func sessionSetupNTWithMaxBufferSize(user, pass string, maxBufferSize uint16) []byte {
	req := sessionSetupNT(user, pass)
	bp.PutLE16(req[protocol.HeaderLen+1+4:protocol.HeaderLen+1+6], maxBufferSize)
	return req
}

// TestSMBSessionSetup_ClientMaxBufferSizeSticky proves the session records the
// client's SESSION_SETUP_ANDX MaxBufferSize ([MS-CIFS] §3.2.1.2
// Server.Connection.ClientMaxBufferSize — "This limit applies to all SMB
// messages sent to the client") from the FIRST request only, falls back to
// defaultClientMaxBufferSize before any SESSION_SETUP, and is NOT overridden
// by a later SESSION_SETUP on the same connection (§3.3.5.43: "These values
// MUST NOT be overridden by values presented in future ... request messages").
func TestSMBSessionSetup_ClientMaxBufferSizeSticky(t *testing.T) {
	svc, sess := newDispatchService(t)

	if got := sess.maxBufferSize(); got != defaultClientMaxBufferSize {
		t.Fatalf("maxBufferSize before SESSION_SETUP = %d, want default %d", got, defaultClientMaxBufferSize)
	}

	if h := respHeader(t, svc.Dispatch(sess, sessionSetupNTWithMaxBufferSize("alice", "", 8712))); h.Status != statusSuccess {
		t.Fatalf("first SESSION_SETUP status = %#x, want success", h.Status)
	}
	if got := sess.maxBufferSize(); got != 8712 {
		t.Fatalf("maxBufferSize after first SESSION_SETUP = %d, want 8712", got)
	}

	// A second SESSION_SETUP on the same connection (e.g. adding a user) must
	// not change the recorded value.
	if h := respHeader(t, svc.Dispatch(sess, sessionSetupNTWithMaxBufferSize("alice", "", 2000))); h.Status != statusSuccess {
		t.Fatalf("second SESSION_SETUP status = %#x, want success", h.Status)
	}
	if got := sess.maxBufferSize(); got != 8712 {
		t.Fatalf("maxBufferSize after second SESSION_SETUP = %d, want unchanged 8712", got)
	}
}
