package afp

import (
	"errors"
	"fmt"

	aspclient "github.com/ObsoleteMadness/ClassicStack/client/asp"
	proto "github.com/ObsoleteMadness/ClassicStack/core/protocol/afp"
)

// Login runs FPLogin over the session. An empty user is a guest login (No User
// Authent); a non-empty user uses the cleartext UAM (the two single-step UAMs the
// server accepts — matching core/service/afp/handlers.go:afpLogin). version selects the
// AFP version string; empty defaults to AFP2.2.
func Login(sess *aspclient.Session, user, pass, version string) error {
	if version == "" {
		version = proto.AFPVersion22
	}
	req := proto.LoginRequest{AFPVersion: version}
	if user == "" {
		req.UAM = proto.UAMNoUserAuthent
	} else {
		req.UAM = proto.UAMCleartext
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
