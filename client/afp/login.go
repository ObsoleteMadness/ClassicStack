package afp

import (
	"errors"
	"fmt"
	"strings"

	aspclient "github.com/ObsoleteMadness/ClassicStack/client/asp"
	proto "github.com/ObsoleteMadness/ClassicStack/core/protocol/afp"
)

// Login runs FPLogin over the session. An empty user is a guest login (No User
// Authent); a non-empty user uses the cleartext UAM (the two single-step UAMs the
// server accepts — matching core/service/afp/handlers.go:afpLogin). version selects the
// AFP version string; empty defaults to AFPVersion21 (the classic-server baseline, and
// a value a System 7.x server actually advertises — unlike "AFP2.2").
func Login(sess *aspclient.Session, user, pass, version string) error {
	if version == "" {
		version = proto.AFPVersion21
	}
	uam := proto.UAMCleartext
	if user == "" {
		uam = proto.UAMNoUserAuthent
	}
	return login(sess, version, uam, user, pass)
}

// LoginNegotiated runs FPLogin choosing the AFP version and UAM from the server's
// advertised FPGetSrvrInfo (srv). It is the correct client behaviour: a classic Mac
// server SILENTLY IGNORES an FPLogin naming a version string or UAM it did not
// advertise, so the version and the UAM name must come from the server's own lists
// verbatim (including their exact case, e.g. "Cleartxt passwrd"). It falls back to the
// client defaults when srv is empty (GetStatus failed) or advertised nothing usable.
func LoginNegotiated(sess *aspclient.Session, user, pass string, srv proto.ServerInfo) error {
	version := srv.PickVersion()
	if version == "" {
		version = proto.AFPVersion21 // GetStatus failed / no known version advertised
	}

	// Guest (No User Authent) when no user is given; otherwise the cleartext UAM,
	// preferring the server's advertised spelling (case varies by server) and falling
	// back to the canonical constant when it did not advertise the list.
	uam := proto.UAMNoUserAuthent
	if user != "" {
		uam = pickCleartextUAM(srv)
	}
	return login(sess, version, uam, user, pass)
}

// pickCleartextUAM returns the server's advertised cleartext-password UAM name (the
// spelling/case varies — "Cleartxt Passwrd" vs "Cleartxt passwrd"), or the canonical
// constant when the server advertised no UAM list. It matches case-insensitively
// against the known cleartext name so either spelling the server used is honoured.
func pickCleartextUAM(srv proto.ServerInfo) string {
	for _, u := range srv.UAMs {
		if strings.EqualFold(u, proto.UAMCleartext) {
			return u // use the server's exact spelling
		}
	}
	return proto.UAMCleartext
}

// login sends one FPLogin command block with the chosen version/UAM/credentials and
// maps a non-zero AFP result to an error.
func login(sess *aspclient.Session, version, uam, user, pass string) error {
	req := proto.LoginRequest{AFPVersion: version, UAM: uam}
	if uam != proto.UAMNoUserAuthent {
		req.User = user
		req.Pass = pass
	}
	_, result, err := sess.Command(req.Marshal())
	if err != nil {
		return err
	}
	if result != proto.NoErr {
		return afpError("FPLogin", result)
	}
	return nil
}

// afpError wraps a non-zero AFP result code as an error naming the command.
func afpError(cmd string, code int32) error {
	return fmt.Errorf("afp: %s: %s (%d)", cmd, proto.ResultName(code), code)
}

// errMalformed reports a reply the client parser could not decode.
func errMalformed(what string) error {
	return fmt.Errorf("afp: malformed %s", what)
}

// IsNotFound reports whether err is an AFP object-not-found error, so callers can map
// it to fs.ErrNotExist semantics.
func IsNotFound(err error) bool {
	var afpErr interface{ Error() string }
	if errors.As(err, &afpErr) {
		return contains(err.Error(), "kFPObjectNotFound") || contains(err.Error(), "kFPDirNotFound")
	}
	return false
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
