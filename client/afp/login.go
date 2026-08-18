package afp

import (
	"errors"
	"fmt"
	"strings"

	aspclient "github.com/ObsoleteMadness/ClassicStack/client/asp"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
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

	var uam string
	var err error
	if user == "" {
		uam, err = pickGuestUAM(srv)
	} else {
		uam, err = pickPasswordUAM(srv)
	}
	if err != nil {
		return err
	}
	afpLog.Log(log.Debug, "FPLogin negotiate",
		log.Str("version", version),
		log.Str("uam", uam),
		log.Str("user", user),
		log.Int("pass_len", int64(len(pass))),
		log.Str("advertised_versions", strings.Join(srv.AFPVersions, "|")),
		log.Str("advertised_uams", strings.Join(srv.UAMs, "|")),
		log.Bool("guest", user == ""))
	if user != "" && proto.IsRandnumUAM(uam) {
		return loginRandnum(sess, version, uam, user, pass)
	}
	return login(sess, version, uam, user, pass)
}

// pickGuestUAM returns the server's advertised guest UAM name (spelling varies) when
// the server offers one. When GetStatus failed (empty UAM list) it falls back to the
// canonical constant.
func pickGuestUAM(srv proto.ServerInfo) (string, error) {
	for _, u := range srv.UAMs {
		if strings.EqualFold(u, proto.UAMNoUserAuthent) {
			return u, nil
		}
	}
	if len(srv.UAMs) == 0 {
		return proto.UAMNoUserAuthent, nil
	}
	return "", fmt.Errorf("afp: server does not offer guest login (advertised uams: %v)", srv.UAMs)
}

// pickPasswordUAM picks the best password UAM the server advertised that this client
// implements. ClassicStack-web prefers advertised cleartext (verbatim spelling) and
// only uses Randnum when cleartext is absent. System 7.1 Personal File Sharing
// accepts a word-aligned Cleartxt FPLogin; a misaligned password field returns
// kFPUserNotAuth (observed 2026-08-18).
func pickPasswordUAM(srv proto.ServerInfo) (string, error) {
	if u, err := pickCleartextUAM(srv); err == nil {
		return u, nil
	}
	if u, err := pickRandnumUAM(srv); err == nil {
		return u, nil
	}
	if len(srv.UAMs) == 0 {
		return proto.UAMCleartext, nil
	}
	return "", fmt.Errorf("afp: no supported password UAM (advertised uams: %v)", srv.UAMs)
}

// pickRandnumUAM returns the server's advertised Randnum exchange UAM name (verbatim
// spelling), or the canonical constant when GetStatus failed.
func pickRandnumUAM(srv proto.ServerInfo) (string, error) {
	for _, u := range srv.UAMs {
		if proto.IsRandnumUAM(u) {
			return u, nil
		}
	}
	if len(srv.UAMs) == 0 {
		return proto.UAMRandnum, nil
	}
	return "", fmt.Errorf("afp: server does not offer Randnum exchange (advertised uams: %v)", srv.UAMs)
}

// pickCleartextUAM returns the server's advertised cleartext-password UAM name (the
// spelling/case varies — "Cleartxt Passwrd" vs "Cleartxt passwrd"), or the canonical
// constant when GetStatus failed (empty list). When the server advertised UAMs but none
// is cleartext, it returns an error rather than sending an unsupported UAM name.
func pickCleartextUAM(srv proto.ServerInfo) (string, error) {
	for _, u := range srv.UAMs {
		if strings.EqualFold(u, proto.UAMCleartext) {
			return u, nil
		}
	}
	if len(srv.UAMs) == 0 {
		return proto.UAMCleartext, nil
	}
	return "", fmt.Errorf("afp: server does not offer cleartext password login (advertised uams: %v)", srv.UAMs)
}

// login sends one FPLogin command block with the chosen version/UAM/credentials and
// maps a non-zero AFP result to an error.
func login(sess *aspclient.Session, version, uam, user, pass string) error {
	req := proto.LoginRequest{AFPVersion: version, UAM: uam, User: user, Pass: pass}
	_, result, err := sess.Command(req.Marshal())
	if err != nil {
		return err
	}
	if result != proto.NoErr {
		afpLog.Log(log.Debug, "FPLogin failed",
			log.Str("version", version),
			log.Str("uam", uam),
			log.Bool("guest", strings.EqualFold(uam, proto.UAMNoUserAuthent)),
			log.Str("result", proto.ResultName(result)),
			log.Int("code", int64(result)))
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
