package afp

import (
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	proto "github.com/ObsoleteMadness/ClassicStack/core/protocol/afp"
)

// fakeAuth is a tiny Authenticator: it admits exactly one (user, pass) pair.
type fakeAuth struct{ user, pass string }

func (f fakeAuth) Authenticate(user, pass string) (bool, error) {
	return user == f.user && pass == f.pass, nil
}

// loginBlock builds the FPLogin argument bytes (passed to afpLogin as block[1:]):
// version pstring, UAM pstring, then for cleartext: username pstring + 8-byte
// password field (space-padded).
func loginBlock(version, uam, user, pass string) []byte {
	out := []byte{byte(len(version))}
	out = append(out, version...)
	out = append(out, byte(len(uam)))
	out = append(out, uam...)
	if uam == "Cleartxt Passwrd" {
		out = append(out, byte(len(user)))
		out = append(out, user...)
		pw := make([]byte, 8)
		copy(pw, pass)
		out = append(out, pw...)
	}
	return out
}

func newAuthService(t *testing.T) *Service {
	t.Helper()
	s := New(log.New(Name))
	return s
}

func TestAFPLogin_GuestDisabled(t *testing.T) {
	s := newAuthService(t)
	s.SetAuthenticator(guestGateAuth{enabled: false, user: "alice", pass: "secret"})

	a := newAFPSession()
	if _, res := s.afpLogin(a, loginBlock("AFP2.2", "No User Authent", "", "")); res != afpErrUserNotAuth {
		t.Fatalf("No User Authent with Guest disabled = %d, want UserNotAuth", res)
	}

	anon := newAFPSession()
	if _, res := s.afpLogin(anon, loginBlock("AFP2.2", "Cleartxt Passwrd", "", "")); res != afpErrUserNotAuth {
		t.Fatalf("anonymous cleartext with Guest disabled = %d, want UserNotAuth", res)
	}

	ok := newAFPSession()
	if _, res := s.afpLogin(ok, loginBlock("AFP2.2", "Cleartxt Passwrd", "alice", "secret")); res != afpNoErr {
		t.Fatalf("named login with Guest disabled = %d, want NoErr", res)
	}
}

// guestGateAuth is a fake Authenticator that also implements GuestEnabled.
type guestGateAuth struct {
	enabled    bool
	user, pass string
}

func (g guestGateAuth) Authenticate(user, pass string) (bool, error) {
	return user == g.user && pass == g.pass, nil
}
func (g guestGateAuth) GuestEnabled() bool { return g.enabled }

func TestAFPLogin_GuestUAM(t *testing.T) {
	s := newAuthService(t)
	a := newAFPSession()
	if _, res := s.afpLogin(a, loginBlock("AFP2.2", "No User Authent", "", "")); res != afpNoErr {
		t.Fatalf("guest login result = %d, want NoErr", res)
	}
	if !a.loggedIn || a.user != "" {
		t.Fatalf("guest session loggedIn=%v user=%q", a.loggedIn, a.user)
	}
}

func TestAFPLogin_CleartextNoStoreIsGuest(t *testing.T) {
	s := newAuthService(t) // no authenticator wired
	a := newAFPSession()
	if _, res := s.afpLogin(a, loginBlock("AFP2.2", "Cleartxt Passwrd", "alice", "pw")); res != afpNoErr {
		t.Fatalf("cleartext-without-store result = %d, want NoErr (guest)", res)
	}
	if !a.loggedIn || a.user != "" {
		t.Fatalf("expected guest identity, got loggedIn=%v user=%q", a.loggedIn, a.user)
	}
}

