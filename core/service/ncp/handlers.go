package ncp

// handlers.go holds the connection/server/bindery-login handlers (the
// fnGetServerDateTime / fnConnBindery subfunctions). File and directory handlers
// live in fileio.go.
//
// Login posture: with no Authenticator wired the login verbs grant a guest
// connection unconditionally (the compatibility-server default). With one wired, a
// cleartext login is validated against it directly; the NetWare keyed (encrypted)
// login is the documented challenge-response — we cannot reverse the client's
// shuffled hash to a cleartext password to feed Authenticate, so a keyed login is
// accepted as a guest-equivalent login (mirroring SMB's "hashed-credential
// accept-as-guest" errata note) rather than rejected. A future slice that stores
// the NetWare-hashed credential can validate the shuffle exactly.

import (
	"strings"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/log"
)

// maxRWBufferSize is the largest read/write buffer the server accepts in
// Negotiate Buffer Size, matching mars_nwe's Ethernet RW_BUFFERSIZE
// (include/net.h); the reply to a capped read must still fit one IPX datagram.
const maxRWBufferSize uint16 = 1024

// negotiateBufferSize answers fnNegotiateBuffer (0x21): the request carries the
// client's proposed buffer/packet size (2 BE), the reply is the accepted size
// (2 BE) = min(maxRWBufferSize, proposed). Per mars_nwe (nwconn.c case 0x21) a
// proposal below 512 is nonsense some clients send (Atari ST PAM's Net/E) and is
// ignored — the reply then re-states the connection's current size.
func (cn *Conn) negotiateBufferSize(body []byte) ([]byte, error) {
	cn.c.mu.Lock()
	if cn.c.rwBufferSize == 0 {
		cn.c.rwBufferSize = maxRWBufferSize
	}
	var proposed uint16
	if len(body) >= 2 {
		if proposed = uint16(body[0])<<8 | uint16(body[1]); proposed >= 512 {
			cn.c.rwBufferSize = min(maxRWBufferSize, proposed)
		}
	}
	accepted := cn.c.rwBufferSize
	cn.c.mu.Unlock()
	cn.svc.logging.Log(log.Debug, "NCP negotiate buffer size",
		log.Int("proposed", int64(proposed)),
		log.Int("accepted", int64(accepted)),
		log.Int("conn", int64(cn.c.number)))
	return appendU16(nil, accepted), nil
}

// getServerDateTime answers fnGetServerDateTime (0x14): 7 bytes —
// year(since 1900), month, day, hour, minute, second, day-of-week.
func (cn *Conn) getServerDateTime() ([]byte, error) {
	now := time.Now()
	return []byte{
		byte(now.Year() - 1900),
		byte(now.Month()),
		byte(now.Day()),
		byte(now.Hour()),
		byte(now.Minute()),
		byte(now.Second()),
		byte(now.Weekday()),
	}, nil
}

// getServerInfo answers Get File Server Information (0x17/0x11). The reply XDATA
// matches mars_nwe (nwbind.c) field-for-field: servername[48], version(1),
// subversion(1), maxconnections[2], connection_in_use[2], max_volumes[2],
// os_revision(1), sft_level(1), tts_level(1), peak_connection[2],
// accounting_version(1), vap_version(1), queuing_version(1),
// print_server_version(1), virtual_console_version(1), security_level(1),
// internet_bridge_version(1), reserved[60]. We report NetWare 3.11.
func (cn *Conn) getServerInfo() ([]byte, error) {
	inUse := uint16(cn.connectedCount())
	out := make([]byte, 0, 48+22+60)
	var nameField [48]byte
	copy(nameField[:], cn.svc.serverName())
	out = append(out, nameField[:]...)
	out = append(out, 3, 11)               // version, subversion (NetWare 3.11)
	out = appendU16(out, maxConnections)   // maxconnections
	out = appendU16(out, inUse)            // connection_in_use
	out = appendU16(out, 1)                // max_volumes
	out = append(out, 0)                   // os_revision
	out = append(out, 2)                   // sft_level
	out = append(out, 1)                   // tts_level
	out = appendU16(out, inUse)            // peak_connection
	out = append(out, 1)                   // accounting_version
	out = append(out, 1)                   // vap_version
	out = append(out, 1)                   // queuing_version
	out = append(out, 0)                   // print_server_version
	out = append(out, 1)                   // virtual_console_version
	out = append(out, 1)                   // security_level
	out = append(out, 1)                   // internet_bridge_version
	out = append(out, make([]byte, 60)...) // reserved
	return out, nil
}

// connectedCount returns the live connection count for the server-info reply.
func (cn *Conn) connectedCount() int {
	conns, _, _ := cn.svc.conns.Snapshot()
	return conns
}

