package afp

import (
	"crypto/des"

	"github.com/ObsoleteMadness/ClassicStack/core/log"
	proto "github.com/ObsoleteMadness/ClassicStack/core/protocol/afp"
)

// afpPasswordKey turns a Mac AFP password into the 8-byte DES key. Shorter passwords
// are suffixed with NUL ($00) to 8 bytes — Inside AppleTalk AFP engineering notes
// Appendix A (Cleartext UAM; Randnum uses the same format). A blank owner password
// is therefore eight zeros, not eight spaces.
func afpPasswordKey(pass string) [8]byte {
	var key [8]byte
	copy(key[:], pass)
	return key
}

// randnumEncrypt DES-ECB-encrypts one 8-byte block with key (Randnum exchange UAM).
func randnumEncrypt(key, plain [8]byte) ([8]byte, error) {
	c, err := des.NewCipher(key[:])
	if err != nil {
		return [8]byte{}, err
	}
	var out [8]byte
	c.Encrypt(out[:], plain[:])
	return out, nil
}

// loginRandnum runs FPLogin + FPLoginCont for the Randnum exchange UAM: the server
// returns kFPAuthContinue with a session ID and 8-byte challenge; the client DES-
// encrypts the challenge with the user's password as key and sends it in FPLoginCont.
func loginRandnum(sess Session, version, uam, user, pass string) error {
	body, result, err := sess.Command(proto.LoginRequest{
		AFPVersion: version,
		UAM:        uam,
		User:       user,
	}.Marshal())
	if err != nil {
		afpLog.Log(log.Debug, "FPLogin Randnum transport error", log.Str("err", err.Error()))
		return err
	}
	if !proto.IsAuthContinue(result) {
		if result != proto.NoErr {
			afpLog.Log(log.Debug, "FPLogin Randnum failed",
				log.Str("version", version),
				log.Str("uam", uam),
				log.Str("result", proto.ResultName(result)),
				log.Int("code", int64(result)))
			return afpError("FPLogin", result)
		}
		return nil
	}
	sessID, challenge, ok := proto.ParseLoginContinueReply(body)
	if !ok {
		afpLog.Log(log.Debug, "FPLogin Randnum malformed continue reply", log.Int("len", int64(len(body))))
		return errMalformed("FPLogin Randnum continue reply")
	}
	afpLog.Log(log.Debug, "FPLogin Randnum continue",
		log.Int("id", int64(sessID)),
		log.Int("reply_len", int64(len(body))),
		log.Int("pass_len", int64(len(pass))))
	key := afpPasswordKey(pass)
	resp, err := randnumEncrypt(key, challenge)
	if err != nil {
		afpLog.Log(log.Debug, "FPLogin Randnum encrypt failed", log.Str("err", err.Error()))
		return err
	}
	_, result, err = sess.Command(proto.LoginContRequest{
		SessionID: sessID,
		Response:  resp,
	}.Marshal())
	if err != nil {
		afpLog.Log(log.Debug, "FPLoginCont Randnum transport error", log.Str("err", err.Error()))
		return err
	}
	if result != proto.NoErr {
		afpLog.Log(log.Debug, "FPLoginCont Randnum failed",
			log.Str("result", proto.ResultName(result)),
			log.Int("code", int64(result)))
		return afpError("FPLoginCont", result)
	}
	return nil
}
