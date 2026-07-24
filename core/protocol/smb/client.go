// client.go holds the CLIENT-DIRECTION SMB1 codec: the request builders a
// redirector emits and the response parsers it reads. It is the mirror of the
// service handlers in core/service/smb (which build responses and parse requests) —
// this package deliberately keeps the two directions in separate files so the server
// ring is never refactored onto shared DTOs (the client SDK adds only the direction
// the servers lack).
//
// Every builder takes a *Builder that stamps the shared header ids (UID/TID/PID/MID)
// and the per-message Flags2 (Unicode / NT-status), so a caller threads its session
// state once and each command inherits it. Names are packed in the wire charset the
// Builder's Flags2 selects: UTF-16LE when SMB_FLAGS2_UNICODE is set, OEM/ANSI
// otherwise — matching wireFor() on the service side. This client negotiates NT LM
// 0.12 with Unicode, so it speaks UTF-16LE paths, but the OEM path is implemented too
// for the CORE/LANMAN dialects.
//
// Ring: CORE (stdlib only, reflection-free; LE codecs from core/binaryprimitives).
//
// Reference: [MS-CIFS] §2.2.4 (per-command request/response formats).
package smb

import (
	"errors"
	"strings"
	"unicode/utf16"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"
)

// Builder stamps the shared SMB1 header fields onto each request a client sends on
// one virtual circuit. UID/TID come from SESSION_SETUP / TREE_CONNECT; PID is the
// client's process id (any stable non-zero value); MID is bumped per request so a
// response can be matched to its request. Unicode selects the filename wire charset.
type Builder struct {
	UID     uint16
	TID     uint16
	PID     uint16
	MID     uint16
	Unicode bool // SMB_FLAGS2_UNICODE: pack names UTF-16LE (else OEM/ANSI)
	// NTStatus selects the header status dialect: when true the request sets
	// SMB_FLAGS2_NT_STATUS and advertises CAP_STATUS32, and responses carry 32-bit
	// NTSTATUS; when false the request uses DOS error codes and omits CAP_STATUS32. It is
	// set from the server's NEGOTIATE capabilities (NegotiateResult.SupportsNTStatus). A
	// Win9x File & Print server negotiates NT LM 0.12 but WITHOUT CAP_STATUS32 and silently
	// drops a request that claims NT status, so this must follow the server, not be assumed.
	NTStatus bool
	// SessionKey is the server's SessionKey from NEGOTIATE, which SESSION_SETUP echoes
	// verbatim ([MS-CIFS] §3.2.4.2.4). A Win9x File & Print server generates a non-zero
	// key and silently DISCARDS a SESSION_SETUP that carries 0 instead of echoing it
	// (observed: the request was NBF-DATA_ACKed but never answered at SMB). Set from
	// NegotiateResult.SessionKey.
	SessionKey uint32

	// MaxTransactBytes caps the MaxDataCount a TRANS2 request advertises — the largest
	// reply the server may return in one transaction. Zero means "no client cap" (the
	// server's own MaxBufferSize governs). A connectionless transport (direct SMB over
	// IPX) has no reassembly, so the whole reply must fit one datagram; the caller sets
	// this to a datagram-safe value there. A stream transport (TCP/NBT) leaves it 0.
	MaxTransactBytes uint16
}

// flags2 is the FLAGS2 word a request carries: always NT-status + long-names, plus
// Unicode when the session negotiated it. Matching the server's wireFor() keying, a
// request with the Unicode bit set sends UTF-16LE names and reads UTF-16LE strings
// back.
func (b *Builder) flags2() uint16 {
	f := Flags2KnowsLongNames | Flags2EAS
	if b.NTStatus {
		f |= Flags2NTStatus
	}
	if b.Unicode {
		f |= Flags2Unicode
	}
	return f
}

// header builds the request header for command cmd, bumping MID is the caller's
// concern (NextMID). Flags carries the standard request bits (canonicalized,
// case-insensitive paths — FlagsRequest); a request never sets SMB_FLAGS_REPLY.
func (b *Builder) header(cmd uint8) Header {
	return Header{
		Command: cmd,
		Flags:   FlagsRequest,
		Flags2:  b.flags2(),
		TID:     b.TID,
		PIDLow:  b.PID,
		UID:     b.UID,
		MID:     b.MID,
	}
}