func TestAFPLogin_CleartextValidatesAgainstStore(t *testing.T) {
	s := newAuthService(t)
	s.SetAuthenticator(fakeAuth{user: "alice", pass: "secret"})
	a := newAFPSession()

	if _, res := s.afpLogin(a, loginBlock("AFP2.2", "Cleartxt Passwrd", "alice", "secret")); res != afpNoErr {
		t.Fatalf("valid login result = %d, want NoErr", res)
	}
	if a.user != "alice" {
		t.Fatalf("identity = %q, want alice", a.user)
	}

	bad := newAFPSession()
	if _, res := s.afpLogin(bad, loginBlock("AFP2.2", "Cleartxt Passwrd", "alice", "wrong")); res != afpErrUserNotAuth {
		t.Fatalf("bad-password result = %d, want UserNotAuth", res)
	}
	if bad.loggedIn {
		t.Fatal("session logged in despite a bad password")
	}

	// Empty username with a store wired → guest (anonymous cleartext attempt).
	anon := newAFPSession()
	if _, res := s.afpLogin(anon, loginBlock("AFP2.2", "Cleartxt Passwrd", "", "")); res != afpNoErr {
		t.Fatalf("anonymous cleartext result = %d, want NoErr (guest)", res)
	}
	if anon.user != "" {
		t.Fatalf("anonymous identity = %q, want guest", anon.user)
	}
}

func TestAFPLogin_EvenPaddedCleartext(t *testing.T) {
	s := newAuthService(t)
	s.SetAuthenticator(fakeAuth{user: "mac", pass: ""})
	a := newAFPSession()
	block := proto.LoginRequest{
		AFPVersion: "AFP2.2",
		UAM:        "Cleartxt Passwrd",
		User:       "mac",
		Pass:       "",
	}.Marshal()
	if _, res := s.afpLogin(a, block[1:]); res != afpNoErr {
		t.Fatalf("even-padded blank-password login = %d, want NoErr", res)
	}
	if a.user != "mac" {
		t.Fatalf("identity = %q, want mac", a.user)
	}
}

func TestAFPVolumeListFilteredByIdentity(t *testing.T) {
	s := newAuthService(t)
	pub, err := NewVolume(VolumeSpec{Name: "Public", Share: fs.ShareSpec{FSType: "memfs"}})
	if err != nil {
		t.Fatal(err)
	}
	priv, err := NewVolume(VolumeSpec{Name: "Private", Share: fs.ShareSpec{FSType: "memfs", AllowedUsers: []string{"alice"}}})
	if err != nil {
		t.Fatal(err)
	}
	s.volumes = []*Volume{pub, priv}

	// A guest sees only the public volume; OpenVol on Private is refused.
	guest := newAFPSession()
	guest.loggedIn = true
	if names := volNames(s.afpGetSrvrParms(guest)); !contains(names, "Public") || contains(names, "Private") {
		t.Fatalf("guest volume list = %v, want only Public", names)
	}
	if _, res := s.afpOpenVol(guest, openVolBlock("Private")); res != afpErrObjectNotFnd {
		t.Fatalf("guest OpenVol(Private) = %d, want ObjectNotFound", res)
	}

	// alice sees both and may open Private.
	alice := newAFPSession()
	alice.loggedIn = true
	alice.user = "alice"
	if names := volNames(s.afpGetSrvrParms(alice)); !contains(names, "Public") || !contains(names, "Private") {
		t.Fatalf("alice volume list = %v, want both", names)
	}
	if _, res := s.afpOpenVol(alice, openVolBlock("Private")); res != afpNoErr {
		t.Fatalf("alice OpenVol(Private) = %d, want NoErr", res)
	}
}

// openVolBlock builds an FPOpenVol request block: cmd, pad, bitmap(2), pstring name.
func openVolBlock(name string) []byte {
	out := []byte{cmdOpenVol, 0, 0, 0, byte(len(name))}
	return append(out, name...)
}

// volNames extracts the volume names from an FPGetSrvrParms reply
// (uint32 time, uint8 count, {uint8 flags, pstring name} × count).
func volNames(reply []byte) []string {
	if len(reply) < 5 {
		return nil
	}
	count := int(reply[4])
	off := 5
	var names []string
	for i := 0; i < count && off < len(reply); i++ {
		off++ // flags
		if off >= len(reply) {
			break
		}
		n := int(reply[off])
		off++
		if off+n > len(reply) {
			break
		}
		names = append(names, string(reply[off:off+n]))
		off += n
	}
	return names
}