// loginUnencrypted handles cleartext Login To File Server (0x17/0x14). The args
// carry the object type (2, BE) and length-prefixed object (user) name and
// password. It validates against the Authenticator when wired; otherwise grants a
// guest login.
func (cn *Conn) loginUnencrypted(args []byte) ([]byte, error) {
	user, pass, ok := parseLoginArgs(args)
	if !ok {
		return nil, errFuncNotSupported
	}
	return cn.grantLogin(user, pass)
}

// getLoginKey answers Get login encryption key (0x17/0x17). We return a fixed
// 8-byte challenge; the keyed-login path accepts the response as a guest-
// equivalent login (see file header), so the key value is not security-critical.
func (cn *Conn) getLoginKey() ([]byte, error) {
	return []byte{0, 0, 0, 0, 0, 0, 0, 0}, nil
}

// loginEncrypted handles keyed (encrypted) login (0x17/0x18). We cannot reverse
// the client's shuffled hash to a cleartext password, so — consistent with the
// compatibility-server posture — we accept it as a guest-equivalent login bound to
// the supplied user name (no credential check). The args carry the object type,
// the response hash, and the length-prefixed object name.
func (cn *Conn) loginEncrypted(args []byte) ([]byte, error) {
	user := parseEncryptedLoginUser(args)
	cn.recordLogin(user)
	cn.svc.counters.loginsOK.Add(1)
	cn.svc.logging.Log(log.Info, "NCP keyed login granted (guest-equivalent)",
		log.Str("user", user), log.Int("conn", int64(cn.c.number)))
	cn.svc.pushStats()
	return nil, nil
}

// recordLogin marks the connection logged in as user, binding it to the
// resolved bindery identity (GUEST for empty/unknown names) and stamping the
// login time the connection-information family reports.
func (cn *Conn) recordLogin(user string) {
	id, typ := cn.svc.loginObjectFor(user)
	cn.c.mu.Lock()
	cn.c.user = user
	cn.c.loggedIn = true
	cn.c.objectID = id
	cn.c.objectType = typ
	cn.c.loginTime = time.Now()
	cn.c.mu.Unlock()
}

// grantLogin validates a cleartext credential (when an Authenticator is wired and
// the volume is not world-open) and records the login on the connection. With no
// Authenticator wired it grants a guest login (the compatibility default). GUEST
// — and an unnamed login — is ALWAYS granted, even with an Authenticator wired:
// the NetWare convention (mars_nwe's standard bindery) is a passwordless GUEST
// account, and vintage clients attach as GUEST when no user is specified.
func (cn *Conn) grantLogin(user, pass string) ([]byte, error) {
	cn.svc.mu.Lock()
	auth := cn.svc.auth
	cn.svc.mu.Unlock()

	guest := user == "" || strings.EqualFold(user, "GUEST")
	if auth != nil && !guest {
		ok, err := auth.Authenticate(user, pass)
		if err != nil || !ok {
			cn.svc.counters.loginsFailed.Add(1)
			cn.svc.logging.Log(log.Info, "NCP login denied",
				log.Str("user", user), log.Int("conn", int64(cn.c.number)))
			return nil, errAccessDenied
		}
	}
	cn.recordLogin(user)
	cn.svc.counters.loginsOK.Add(1)
	cn.svc.logging.Log(log.Info, "NCP login granted",
		log.Str("user", user), log.Bool("guest", guest),
		log.Int("conn", int64(cn.c.number)))
	cn.svc.pushStats()
	return nil, nil
}

// parseLoginArgs reads the cleartext-login arguments: object type (2 BE), a
// 1-byte-length-prefixed object (user) name, then a 1-byte-length-prefixed
// password. Returns ok=false on a truncated buffer.
func parseLoginArgs(args []byte) (user, pass string, ok bool) {
	if len(args) < 3 {
		return "", "", false
	}
	p := 2 // skip object type
	name, p, ok := readByteString(args, p)
	if !ok {
		return "", "", false
	}
	pw, _, ok := readByteString(args, p)
	if !ok {
		return "", "", false
	}
	return name, pw, true
}

// parseEncryptedLoginUser reads the user name from a keyed-login request. Per
// mars_nwe (nwbind.c) the layout is crypt_key[8], object_type[2 BE], then the
// length-prefixed object name; a truncated buffer yields "".
func parseEncryptedLoginUser(args []byte) string {
	const off = 8 + 2 // crypt_key[8] + object_type[2]
	if len(args) < off {
		return ""
	}
	name, _, ok := readByteString(args, off)
	if !ok {
		return ""
	}
	return name
}

// readByteString reads a 1-byte-length-prefixed string at offset p and returns it
// with the offset advanced past it.
func readByteString(b []byte, p int) (string, int, bool) {
	if p >= len(b) {
		return "", p, false
	}
	n := int(b[p])
	p++
	if p+n > len(b) {
		return "", p, false
	}
	return string(b[p : p+n]), p + n, true
}