// NextMID advances the multiplex id and returns the new value, so each request on the
// circuit carries a distinct MID ([MS-CIFS] §3.2.4.1 the client assigns a unique MID).
func (b *Builder) NextMID() uint16 {
	b.MID++
	return b.MID
}

// frame assembles a request frame: header + WordCount-prefixed words + ByteCount-
// prefixed area, the uniform SMB1 message shape (mirrors the service reply() helper).
func (b *Builder) frame(cmd uint8, words, area []byte) []byte {
	out := b.header(cmd).Encode(nil)
	out = append(out, byte(len(words)/2))
	out = append(out, words...)
	out = append(out, byte(len(area)), byte(len(area)>>8))
	return append(out, area...)
}

// ErrShortResponse is returned by a response parser when the frame is too short to
// carry the fields the command's format mandates.
var ErrShortResponse = errors.New("smb: response shorter than command format requires")

// ErrStatus wraps a non-success NTSTATUS/DOS status from a response header, so a
// caller can branch on the wire result. The service maps its internal NTSTATUS to the
// wire form (NT status or DOS class/code) by the request's Flags2; because this client
// always sets SMB_FLAGS2_NT_STATUS the value here is the raw NTSTATUS.
type ErrStatus struct {
	Command uint8
	Status  uint32
}

func (e *ErrStatus) Error() string {
	return "smb: " + CommandName(e.Command) + " failed: status 0x" + hex32(e.Status)
}

// hex32 formats a uint32 as eight uppercase hex digits.
func hex32(v uint32) string {
	const digits = "0123456789ABCDEF"
	var b [8]byte
	for i := 7; i >= 0; i-- {
		b[i] = digits[v&0xF]
		v >>= 4
	}
	return string(b[:])
}

// respBody splits a response frame into its header, parameter words, and byte area,
// verifying the reply flag and returning an *ErrStatus for a non-success status. It is
// the client-side counterpart of the service reqBody helper.
func respBody(cmd uint8, resp []byte) (h Header, words, area []byte, err error) {
	h, err = DecodeHeader(resp)
	if err != nil {
		return Header{}, nil, nil, err
	}
	if h.Status != StatusSuccess {
		return h, nil, nil, &ErrStatus{Command: cmd, Status: h.Status}
	}
	if len(resp) < HeaderLen+1 {
		return h, nil, nil, ErrShortResponse
	}
	wct := int(resp[HeaderLen])
	wStart := HeaderLen + 1
	bccOff := wStart + 2*wct
	if len(resp) < bccOff+2 {
		return h, nil, nil, ErrShortResponse
	}
	bcc := int(bp.LE16(resp[bccOff : bccOff+2]))
	dataOff := bccOff + 2
	if len(resp) < dataOff+bcc {
		return h, nil, nil, ErrShortResponse
	}
	return h, resp[wStart:bccOff], resp[dataOff : dataOff+bcc], nil
}

// --- NEGOTIATE ---

// clientDialects is the ordered dialect list this client offers. Least→most
// functional, matching the order a real redirector uses so the server's
// SelectDialect (most-recent-wins) picks NT LM 0.12 whenever both support it.
var clientDialects = []string{
	DialectPCNetwork1,
	DialectLANMAN10,
	DialectLM12X002,
	DialectLANMAN21,
	DialectNTLM,
}

// BuildNegotiate builds an SMB_COM_NEGOTIATE request offering clientDialects
// ([MS-CIFS] §2.2.4.52.1): WCT=0, the byte area a sequence of 0x02 buffer-format
// bytes each followed by a NUL-terminated dialect string. The header carries no
// UID/TID yet (the session is not established), so it is sent through a bare Builder.
func (b *Builder) BuildNegotiate() []byte {
	var area []byte
	for _, d := range clientDialects {
		area = append(area, 0x02)
		area = append(area, []byte(d)...)
		area = append(area, 0)
	}
	return b.frame(CommandNegotiate, nil, area)
}

