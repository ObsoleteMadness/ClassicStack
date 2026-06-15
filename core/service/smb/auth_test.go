package smb

import (
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
	bad := newSession()
	hb := respHeader(t, svc.Dispatch(bad, sessionSetupNT("alice", "wrong")))
	if hb.Status != statusLogonFailure {
		t.Fatalf("bad-login status = %#x, want LOGON_FAILURE", hb.Status)
	}
	if bad.user != "" {
		t.Fatalf("failed login left identity %q", bad.user)
	}
}

func TestSMBShareGatedByIdentity(t *testing.T) {
	svc := restrictedService(t)

	// Guest session: PRIVATE is hidden from NetShareEnum and refused at tree-connect.
	guest := newSession()
	names := enumShareNames(svc, guest)
	if !names["PUBLIC"] || names["PRIVATE"] {
		t.Fatalf("guest share list = %v, want PUBLIC only", names)
	}
	if h := respHeader(t, svc.Dispatch(guest, treeConnectReq("PRIVATE"))); h.Status != statusBadNetworkName {
		t.Fatalf("guest tree-connect PRIVATE status = %#x, want BAD_NETWORK_NAME", h.Status)
	}

	// alice session: PRIVATE listed and bindable.
	alice := newSession()
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
