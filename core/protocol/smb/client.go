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
//
// NEGOTIATE is the exception: it is sent BEFORE any dialect is agreed, so it carries
// the bare pre-negotiation header — see negotiateFlags/negotiateFlags2.
func (b *Builder) header(cmd uint8) Header {
	if cmd == CommandNegotiate {
		return Header{
			Command: cmd,
			Flags:   negotiateFlags,
			Flags2:  negotiateFlags2,
			TID:     b.TID,
			PIDLow:  b.PID,
			UID:     b.UID,
			MID:     b.MID,
		}
	}
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
// wire form (NT status or DOS class/code) by the request's Flags2.
//
// DOS reports whether the reply used the DOS class/code encoding rather than a 32-bit
// NTSTATUS — that is, whether the RESPONSE header cleared SMB_FLAGS2_NT_STATUS. It
// matters for reading the value: a DOS status packs ErrorClass in the low byte and
// ErrorCode in the high word, so ERRSRV(2)/18 appears as the uint32 0x00120002, which
// is not a meaningful NTSTATUS at all (its severity bits say "success"). This client
// does NOT always set SMB_FLAGS2_NT_STATUS — NEGOTIATE never does (negotiateFlags2)
// and a server without CAP_STATUS32 answers everything in DOS codes — so the encoding
// has to be read off the reply rather than assumed.
type ErrStatus struct {
	Command uint8
	Status  uint32
	DOS     bool
}

// ErrorClass returns the DOS error class (ERRDOS 1 / ERRSRV 2 / ERRHRD 3) and code
// from a DOS-encoded status. It is meaningless when DOS is false.
func (e *ErrStatus) ErrorClass() (class uint8, code uint16) {
	return uint8(e.Status), uint16(e.Status >> 16)
}

func (e *ErrStatus) Error() string {
	if e.DOS {
		class, code := e.ErrorClass()
		return "smb: " + CommandName(e.Command) + " failed: " + dosErrorName(class, code)
	}
	return "smb: " + CommandName(e.Command) + " failed: status 0x" + hex32(e.Status)
}

// DOS error classes ([MS-CIFS] §2.2.3.1 SMB_ERROR, [smb6.0] 4442). ErrorClass sits in
// the low byte of a DOS-encoded header Status.
const (
	ErrClassSuccess uint8 = 0x00
	ErrClassDOS     uint8 = 0x01 // ERRDOS — generated by the OS/2-style file system
	ErrClassSrv     uint8 = 0x02 // ERRSRV — generated by the server network file manager
	ErrClassHrd     uint8 = 0x03 // ERRHRD — hardware error
	ErrClassCmd     uint8 = 0xFF // ERRCMD — not an SMB request
)

// ERRSRV codes this client can meet and name ([smb6.0] 4571ff). Only the ones we
// actually distinguish are listed; anything else prints as a bare number.
const (
	ErrSrvError    uint16 = 1  // non-specific: first command on VC was not negotiate, internal error
	ErrSrvBadPw    uint16 = 2  // bad name/password pair in Tree Connect or Session Setup
	ErrSrvAccess   uint16 = 4  // no access rights in the TID/UID context
	ErrSrvInvNid   uint16 = 5  // invalid TID
	ErrSrvInvNetNm uint16 = 6  // invalid network name in tree connect
	ErrSrvSmbCmd   uint16 = 64 // server did not recognise the command
	// ErrSrvUnknownName (18) is NOT in the published ERRSRV table ([smb6.0] 4571
	// jumps 7 → 49). ERRATA: a Win98 direct-hosted-IPX server answers with it when a
	// NEGOTIATE arrives carrying no [SOURCE][DESTINATION] name trailer, i.e. when
	// nothing in the datagram says which of the server's NetBIOS names it is for.
	// See the ERRATA on AppendNameTrailer.
	ErrSrvUnknownName uint16 = 18
)

// dosErrorName renders a DOS class/code pair as "ERRSRV/ERRbadpw (2/2)" — the class
// mnemonic, the code mnemonic when known, and always the raw numbers so an unnamed
// code is still diagnosable.
func dosErrorName(class uint8, code uint16) string {
	var name string
	switch class {
	case ErrClassDOS:
		name = "ERRDOS"
	case ErrClassSrv:
		name = "ERRSRV"
	case ErrClassHrd:
		name = "ERRHRD"
	case ErrClassCmd:
		name = "ERRCMD"
	default:
		name = "class " + dec(uint32(class))
	}
	if class == ErrClassSrv {
		switch code {
		case ErrSrvError:
			name += "/ERRerror"
		case ErrSrvBadPw:
			name += "/ERRbadpw"
		case ErrSrvAccess:
			name += "/ERRaccess"
		case ErrSrvInvNid:
			name += "/ERRinvnid"
		case ErrSrvInvNetNm:
			name += "/ERRinvnetname"
		case ErrSrvSmbCmd:
			name += "/ERRsmbcmd"
		case ErrSrvUnknownName:
			name += "/unknown-name"
		}
	}
	return name + " (" + dec(uint32(class)) + "/" + dec(uint32(code)) + ")"
}

// dec formats a uint32 in decimal without fmt (core ring: reflection-free).
func dec(v uint32) string {
	if v == 0 {
		return "0"
	}
	var b [10]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	return string(b[i:])
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
		// The reply's own Flags2 says which encoding its Status uses — the request's
		// does not decide it, and NEGOTIATE requests carry no NT-status bit at all.
		return h, nil, nil, &ErrStatus{
			Command: cmd,
			Status:  h.Status,
			DOS:     h.Flags2&Flags2NTStatus == 0,
		}
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

// negotiateFlags / negotiateFlags2 are the Flags and Flags2 an SMB_COM_NEGOTIATE
// request carries. They are ZERO, not the FlagsRequest (0x18) every LATER request
// uses, because NEGOTIATE precedes the dialect agreement: the client cannot yet claim
// capabilities the negotiated dialect has not established.
//
// ERRATA — this is a PER-MESSAGE property, not a per-transport one. Every golden
// capture agrees, across all three carriers:
//
//	client                 NEGOTIATE (0x72)     SESSION_SETUP / TREE_CONNECT
//	Win98 (nbf/nbipx/nwlink) Flags 0x00, F2 0x0000   Flags 0x10
//	OS/2  (nbf-os2-win98)    Flags 0x08, F2 0x0000   Flags 0x18 / 0x08
//
// (spec/captures/nbf-win98.pcap frames 77/81, nbipx-win98.pcap 67/69,
// nwlink-win98.pcap 16/18, nbf-os2-win98.pcap 100/104.) NOT ONE of them sets 0x18 on
// a NEGOTIATE, and Flags2 is 0x0000 on every observed NEGOTIATE.
//
// We used to stamp FlagsRequest (0x18) and Flags2 0x0003 (KnowsLongNames|EAS) on
// NEGOTIATE too. The FlagsRequest errata that motivated 0x18 is about SESSION_SETUP —
// an NT redirector's SESSION_SETUP carries 0x18, matching the OS/2 column above — and
// never justified it on NEGOTIATE.
//
// Clearing Flags2 also clears SMB_FLAGS2_NT_STATUS, so a NEGOTIATE failure comes back
// DOS-encoded; ErrStatus reads the encoding off the reply rather than assuming NTSTATUS.
// (This header was investigated as the cause of a Win98 direct-hosted-IPX server
// refusing our NEGOTIATE with ERRSRV/18. It was NOT: that refusal was the missing
// direct-IPX name trailer, see AppendNameTrailer. The per-message Flags finding stands
// on the four captures above in its own right.)
const (
	negotiateFlags  uint8  = 0x00
	negotiateFlags2 uint16 = 0x0000
)

// clientDialects is the ordered dialect list this client offers. Least→most
// functional, matching the order a real redirector uses so the server's
// SelectDialect (most-recent-wins) picks NT LM 0.12 whenever both support it.
//
// It offers BOTH the OS/2-flavoured names (LANMAN1.0 / LM1.2X002 / LANMAN2.1) and
// the DOS-flavoured ones (MICROSOFT NETWORKS 3.0 / DOS LM1.2X002 / DOS LANMAN2.1 /
// Windows for Workgroups 3.1a). A real Win9x NWLink redirector offers the DOS family
// — golden capture spec/captures/nwlink-win98.pcap frame 16 lists exactly
// PC NETWORK PROGRAM 1.0, MICROSOFT NETWORKS 3.0, DOS LM1.2X002, DOS LANMAN2.1,
// Windows for Workgroups 3.1a, NT LM 0.12 — and a server that only recognises the
// DOS spellings would find nothing it knows in an OS/2-only list. Offering both
// costs one byte-area entry each and cannot lose: SelectDialect is most-recent-wins,
// so NT LM 0.12 still wins against any peer that speaks it.
var clientDialects = []string{
	DialectPCNetwork1,
	DialectMSNet30,
	DialectLANMAN10,
	DialectDOSLM12,
	DialectLM12X002,
	DialectDOSLANMAN2,
	DialectLANMAN21,
	DialectWfW311,
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
	DialectIndex     uint16
	Dialect          string
	Family           DialectFamily
	UserSecurity     bool   // server advertised SECURITY_MODE_USER_SECURITY (send credentials)
	EncryptPasswords bool   // server advertised NEGOTIATE_ENCRYPT_PASSWORDS
	MaxBuffer        uint32 // server MaxBufferSize (the largest single request it accepts)
	Capabilities     uint32 // server Capabilities word (NT family only; 0 for older dialects)
	SessionKey       uint32 // server SessionKey; the client echoes it in SESSION_SETUP
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
		sec := uint16(words[2])
		res.UserSecurity = sec&SecurityModeUser != 0
		res.EncryptPasswords = sec&SecurityModeEncrypt != 0
		res.MaxBuffer = bp.LE32(words[7:11])
		res.SessionKey = bp.LE32(words[15:19])
		res.Capabilities = bp.LE32(words[19:23])
	case DialectFamilyLanMan:
		// WCT=13: DialectIndex(2) SecurityMode(2) MaxBufferSize(2, 16-bit).
		if len(words) < 6 {
			return res, ErrShortResponse
		}
		sec := bp.LE16(words[2:4])
		res.UserSecurity = sec&SecurityModeUser != 0
		res.EncryptPasswords = sec&SecurityModeEncrypt != 0
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
	bp.PutLE16(words[6:8], sessionSetupMaxMpx)    // MaxMpxCount
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

// sessionSetupMaxMpx is the MaxMpxCount advertised in SESSION_SETUP. The MS redirector
// sends 2 against this Win98 box (captures/nt-98-nbf.pcap frame 217); a client should not
// exceed the server's advertised count but 2 is within Win98's own advert.
const sessionSetupMaxMpx = 2

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
	return b.buildTreeConnect(server, share, "?????")
}

// ServiceIPC is the Service string for the inter-process-communication pipe share
// (IPC$), over which RAP transactions (NetShareEnum) ride.
const ServiceIPC = "IPC"

// BuildTreeConnectIPC builds a TREE_CONNECT_ANDX to the server's IPC$ pipe share,
// declaring Service "IPC" so the server binds the transaction pipe rather than a disk
// tree. It is the tree the RAP NetShareEnum transaction runs on.
func (b *Builder) BuildTreeConnectIPC(server string) []byte {
	return b.buildTreeConnect(server, "IPC$", ServiceIPC)
}

func (b *Builder) buildTreeConnect(server, share, service string) []byte {
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
	area = append(area, []byte(service)...) // Service ("?????" any / "IPC" pipe)
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

// --- RAP NetShareEnum (share list over IPC$ \PIPE\LANMAN) ---

// RAP (Remote Administration Protocol, [MS-RAP]) constants for the NetShareEnum call
// carried in an SMB_COM_TRANSACTION on the IPC$ \PIPE\LANMAN pipe.
const (
	rapNetShareEnum uint16 = 0x0000 // NetShareEnum function code
	// The RAP descriptor strings for NetShareEnum level 1: ParamDesc "WrLeh" (share
	// level W, receive buffer r/L, entries-read e, available h) and ReturnDesc "B13BWz"
	// (SHARE_INFO_1: netname B13, pad B, type W, remark pointer z) — the format the
	// server's buildNetShareEnumResponse produces (20-byte records + remark heap).
	rapNetShareEnumParamDesc  = "WrLeh"
	rapNetShareEnumReturnDesc = "B13BWz"
	rapShareInfo1Level        = 1     // detail level 1 → SHARE_INFO_1
	rapReceiveBufferLen       = 65535 // ask for the largest reply the server will pack
	// rapNetShareEnumReplyParamLen is the reply's parameter block size: Status(2) +
	// Converter(2) + EntriesReturned(2) + EntriesAvailable(2) = 8. This is the request's
	// MaxParameterCount — a too-large value (e.g. the receive-buffer length) makes Win98
	// misframe the reply (it echoed 0xFFFF back as TotalParameterCount, corrupting the
	// param/data split).
	rapNetShareEnumReplyParamLen = 8
)

const lanmanPipe = `\PIPE\LANMAN`

// shareInfo1Size is the on-wire SHARE_INFO_1 record: netname(13)+pad(1)+type(2)+
// remark-pointer(4) = 20 bytes ([MS-RAP] SHARE_INFO_1), matching the server.
const shareInfo1Size = 20

// STYPE_* share types ([MS-SRVS]) reported in SHARE_INFO_1.shi1_type.
const (
	ShareTypeDisk uint16 = 0x0000 // STYPE_DISKTREE
	ShareTypeIPC  uint16 = 0x0003 // STYPE_IPC
)

// ShareInfo is one enumerated share: its name, STYPE_* type, and remark/comment.
type ShareInfo struct {
	Name    string
	Type    uint16
	Comment string
}

// BuildNetShareEnum builds the SMB_COM_TRANSACTION request that carries a RAP
// NetShareEnum (level 1) over the IPC$ \PIPE\LANMAN pipe. The transaction's parameter
// area is the RAP request: function code + ParamDesc + ReturnDesc + Level +
// ReceiveBufferLength; there is no transaction data. The TID must already name the IPC$
// tree.
func (b *Builder) BuildNetShareEnum() []byte {
	// RAP request parameter block.
	rap := make([]byte, 0, 32)
	rap = bp.AppendLE16(rap, rapNetShareEnum)
	rap = append(rap, []byte(rapNetShareEnumParamDesc)...)
	rap = append(rap, 0)
	rap = append(rap, []byte(rapNetShareEnumReturnDesc)...)
	rap = append(rap, 0)
	rap = bp.AppendLE16(rap, rapShareInfo1Level)
	rap = bp.AppendLE16(rap, rapReceiveBufferLen)

	name := lanmanPipe + "\x00" // the transaction Name (the pipe), OEM/ASCII

	// SMB_COM_TRANSACTION request, WCT=14 ([MS-CIFS] §2.2.4.33.1). Setup words = 0. The
	// byte area is: Name\0 [pad] Parameters [Data]. Parameter/Data offsets are
	// header-relative.
	const wct = 14
	words := make([]byte, wct*2)
	bp.PutLE16(words[0:2], uint16(len(rap)))             // TotalParameterCount
	bp.PutLE16(words[2:4], 0)                            // TotalDataCount
	bp.PutLE16(words[4:6], rapNetShareEnumReplyParamLen) // MaxParameterCount (reply param block)
	bp.PutLE16(words[6:8], rapReceiveBufferLen)          // MaxDataCount (max reply data — the share records)
	words[8] = 0                                         // MaxSetupCount
	// words[9] Reserved; words[10:12] Flags = 0; words[12:16] Timeout = 0; words[16:18] Reserved.
	bp.PutLE16(words[18:20], uint16(len(rap))) // ParameterCount
	// ParameterOffset / DataOffset computed below once we know the byte-area layout.
	bp.PutLE16(words[22:24], 0) // DataCount
	words[26] = 0               // SetupCount (no setup words)
	// words[27] Reserved.

	// Byte area: Name\0, then (2-byte aligned) the RAP parameters.
	area := make([]byte, 0, len(name)+1+len(rap))
	area = append(area, []byte(name)...)
	// Parameters must start on an even offset from the SMB header. Compute the current
	// header-relative offset and pad to align.
	base := HeaderLen + 1 + wct*2 + 2 // header + WCT + words + ByteCount
	paramOff := base + len(area)
	if paramOff%2 != 0 {
		area = append(area, 0)
		paramOff++
	}
	area = append(area, rap...)

	bp.PutLE16(words[20:22], uint16(paramOff))          // ParameterOffset
	bp.PutLE16(words[24:26], uint16(paramOff+len(rap))) // DataOffset (no data; points past params)

	return b.frame(CommandTransaction, words, area)
}

// ParseNetShareEnum parses the SMB_COM_TRANSACTION response to a RAP NetShareEnum. The
// transaction parameter block is Status(2)+Converter(2)+EntriesReturned(2)+
// EntriesAvailable(2); the data block is EntriesReturned SHARE_INFO_1 records (20 bytes
// each) followed by a remark heap. Each record's remark pointer low word is the offset
// of its NUL-terminated comment within the data block, adjusted by the Converter word
// ([MS-RAP]: the server may bias heap pointers; Converter is subtracted). A non-zero RAP
// Status is returned as an error.
func ParseNetShareEnum(resp []byte) ([]ShareInfo, error) {
	_, _, _, err := respBody(CommandTransaction, resp)
	if err != nil {
		return nil, err
	}
	params, data, err := transactionResponse(resp)
	if err != nil {
		return nil, err
	}
	if len(params) < 8 {
		return nil, ErrShortResponse
	}
	status := bp.LE16(params[0:2])
	if status != 0 {
		return nil, &RAPError{Status: status}
	}
	converter := bp.LE16(params[2:4])
	entries := int(bp.LE16(params[4:6]))

	out := make([]ShareInfo, 0, entries)
	for i := 0; i < entries; i++ {
		base := i * shareInfo1Size
		if base+shareInfo1Size > len(data) {
			break
		}
		rec := data[base : base+shareInfo1Size]
		name := oemString(rec[0:13]) // netname, NUL-padded within 13
		typ := bp.LE16(rec[14:16])
		remark := ""
		// The remark pointer (low word) is a data-relative offset once the Converter bias
		// is removed.
		ptr := bp.LE16(rec[16:18])
		if ptr >= converter {
			off := int(ptr - converter)
			if off >= 0 && off < len(data) {
				remark = oemStringZ(data[off:])
			}
		}
		out = append(out, ShareInfo{Name: name, Type: typ, Comment: remark})
	}
	return out, nil
}

// RAPError is a non-zero RAP Status returned in a TRANSACTION reply's parameter block.
type RAPError struct{ Status uint16 }

func (e *RAPError) Error() string { return "smb: RAP status " + hex16(e.Status) }

// hex16 formats a uint16 as four uppercase hex digits.
func hex16(v uint16) string {
	const d = "0123456789ABCDEF"
	return string([]byte{d[v>>12&0xF], d[v>>8&0xF], d[v>>4&0xF], d[v&0xF]})
}

// transactionResponse extracts the parameter and data blocks from an SMB_COM_TRANSACTION
// response (WCT=10) using its header-relative ParameterOffset/DataOffset.
func transactionResponse(resp []byte) (params, data []byte, err error) {
	if len(resp) < HeaderLen+1 {
		return nil, nil, ErrShortResponse
	}
	wct := int(resp[HeaderLen])
	wStart := HeaderLen + 1
	if wct < 10 || wStart+2*wct+2 > len(resp) {
		return nil, nil, ErrShortResponse
	}
	w := resp[wStart : wStart+2*wct]
	pCount := int(bp.LE16(w[6:8]))
	pOff := int(bp.LE16(w[8:10]))
	dCount := int(bp.LE16(w[12:14]))
	dOff := int(bp.LE16(w[14:16]))
	if pOff+pCount > len(resp) || dOff+dCount > len(resp) {
		return nil, nil, ErrShortResponse
	}
	return resp[pOff : pOff+pCount], resp[dOff : dOff+dCount], nil
}

// oemString reads a NUL-padded OEM/ASCII string from a fixed-width field.
func oemString(b []byte) string {
	if i := indexZero(b); i >= 0 {
		b = b[:i]
	}
	return string(b)
}

// oemStringZ reads a NUL-terminated OEM/ASCII string from the start of b.
func oemStringZ(b []byte) string {
	if i := indexZero(b); i >= 0 {
		return string(b[:i])
	}
	return string(b)
}

// indexZero returns the index of the first NUL byte in b, or -1.
func indexZero(b []byte) int {
	for i, c := range b {
		if c == 0 {
			return i
		}
	}
	return -1
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