// NegotiateResult is the parsed NEGOTIATE response a client needs: which dialect the
// server selected (by index into the offered list) and its family, plus whether the
// server runs USER-level security (so the client knows to send credentials). The
// server's SecurityMode/MaxBufferSize/Capabilities are read but only SecurityMode is
// surfaced — this client always uses plain READ/WRITE_ANDX and its own buffer cap.
type NegotiateResult struct {
	DialectIndex uint16
	Dialect      string
	Family       DialectFamily
	UserSecurity bool   // server advertised SECURITY_MODE_USER_SECURITY (send credentials)
	MaxBuffer    uint32 // server MaxBufferSize (the largest single request it accepts)
	Capabilities uint32 // server Capabilities word (NT family only; 0 for older dialects)
	SessionKey   uint32 // server SessionKey; the client echoes it in SESSION_SETUP
}

// SupportsNTStatus reports whether the negotiated server speaks 32-bit NTSTATUS in its
// headers (CAP_STATUS32). A server that does NOT — e.g. Windows 9x File & Print Sharing,
// which negotiates NT LM 0.12 but advertises Capabilities without CAP_STATUS32 and replies
// in DOS error codes — will silently DISCARD a request whose header sets SMB_FLAGS2_NT_STATUS
// (ground truth captures/nt-98-nbf.pcap: the MS redirector talking to that same Win98 box
// sends DOS-code Flags2 and gets a reply; our NT-status request was DATA_ACKed but never
// answered at SMB). So the client keys its Flags2 NT-status bit on this.
func (r NegotiateResult) SupportsNTStatus() bool { return r.Capabilities&CapNTStatus != 0 }

// ParseNegotiate parses an SMB_COM_NEGOTIATE response. The wire format is keyed by the
// selected dialect family ([MS-CIFS] §2.2.4.52.2): Core WCT=1 (DialectIndex only),
// LANMAN WCT=13 (16-bit SecurityMode/MaxBufferSize), NT WCT=17 (8-bit SecurityMode,
// 32-bit MaxBufferSize). DialectIndex 0xFFFF means the server matched none of our
// dialects. The selected dialect string is recovered from clientDialects by index.
func ParseNegotiate(resp []byte) (NegotiateResult, error) {
	h, words, _, err := respBody(CommandNegotiate, resp)
	if err != nil {
		return NegotiateResult{}, err
	}
	_ = h
	if len(words) < 2 {
		return NegotiateResult{}, ErrShortResponse
	}
	idx := bp.LE16(words[0:2])
	res := NegotiateResult{DialectIndex: idx}
	if idx == 0xFFFF || int(idx) >= len(clientDialects) {
		return res, errors.New("smb: server matched no offered dialect")
	}
	res.Dialect = clientDialects[idx]
	res.Family = dialectFamily(res.Dialect)

	switch res.Family {
	case DialectFamilyNT:
		// WCT=17: DialectIndex(2) SecurityMode(1) MaxMpxCount(2) MaxVcs(2)
		// MaxBufferSize(4, words[7:11]) MaxRawSize(4) SessionKey(4) Capabilities(4,
		// words[19:23]) ...
		if len(words) < 34 {
			return res, ErrShortResponse
		}
		res.UserSecurity = words[2]&0x01 != 0
		res.MaxBuffer = bp.LE32(words[7:11])
		res.SessionKey = bp.LE32(words[15:19])
		res.Capabilities = bp.LE32(words[19:23])
	case DialectFamilyLanMan:
		// WCT=13: DialectIndex(2) SecurityMode(2) MaxBufferSize(2, 16-bit).
		if len(words) < 6 {
			return res, ErrShortResponse
		}
		res.UserSecurity = bp.LE16(words[2:4])&0x01 != 0
		res.MaxBuffer = uint32(bp.LE16(words[4:6]))
	default:
		// Core WCT=1: no security/buffer fields; share-level, minimal buffer.
	}
	return res, nil
}

// --- SESSION_SETUP_ANDX ---

// BuildSessionSetup builds an SMB_COM_SESSION_SETUP_ANDX request in the NT LM 0.12
// form (WCT=13, [MS-CIFS] §2.2.4.53.1). It sends a cleartext password in the
// case-insensitive password field (the compatibility server validates cleartext; an
// empty password is the guest path). user/domain identify the account; the byte area
// carries CI-password + AccountName + PrimaryDomain + NativeOS + NativeLanMan.
//
// maxBuffer is the client's own MaxBufferSize (the largest response it will accept);
// the server saves it from the first setup ([MS-CIFS] §3.3.5.43).
func (b *Builder) BuildSessionSetup(user, password, domain string, maxBuffer uint16) []byte {
	// Case-insensitive (LM/ANSI) password: cleartext bytes + NUL. When no password is
	// given, send a SINGLE NUL byte (length 1), NOT a zero-length field: the "null
	// password" for a guest/share-level logon is one NUL. Ground truth
	// captures/nt-98-nbf.pcap frame 217 — the MS redirector logs into the same Win98 box
	// with ANSI Password Length 1 (a lone 0x00) and Win98 grants the session; a length-0
	// field is silently rejected. The server trims the trailing NUL either way.
	ciPass := append([]byte(password), 0)

	// Capabilities: advertise CAP_STATUS32 only when the session uses NT status (so a
	// non-NT-status Win9x server is not told we speak a status dialect it does not).
	caps := negotiateClientCaps
	if !b.NTStatus {
		caps &^= CapNTStatus
	}

	words := make([]byte, 26) // WCT=13
	words[0] = CommandNoAndXCommand
	words[1] = 0x00
	bp.PutLE16(words[2:4], 0)                     // AndXOffset (no chaining)
	bp.PutLE16(words[4:6], maxBuffer)             // MaxBufferSize
	bp.PutLE16(words[6:8], 1)                     // MaxMpxCount
	bp.PutLE16(words[8:10], 0)                    // VcNumber
	bp.PutLE32(words[10:14], b.SessionKey)        // SessionKey (echo the server's NEGOTIATE key)
	bp.PutLE16(words[14:16], uint16(len(ciPass))) // CaseInsensitivePasswordLength
	bp.PutLE16(words[16:18], 0)                   // CaseSensitivePasswordLength (no NTLM)
	bp.PutLE32(words[18:22], 0)                   // Reserved
	bp.PutLE32(words[22:26], caps)                // Capabilities

	var area []byte
	area = append(area, ciPass...)
	// The account name and following strings are in the wire charset. When Unicode is
	// set, the strings must be 2-byte aligned; a pad byte precedes them if the current
	// offset (after the CI password) is odd.
	if b.Unicode && len(area)%2 != 0 {
		area = append(area, 0)
	}
	area = appendWireString(area, user, b.Unicode)
	area = appendWireString(area, domain, b.Unicode)
	area = appendWireString(area, "ClassicStack", b.Unicode) // NativeOS
	area = appendWireString(area, "ClassicStack", b.Unicode) // NativeLanMan

	return b.frame(CommandSessionSetupAndX, words, area)
}

// negotiateClientCaps is the Capabilities word the client advertises in
// SESSION_SETUP: NT SMBs + 32-bit status + NT find + large files, mirroring the
// server's negotiateCapabilities so both agree on the NT feature set. CAP_STATUS32 is
// masked out at build time when the negotiated server does not support it (Win9x).
const negotiateClientCaps uint32 = CapNTSMBs | CapNTStatus | CapNTFind | CapLargeFiles

// SessionSetupResult is the parsed SESSION_SETUP_ANDX response: the granted UID (from
// the response header) and whether the server logged the client in as guest.
type SessionSetupResult struct {
	UID   uint16
	Guest bool
}

// ParseSessionSetup parses an SMB_COM_SESSION_SETUP_ANDX response (WCT=3:
// AndXCommand/AndXReserved/AndXOffset + Action). Action bit 0 (0x0001) is
// SMB_SETUP_GUEST — the server granted a guest session ([MS-CIFS] §2.2.4.53.2). The
// UID is taken from the response header, which the client sends on every subsequent
// request.
func ParseSessionSetup(resp []byte) (SessionSetupResult, error) {
	h, words, _, err := respBody(CommandSessionSetupAndX, resp)
	if err != nil {
		return SessionSetupResult{}, err
	}
	res := SessionSetupResult{UID: h.UID}
	if len(words) >= 6 {
		res.Guest = bp.LE16(words[4:6])&0x0001 != 0
	}
	return res, nil
}

// --- TREE_CONNECT_ANDX ---

// BuildTreeConnect builds an SMB_COM_TREE_CONNECT_ANDX request (WCT=4, [MS-CIFS]
// §2.2.4.55.1) for the UNC path \\server\share. The password is empty (share-level
// auth is not used by this client); the byte area carries Password + Path + Service.
// The Path is always OEM/ASCII on the wire even in a Unicode session in the classic
// TREE_CONNECT_ANDX form we use (Flags bit for Unicode paths is left clear), matching
// the server's parseTreeConnectShareName which splits OEM NUL strings.
func (b *Builder) BuildTreeConnect(server, share string) []byte {
	words := make([]byte, 8) // WCT=4
	words[0] = CommandNoAndXCommand
	words[1] = 0x00
	bp.PutLE16(words[2:4], 0) // AndXOffset
	bp.PutLE16(words[4:6], 0) // Flags
	bp.PutLE16(words[6:8], 1) // PasswordLength = 1 (a single NUL for no password)

	unc := `\\` + server + `\` + share
	var area []byte
	area = append(area, 0)                  // Password: one NUL (length 1, matches PasswordLength)
	area = append(area, []byte(unc)...)     // Path (OEM/ASCII)
	area = append(area, 0)                  // Path NUL
	area = append(area, []byte("?????")...) // Service: "?????" = any type
	area = append(area, 0)
	return b.frame(CommandTreeConnectAndX, words, area)
}

// ParseTreeConnect parses an SMB_COM_TREE_CONNECT_ANDX response (WCT=3), returning the
// granted TID from the response header. The service string in the byte area ("A:" /
// "IPC") is not needed by the client.
func ParseTreeConnect(resp []byte) (tid uint16, err error) {
	h, _, _, err := respBody(CommandTreeConnectAndX, resp)
	if err != nil {
		return 0, err
	}
	return h.TID, nil
}

// --- TREE_DISCONNECT / LOGOFF ---

// BuildTreeDisconnect builds an SMB_COM_TREE_DISCONNECT request (WCT=0) releasing the
// Builder's TID.
func (b *Builder) BuildTreeDisconnect() []byte {
	return b.frame(CommandTreeDisconnect, nil, nil)
}

// BuildLogoff builds an SMB_COM_LOGOFF_ANDX request (WCT=2) clearing the granted UID.
func (b *Builder) BuildLogoff() []byte {
	words := make([]byte, 4)
	words[0] = CommandNoAndXCommand
	bp.PutLE16(words[2:4], 0) // AndXOffset
	return b.frame(CommandLogoffAndX, words, nil)
}

// --- wire-string helpers (client direction) ---

// appendWireString appends s NUL-terminated in the wire charset: UTF-16LE (with a
// 0x0000 terminator) when unicode, else OEM/ASCII bytes with a single-NUL terminator.
// The mirror of the service readWireString.
func appendWireString(dst []byte, s string, unicode bool) []byte {
	if unicode {
		for _, u := range utf16.Encode([]rune(s)) {
			dst = append(dst, byte(u), byte(u>>8))
		}
		return append(dst, 0, 0)
	}
	dst = append(dst, []byte(s)...)
	return append(dst, 0)
}

// encodePath encodes a share-relative '/'-separated path as an SMB wire path: a
// leading backslash, backslash separators, in the wire charset. Empty (the share
// root) becomes "\". The result is NOT NUL-terminated here (callers append the
// terminator or a length as the command format needs).
func encodePathBytes(path string, unicode bool) []byte {
	wirePath := "\\" + strings.ReplaceAll(strings.Trim(path, "/"), "/", "\\")
	if unicode {
		var out []byte
		for _, u := range utf16.Encode([]rune(wirePath)) {
			out = append(out, byte(u), byte(u>>8))
		}
		return out
	}
	return []byte(wirePath)
}
