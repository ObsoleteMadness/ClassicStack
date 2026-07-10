package smb

import (
	"strings"
	"time"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"

	protocol "github.com/ObsoleteMadness/ClassicStack/core/protocol/smb"
)

// --- SMB1 session-establishment handlers (NEGOTIATE / SESSION_SETUP_ANDX /
// TREE_CONNECT[_ANDX] / TREE_DISCONNECT / LOGOFF_ANDX / ECHO), re-expressed over
// the §9 share seam. These mirror the faithful wire formats of the legacy
// service/smb (command_core.go) — the byte layouts the field validated against
// Win9x/WfW clients — but bind tree connects to a *Share rather than a share
// index, and decode/encode with core/binaryprimitives little-endian codecs (the
// core ring forbids encoding/binary; §1 / archtest). ---

// NEGOTIATE response parameters (SMBLibrary-compatible defaults; see the legacy
// service/smb/server.go for the rationale these were tuned against Win9x). We
// deliberately do NOT advertise CAP_RAW_MODE / CAP_MPX_MODE (those transports are
// not implemented), so Win9x falls back to plain READ/WRITE/WRITE_ANDX.
const (
	negotiateSecurityModeShare = 0x00 // share-level, plaintext, no challenge
	negotiateSecurityModeUser  = 0x01 // SECURITY_MODE_USER_SECURITY, plaintext, no challenge

	// MaxMpxCount is the number of outstanding requests the CLIENT may keep in
	// flight ([MS-CIFS] 2.2.4.52.2) — it is a promise about client behavior, not
	// server concurrency, and we process pipelined requests in arrival order
	// regardless. Advertising 1 starves the NT redirector, which reserves mpx
	// slots internally (oplock breaks, echoes, transaction secondaries) and
	// fails operations CLIENT-SIDE with STATUS_INSUFFICIENT_RESOURCES (net view
	// → error 1450, with nothing on the wire) when no slot is free. Real
	// servers (NT, Samba) advertise 50.
	negotiateMaxMpxCount   = 50
	negotiateMaxNumberVcs  = 1                  // one virtual circuit per session
	negotiateMaxBufferSize = 0x4000             // 16 KiB per request
	negotiateMaxRawSize    = 0                  // raw mode disabled
	windowsFiletimeOffset  = 116444736000000000 // 100-ns intervals, 1601→1970 epoch

	capNTSMBs     = 0x00000010 // CAP_NT_SMBS
	capStatus32   = 0x00000040 // CAP_STATUS32 (server returns 32-bit NTSTATUS)
	capNTFind     = 0x00000200 // CAP_NT_FIND
	capLargeFiles = 0x00000008 // CAP_LARGE_FILES

	negotiateCapabilities = capNTSMBs | capStatus32 | capNTFind | capLargeFiles
)

// sessionGuestUID is the user id granted to every SESSION_SETUP_ANDX. This is a
// compatibility server: it does not authenticate, it grants a guest session (the
// honest security posture documented in the package doc).
const sessionGuestUID = 1

// responseHeader builds the reply SMB header from the request header: the same
// ids (TID/PID/UID/MID), the reply flag set, the given status, and the carried
// flags2 (with KNOWS_LONG_NAMES advertised, as the legacy server stamps).
func responseHeader(h protocol.Header, status uint32) protocol.Header {
	h.Flags |= protocol.FlagReply
	h.Flags2 |= protocol.Flags2KnowsLongNames
	h.Status = status
	return h
}

// DOS-form status words the wire carries for CORE/LANMAN clients that did NOT set
// SMB_FLAGS2_NT_STATUS. The header Status field for such clients is a
// {ErrorClass(1), reserved(1), ErrorCode(2 LE)} triple ([MS-CIFS] 2.2.3.1); packed
// little-endian a value 0x00CCcccc puts ErrorClass in byte 0 and ErrorCode in bytes
// 2-3. Class ERRDOS=0x01, ERRSRV=0x02 ([smb6.0] 4603-4604). These mirror the
// field-validated legacy service/smb table (server.go smbStatusErr*).
const (
	dosErrBadFunc     = 0x00010001 // ERRDOS/ERRbadfunc (code 1)
	dosErrBadFile     = 0x00020001 // ERRDOS/ERRbadfile (code 2)
	dosErrBadPath     = 0x00030001 // ERRDOS/ERRbadpath (code 3)
	dosErrNoAccess    = 0x00050001 // ERRDOS/ERRnoaccess (code 5)
	dosErrBadFid      = 0x00060001 // ERRDOS/ERRbadfid (code 6)
	dosErrNoFiles     = 0x00120001 // ERRDOS/ERRnofiles (code 18)
	dosErrInvNetName  = 0x00430001 // ERRDOS/ERRinvnetname (code 67)
	dosErrBadTID      = 0x00050002 // ERRSRV/ERRinvtid (code 5)
	dosErrBadPw       = 0x00020002 // ERRSRV/ERRbadpw (code 2) — [smb6.0] 4652
	dosErrSrvError    = 0x00010002 // ERRSRV/ERRerror (code 1) — generic server error
	dosErrUseStandard = 0x00FB0002 // ERRSRV/ERRuseSTD (code 251)
)

// toWireStatus maps an NTSTATUS to the value to put on the wire: the NTSTATUS
// itself when the request set SMB_FLAGS2_NT_STATUS, otherwise the equivalent DOS
// class/code (the form CORE-dialect clients expect). This mirrors the legacy
// service/smb toWireErrorStatus exactly — an unmapped NTSTATUS (high byte set) with
// the NT-status bit clear collapses to ERRSRV/ERRerror rather than leaking a raw
// NTSTATUS a CORE client would mis-read (the 0xC000006D → bogus class 0x6d symptom
// in captures/ipx.pcap).
func toWireStatus(reqFlags2 uint16, status uint32) uint32 {
	if reqFlags2&protocol.Flags2NTStatus != 0 {
		return status
	}
	switch status {
	case statusSuccess:
		return statusSuccess
	case statusSMBBadTID:
		return dosErrBadTID // already DOS-form
	case statusUseStandard:
		return dosErrUseStandard // already DOS-form
	case statusBadNetworkName:
		return dosErrInvNetName
	case statusAccessDenied, statusObjectNameCollision, statusDirectoryNotEmpty:
		return dosErrNoAccess
	case statusNotSupported:
		return dosErrBadFunc
	case statusObjectNameNotFound:
		return dosErrBadFile
	case statusObjectPathNotFound, statusObjectNameInvalid, statusFileIsADirectory, statusNotADirectory:
		return dosErrBadPath
	case statusInvalidHandle:
		return dosErrBadFid
	case statusNoMoreFiles:
		return dosErrNoFiles
	case statusLogonFailure:
		// [smb6.0] 4652: a bad name/password pair in a Tree Connect or Session
		// Setup is ERRSRV(class 2)/ERRbadpw(code 2). Without this a DOS-codes
		// client receives the raw NTSTATUS 0xC000006D whose low byte 0x6d it
		// decodes as a bogus error class ("unknown error class 0x6d").
		return dosErrBadPw
	default:
		// Any unmapped code: pass through already-DOS-form values (high byte
		// clear); collapse a raw NTSTATUS to a generic ERRSRV/ERRerror so a CORE
		// client never sees an NTSTATUS in its DOS-form Status field.
		if status&0xFF000000 == 0 {
			return status
		}
		return dosErrSrvError
	}
}

// buildErrorResponse builds a header-only SMB error reply (WCT=0, BCC=0) carrying
// the wire-form status for the request's flags2.
func buildErrorResponse(h protocol.Header, req []byte, status uint32) []byte {
	rh := responseHeader(h, toWireStatus(h.Flags2, status))
	out := rh.Encode(nil)
	out = append(out, 0)    // WordCount = 0
	out = append(out, 0, 0) // ByteCount = 0
	return out
}

// handleNegotiate answers SMB_COM_NEGOTIATE. It selects the most-recent dialect the
// client offered and replies in the wire format that dialect mandates — the response
// WordCount MUST match the selected dialect family ([MS-CIFS] 2.2.4.52.2): Core →
// WCT=1, LANMAN 1.0..2.1 / WfW 3.1a → WCT=13, NT LM 0.12 → WCT=17. Emitting the wrong
// word count for the selected dialect yields a malformed reply a client may reject.
//
// SecurityMode is plaintext, no challenge; share- vs user-level is chosen by
// securityMode() from whether a user store is wired. The response header echoes the
// request header (reply flag + SUCCESS status); it does NOT stamp
// SMB_FLAGS2_KNOWS_LONG_NAMES the way the generic responseHeader helper does —
// NEGOTIATE preserves the client's Flags2.
func (s *Service) handleNegotiate(sess *smbSession, h protocol.Header, req []byte) []byte {
	idx, name, family := protocol.SelectDialect(parseNegotiateDialects(req))

	// Record what this client negotiated on the session, so later behaviour and the
	// management view key off the session's negotiated version. An unmatched list
	// still records the (empty) outcome — the session is Core by default.
	sess.setNegotiated(name, int(family))

	switch family {
	case protocol.DialectFamilyNT:
		return s.buildNegotiateNT(h, idx)
	case protocol.DialectFamilyLanMan:
		return s.buildNegotiateLanMan(h, idx, name)
	case protocol.DialectFamilyUnknown:
		// None of the offered dialects is supported: core-shape reply with 0xFFFF.
		return buildNegotiateCore(h, 0xFFFF)
	default: // DialectFamilyCore
		return buildNegotiateCore(h, idx)
	}
}

// negotiateResponseHeader builds the NEGOTIATE reply header: the request header with
// the reply flag and SUCCESS status set, Flags2 preserved exactly as the client sent
// it. Mid/Pid/etc. are carried through. Unlike responseHeader it does NOT add
// SMB_FLAGS2_KNOWS_LONG_NAMES ([smb6.0]: the server must return the same Mid/Pid; the
// legacy server copies the request header verbatim for NEGOTIATE).
func negotiateResponseHeader(h protocol.Header) protocol.Header {
	h.Flags |= protocol.FlagReply
	h.Status = statusSuccess
	return h
}

// buildNegotiateCore emits the Core / "PC NETWORK PROGRAM 1.0" response
// ([MS-CIFS] 2.2.4.52.2; [smb6.0]): WCT=1 (DialectIndex only), ByteCount=0. Also used
// for the no-supported-dialect case (index 0xFFFF).
func buildNegotiateCore(h protocol.Header, dialectIdx uint16) []byte {
	out := negotiateResponseHeader(h).Encode(nil)
	out = append(out, 1) // WordCount = 1
	w := make([]byte, 2)
	bp.PutLE16(w[0:2], dialectIdx) // DialectIndex
	out = append(out, w...)
	out = append(out, 0, 0) // ByteCount = 0
	return out
}

// buildNegotiateLanMan emits the LANMAN 1.0..2.1 response ([MS-CIFS] 2.2.4.52.2;
// [smb6.0]): WCT=13. Note SecurityMode and MaxBufferSize are 16-bit here (they are
// 8-bit / 32-bit in the NT form), there is no Capabilities field, and the timestamp is
// the DOS SMB_TIME/SMB_DATE pair.
//
// The PrimaryDomain is included in the byte area ONLY when the negotiated dialect is
// DOS LANMAN2.1 or LANMAN2.1 ([smb6.0] 1127); for every earlier LANMAN-family dialect
// (MICROSOFT NETWORKS 3.0, LANMAN1.0, LM1.2X002, DOS LM1.2X002, Windows for Workgroups
// 3.1a) the byte area is empty (ByteCount=0). Appending it for those dialects yields
// trailing "Unknown Data" a client does not parse (observed in captures/ipx.pcap for a
// WfW 3.1a selection).
// securityMode picks the NEGOTIATE SecurityMode ([MS-CIFS] 2.2.4.52.2, bit 0).
// With no named users the server is SHARE-level (bit 0 clear): every share is
// guest-open, no account/password is wanted, and — decisively — the NT-family
// redirector refuses to use a USER-level server that offers no challenge (it
// will not send a plaintext password: netbeui.pcap frames 51–61 show NT 3.51
// answering such a NEGOTIATE response with Session End + DISC and reporting
// "access denied", without ever attempting SESSION_SETUP). With named users we
// advertise USER-level plaintext so Win9x/DOS clients send cleartext
// credentials; NT clients then need challenge/response we do not implement —
// see spec/errata.md.
//
// "Has named users" is read live off the wired Authenticator when it reports
// its user set (the built-in store's HasUsers — the compose root wires the
// store even when it is empty, so wiring alone is not the signal). An
// authenticator that cannot report is taken as user-level, the conservative
// reading.
func (s *Service) securityMode() byte {
	s.mu.Lock()
	authn := s.auth
	s.mu.Unlock()
	if !storeHasUsers(authn) {
		return negotiateSecurityModeShare
	}
	return negotiateSecurityModeUser
}

// storeHasUsers reports whether the wired Authenticator currently holds any
// named users. nil (no store) is false; a store that exposes HasUsers (the
// built-in adapter/auth/local store — the compose root wires it even when
// empty, so wiring alone is not the signal) is asked live; an authenticator
// that cannot report is taken as populated, the conservative reading. Both the
// NEGOTIATE security posture and SESSION_SETUP validation key off this: with
// no named users the server is share-level and every credential is accepted
// as-is (guest), never challenged or failed.
func storeHasUsers(authn Authenticator) bool {
	if authn == nil {
		return false
	}
	if r, ok := authn.(interface{ HasUsers() bool }); ok {
		return r.HasUsers()
	}
	return true
}

func (s *Service) buildNegotiateLanMan(h protocol.Header, dialectIdx uint16, dialect string) []byte {
	out := negotiateResponseHeader(h).Encode(nil)
	out = append(out, 13) // WordCount = 13

	w := make([]byte, 26)                              // 13 words
	bp.PutLE16(w[0:2], dialectIdx)                     // DialectIndex
	bp.PutLE16(w[2:4], uint16(s.securityMode()))       // SecurityMode (16-bit)
	bp.PutLE16(w[4:6], uint16(negotiateMaxBufferSize)) // MaxBufferSize (16-bit)
	bp.PutLE16(w[6:8], negotiateMaxMpxCount)           // MaxMpxCount
	bp.PutLE16(w[8:10], negotiateMaxNumberVcs)         // MaxNumberVcs
	bp.PutLE16(w[10:12], uint16(negotiateMaxRawSize))  // RawMode
	bp.PutLE32(w[12:16], 0)                            // SessionKey
	tm, dt := smbServerTimeDate(time.Now().UTC())
	bp.PutLE16(w[16:18], tm) // ServerTime (SMB_TIME)
	bp.PutLE16(w[18:20], dt) // ServerDate (SMB_DATE)
	bp.PutLE16(w[20:22], 0)  // ServerTimeZone
	bp.PutLE16(w[22:24], 0)  // EncryptionKeyLength = 0 (plaintext, no challenge)
	bp.PutLE16(w[24:26], 0)  // Reserved (MBZ)
	out = append(out, w...)

	// Byte area: EncryptionKey (empty) + PrimaryDomain (only for LANMAN2.1 dialects).
	var area []byte
	if dialect == protocol.DialectDOSLANMAN2 || dialect == protocol.DialectLANMAN21 {
		area = append([]byte(normalizeName(s.workgroup())), 0)
	}
	bcc := make([]byte, 2)
	bp.PutLE16(bcc, uint16(len(area)))
	out = append(out, bcc...)
	out = append(out, area...)
	return out
}

// buildNegotiateNT emits the NT LM 0.12 response ([MS-CIFS] 2.2.4.52.2; [smb6.0]):
// WCT=17, 8-bit SecurityMode, 32-bit MaxBufferSize, a Capabilities field, and a 64-bit
// FILETIME. ByteArea = Challenge (none) + DomainName.
func (s *Service) buildNegotiateNT(h protocol.Header, dialectIdx uint16) []byte {
	domain := normalizeName(s.workgroup())
	domainBytes := append([]byte(domain), 0)

	out := negotiateResponseHeader(h).Encode(nil)
	out = append(out, 17) // WordCount = 17

	w := make([]byte, 34)          // 17 words
	bp.PutLE16(w[0:2], dialectIdx) // DialectIndex
	w[2] = s.securityMode()        // SecurityMode (8-bit)
	bp.PutLE16(w[3:5], negotiateMaxMpxCount)
	bp.PutLE16(w[5:7], negotiateMaxNumberVcs)
	bp.PutLE32(w[7:11], negotiateMaxBufferSize) // MaxBufferSize (32-bit)
	bp.PutLE32(w[11:15], negotiateMaxRawSize)
	bp.PutLE32(w[15:19], 0)                     // SessionKey
	bp.PutLE32(w[19:23], negotiateCapabilities) // Capabilities
	ft := uint64(time.Now().UTC().UnixNano()/100) + windowsFiletimeOffset
	bp.PutLE32(w[23:27], uint32(ft))     // SystemTimeLow
	bp.PutLE32(w[27:31], uint32(ft>>32)) // SystemTimeHigh
	bp.PutLE16(w[31:33], 0)              // ServerTimeZone
	w[33] = 0                            // ChallengeLength = 0 (no challenge)
	out = append(out, w...)

	bcc := make([]byte, 2)
	bp.PutLE16(bcc, uint16(len(domainBytes)))
	out = append(out, bcc...)
	out = append(out, domainBytes...) // Challenge (empty) + DomainName
	return out
}

// smbServerTimeDate packs a UTC time into the DOS SMB_TIME / SMB_DATE 16-bit fields the
// LANMAN NEGOTIATE response carries ([MS-DTYP] SMB_DATE/SMB_TIME): SMB_TIME =
// seconds/2(0-4) | minutes(5-10) | hours(11-15); SMB_DATE = day(0-4) | month(5-8) |
// (year-1980)(9-15).
func smbServerTimeDate(t time.Time) (smbTime, smbDate uint16) {
	smbTime = uint16(t.Second()/2) | uint16(t.Minute())<<5 | uint16(t.Hour())<<11
	smbDate = uint16(t.Day()) | uint16(t.Month())<<5 | uint16(t.Year()-1980)<<9
	return smbTime, smbDate
}

// statusLogonFailure is STATUS_LOGON_FAILURE — the named account/password did not
// validate against the wired user store.
const statusLogonFailure uint32 = 0xC000006D

// handleSessionSetup answers SMB_COM_SESSION_SETUP_ANDX. It grants a guest session
// (UID=1, Action=0x0001) unless the wired store HAS named users (storeHasUsers) and
// the client presented a NON-EMPTY cleartext password for a named account, in which
// case it validates the pair against the store: success grants a named, non-guest
// session (Action=0x0000); failure returns STATUS_LOGON_FAILURE.
//
// A credential-less setup (empty password) is ALWAYS granted as guest, even with a
// store wired: [smb6.0] 289-291 requires a user-level server to admit a client that
// sends no password (the "implicit user logon" path), and the legacy service always
// did so. Authenticating an empty password and returning STATUS_LOGON_FAILURE was the
// captures/ipx.pcap regression (WIN98USER, ANSI+Unicode PasswordLength 0 → frame 111).
//
// We can only validate a CLEARTEXT password — a legacy client sending an LM/NTLM
// hash cannot be reversed, so a hashed credential is accepted AS GUEST (it still
// only sees guest-open shares). See spec/errata.md "SMB hashed-credential
// accept-as-guest". WCT=3: AndXCommand/AndXReserved/AndXOffset + Action; the byte area
// carries NativeOS / NativeLanMan (server name) + PrimaryDomain (workgroup).
func (s *Service) handleSessionSetup(sess *smbSession, h protocol.Header, req []byte) []byte {
	user, pass, hashed := parseSessionSetup(req, h.Flags2)

	s.mu.Lock()
	authn := s.auth
	s.mu.Unlock()

	action := uint16(0x0001) // guest logon by default
	identity := ""
	// Only authenticate when the client actually presented a credential: a named
	// account WITH a non-empty cleartext password. An empty password is the
	// credential-less guest path ([smb6.0] 289), never a failed authentication.
	// And only when the store actually HAS named users: with an empty store the
	// server advertised SHARE-level security, no account can possibly match, and
	// clients that volunteer their logged-on identity anyway (OS/2 LAN Manager
	// sends user+password with every SESSION_SETUP; netbeui.pcap frame 31) must
	// be accepted as guests, not failed with STATUS_LOGON_FAILURE.
	if storeHasUsers(authn) && user != "" && pass != "" && !hashed {
		ok, err := authn.Authenticate(user, pass)
		if err != nil {
			s.logf("SESSION_SETUP authenticate error")
			return errResponse(h, toWireStatus(h.Flags2, statusLogonFailure))
		}
		if !ok {
			return errResponse(h, toWireStatus(h.Flags2, statusLogonFailure))
		}
		identity = user
		action = 0x0000 // non-guest logon
	}

	sess.mu.Lock()
	sess.uid = sessionGuestUID
	sess.user = identity
	sess.mu.Unlock()

	h.UID = sessionGuestUID
	rh := responseHeader(h, statusSuccess)
	out := rh.Encode(nil)
	out = append(out, 3) // WordCount

	w := make([]byte, 6)
	w[0] = protocol.CommandNoAndXCommand // AndXCommand = no chaining
	w[1] = 0x00                          // AndXReserved
	bp.PutLE16(w[2:4], 0)                // AndXOffset
	bp.PutLE16(w[4:6], action)           // Action (0=user, 1=guest)
	out = append(out, w...)

	// Byte area: NativeOS, NativeLanMan, PrimaryDomain. Win9x/WfW expect the
	// server identity here; two bare NULs (the earlier stub) left NativeLanMan
	// blank, which some clients log as an anonymous server.
	area := sessionSetupTrailer(s.serverName(), s.workgroup(), h.Flags2&protocol.Flags2Unicode != 0)
	bcc := make([]byte, 2)
	bp.PutLE16(bcc, uint16(len(area)))
	out = append(out, bcc...)
	out = append(out, area...)
	return out
}

// sessionSetupTrailer builds the SESSION_SETUP_ANDX response byte area: the
// NUL-terminated NativeOS, NativeLanMan and PrimaryDomain strings. We report the
// server name for both NativeOS and NativeLanMan (a compatibility server has no
// real OS/LANMAN version to advertise) and the workgroup as PrimaryDomain. When the
// client negotiated Unicode the strings are UTF-16LE and a single pad byte precedes
// them so the 16-bit strings start word-aligned within the SMB.
func sessionSetupTrailer(serverName, workgroup string, unicode bool) []byte {
	fields := []string{serverName, serverName, workgroup}
	if !unicode {
		var out []byte
		for _, f := range fields {
			out = append(out, []byte(f)...)
			out = append(out, 0)
		}
		return out
	}
	out := []byte{0x00} // pad byte to word-align the UTF-16LE strings
	for _, f := range fields {
		for _, r := range f {
			out = append(out, byte(r), byte(r>>8))
		}
		out = append(out, 0x00, 0x00)
	}
	return out
}

// parseSessionSetup extracts the AccountName and cleartext password from a
// SESSION_SETUP_ANDX request. It handles the NT LM 0.12 variant (WCT=13: two
// password-length words locate the byte-area layout) and the older LM variant
// (WCT=10: one password-length word). hashed reports that the supplied password
// is a binary LM/NTLM hash (length != the cleartext we can validate, or the
// case-sensitive response is present) rather than a cleartext string — in that
// case the caller falls back to a guest grant. A frame we cannot parse yields an
// empty user (guest).
func parseSessionSetup(req []byte, flags2 uint16) (user, pass string, hashed bool) {
	words, area, ok := reqBody(req)
	if !ok {
		return "", "", false
	}
	unicode := flags2&protocol.Flags2Unicode != 0

	switch {
	case len(words) >= 26: // WCT>=13: NT LM 0.12
		ciPwLen := int(bp.LE16(words[14:16])) // CaseInsensitivePasswordLength
		csPwLen := int(bp.LE16(words[16:18])) // CaseSensitivePasswordLength
		// A case-sensitive (NTLM) response, or a case-insensitive blob longer than a
		// plausible cleartext string with a trailing NUL, is a hash we cannot reverse.
		if csPwLen > 0 {
			hashed = true
		}
		off := ciPwLen + csPwLen
		if off > len(area) {
			return "", "", hashed
		}
		// AccountName is the first string after the two password blobs.
		name, _ := readWireString(area[off:], unicode)
		if !hashed && ciPwLen > 0 {
			// The case-insensitive field is the cleartext (or LM hash). 24 bytes is
			// the LM/NTLM response size — treat that as a hash, shorter as cleartext.
			if ciPwLen == 24 {
				hashed = true
			} else {
				pass = strings.TrimRight(string(area[:ciPwLen]), "\x00")
			}
		}
		return strings.TrimRight(name, "\x00"), pass, hashed
	case len(words) >= 20: // WCT=10: LM 1.0/2.0 (single password length)
		pwLen := int(bp.LE16(words[14:16]))
		if pwLen > len(area) {
			return "", "", false
		}
		if pwLen == 24 {
			hashed = true
		} else if pwLen > 0 {
			pass = strings.TrimRight(string(area[:pwLen]), "\x00")
		}
		name, _ := readWireString(area[pwLen:], unicode)
		return strings.TrimRight(name, "\x00"), pass, hashed
	default:
		return "", "", false
	}
}

// readWireString reads one NUL-terminated string from b in the wire charset
// (UTF-16LE when the Unicode flag is set, else OEM/ANSI bytes), returning the
// decoded string and the number of raw bytes consumed including the terminator.
func readWireString(b []byte, unicode bool) (string, int) {
	if unicode {
		// 2-byte alignment is the caller's concern; here decode UTF-16LE to the
		// first 0x0000 unit. Non-ASCII is rare for an account name; take the low
		// byte (matches the OEM behaviour for ASCII account names).
		var sb strings.Builder
		i := 0
		for i+1 < len(b) {
			lo, hi := b[i], b[i+1]
			if lo == 0 && hi == 0 {
				i += 2
				break
			}
			sb.WriteByte(lo) // ASCII account names: low byte is the character
			i += 2
		}
		return sb.String(), i
	}
	for i := 0; i < len(b); i++ {
		if b[i] == 0 {
			return string(b[:i]), i + 1
		}
	}
	return string(b), len(b)
}

// handleTreeConnectAndX answers SMB_COM_TREE_CONNECT_ANDX (0x75). It resolves the
// share name from the request, binds a TID to the matching *Share (or the virtual
// IPC$ pipe tree), and returns the AndX response (WCT=3) carrying the service
// string ("A:" for a disk share, "IPC" for the pipe).
func (s *Service) handleTreeConnectAndX(sess *smbSession, h protocol.Header, req []byte) []byte {
	name, ok := parseTreeConnectShareName(req)
	if !ok {
		return buildErrorResponse(h, req, statusBadNetworkName)
	}

	if strings.EqualFold(name, ipcShareName) {
		tid := sess.allocTID(&treeConnect{ipc: true})
		return buildTreeConnectAndXResponse(h, tid, "IPC")
	}

	sh, found := s.ShareByName(name)
	if !found || !sh.allows(sess.user) {
		// A share the session identity may not access is reported as if it does not
		// exist (BAD_NETWORK_NAME), so naming a restricted share directly is refused
		// without leaking its existence — matching the enumeration that omitted it.
		return buildErrorResponse(h, req, statusBadNetworkName)
	}
	tid := sess.allocTID(&treeConnect{share: sh})
	return buildTreeConnectAndXResponse(h, tid, "A:")
}

// handleTreeConnect answers the original SMB_COM_TREE_CONNECT (0x70) used by WfW
// 3.11 / CORE-dialect clients: WCT=2 reply (MaxBufferSize, TID), BCC=0. The
// share-resolution logic is identical to the AndX variant.
func (s *Service) handleTreeConnect(sess *smbSession, h protocol.Header, req []byte) []byte {
	name, ok := parseTreeConnectShareName(req)
	if !ok {
		return buildErrorResponse(h, req, statusBadNetworkName)
	}

	var tc *treeConnect
	if strings.EqualFold(name, ipcShareName) {
		tc = &treeConnect{ipc: true}
	} else {
		sh, found := s.ShareByName(name)
		if !found || !sh.allows(sess.user) {
			return buildErrorResponse(h, req, statusBadNetworkName)
		}
		tc = &treeConnect{share: sh}
	}
	tid := sess.allocTID(tc)

	h.TID = tid
	rh := responseHeader(h, statusSuccess)
	out := rh.Encode(nil)
	out = append(out, 2) // WordCount
	w := make([]byte, 4)
	bp.PutLE16(w[0:2], negotiateMaxBufferSize)
	bp.PutLE16(w[2:4], tid)
	out = append(out, w...)
	out = append(out, 0, 0) // ByteCount = 0
	return out
}

// buildTreeConnectAndXResponse builds the TREE_CONNECT_ANDX success reply (WCT=3):
// AndXCommand/AndXReserved/AndXOffset + OptionalSupport, then a ByteCount-prefixed
// service string ("A:\0" / "IPC\0") and an empty NativeFileSystem.
func buildTreeConnectAndXResponse(h protocol.Header, tid uint16, service string) []byte {
	h.TID = tid
	rh := responseHeader(h, statusSuccess)
	out := rh.Encode(nil)
	out = append(out, 3) // WordCount

	w := make([]byte, 6)
	w[0] = protocol.CommandNoAndXCommand // AndXCommand = no chaining
	w[1] = 0x00
	bp.PutLE16(w[2:4], 0) // AndXOffset
	bp.PutLE16(w[4:6], 0) // OptionalSupport
	out = append(out, w...)

	svc := append([]byte(service), 0)
	nativeFS := []byte{0}
	bcc := make([]byte, 2)
	bp.PutLE16(bcc, uint16(len(svc)+len(nativeFS)))
	out = append(out, bcc...)
	out = append(out, svc...)
	out = append(out, nativeFS...)
	return out
}

// handleTreeDisconnect releases the request's TID (SMB_COM_TREE_DISCONNECT) and
// returns a header-only success (WCT=0, BCC=0).
func (s *Service) handleTreeDisconnect(sess *smbSession, h protocol.Header, req []byte) []byte {
	sess.dropTree(h.TID)
	rh := responseHeader(h, statusSuccess)
	out := rh.Encode(nil)
	out = append(out, 0)    // WordCount
	out = append(out, 0, 0) // ByteCount
	return out
}

// handleLogoff answers SMB_COM_LOGOFF_ANDX (WCT=2: AndXCommand/AndXReserved/
// AndXOffset) by clearing the granted UID. The session may re-setup afterwards.
func (s *Service) handleLogoff(sess *smbSession, h protocol.Header, req []byte) []byte {
	sess.mu.Lock()
	sess.uid = 0
	sess.mu.Unlock()

	rh := responseHeader(h, statusSuccess)
	out := rh.Encode(nil)
	out = append(out, 2) // WordCount
	w := make([]byte, 4)
	w[0] = protocol.CommandNoAndXCommand
	w[1] = 0x00
	bp.PutLE16(w[2:4], 0) // AndXOffset
	out = append(out, w...)
	out = append(out, 0, 0) // ByteCount
	return out
}

// handleEcho answers SMB_COM_ECHO by mirroring the request data with
// SequenceNumber=1 (WCT=1). A malformed echo is dropped (nil).
func (s *Service) handleEcho(h protocol.Header, req []byte) []byte {
	body := req[protocol.HeaderLen:]
	if len(body) < 1 {
		return nil
	}
	wct := int(body[0])
	// ECHO request: WCT=1 (EchoCount) + ByteCount + data.
	if wct < 1 || len(body) < 1+2*wct+2 {
		return nil
	}
	bccOff := 1 + 2*wct
	bcc := int(bp.LE16(body[bccOff : bccOff+2]))
	dataOff := bccOff + 2
	if len(body) < dataOff+bcc {
		return nil
	}
	data := body[dataOff : dataOff+bcc]

	rh := responseHeader(h, statusSuccess)
	out := rh.Encode(nil)
	out = append(out, 1) // WordCount
	w := make([]byte, 2)
	bp.PutLE16(w, 1) // SequenceNumber = 1
	out = append(out, w...)
	bccOut := make([]byte, 2)
	bp.PutLE16(bccOut, uint16(len(data)))
	out = append(out, bccOut...)
	out = append(out, data...)
	return out
}

// --- request parsing helpers ---

// parseNegotiateDialects returns the ordered list of dialect strings offered in a
// NEGOTIATE request byte area. Each entry is a 0x02 buffer-format byte followed by a
// NUL-terminated ASCII string ([MS-CIFS] 2.2.4.52.1). The returned slice preserves the
// request order so its indices are the DialectIndex values the response selects from.
func parseNegotiateDialects(req []byte) []string {
	if len(req) < protocol.HeaderLen+3 {
		return nil
	}
	bcc := int(bp.LE16(req[protocol.HeaderLen+1 : protocol.HeaderLen+3]))
	start := protocol.HeaderLen + 3
	if len(req) < start+bcc {
		return nil
	}
	rest := req[start : start+bcc]
	var out []string
	for len(rest) >= 2 {
		if rest[0] != 0x02 {
			break
		}
		rest = rest[1:]
		nul := indexByte(rest, 0)
		if nul < 0 {
			break
		}
		out = append(out, string(rest[:nul]))
		rest = rest[nul+1:]
	}
	return out
}

// parseTreeConnectShareName extracts the share leaf from a TREE_CONNECT[_ANDX]
// request's byte area: it scans the NUL-separated strings for a UNC path
// (\\server\share[\...]) and returns the share segment. A 0x04 ASCII
// buffer-format prefix (CORE TREE_CONNECT) is stripped; the AndX variant places
// the path raw.
func parseTreeConnectShareName(req []byte) (string, bool) {
	if len(req) < protocol.HeaderLen+1 {
		return "", false
	}
	wct := int(req[protocol.HeaderLen])
	bccOff := protocol.HeaderLen + 1 + 2*wct
	if len(req) < bccOff+2 {
		return "", false
	}
	bcc := int(bp.LE16(req[bccOff : bccOff+2]))
	dataOff := bccOff + 2
	if len(req) < dataOff+bcc {
		return "", false
	}
	area := req[dataOff : dataOff+bcc]

	for _, part := range splitNULStrings(area) {
		if len(part) > 0 && part[0] == 0x04 {
			part = part[1:]
		}
		p := strings.TrimSpace(part)
		if p == "" || !strings.Contains(p, "\\") {
			continue
		}
		trimmed := strings.TrimLeft(p, "\\")
		segments := strings.Split(trimmed, "\\")
		if len(segments) >= 2 && segments[1] != "" {
			return segments[1], true
		}
	}
	return "", false
}

// splitNULStrings splits a byte area on NUL bytes into UTF-8 strings, dropping
// empties. (TREE_CONNECT names are ASCII/OEM; a Unicode-flagged path would need
// UTF-16 splitting, which a later FS slice handles for file paths.)
func splitNULStrings(area []byte) []string {
	var out []string
	start := 0
	for i := range area {
		if area[i] == 0 {
			if i > start {
				out = append(out, string(area[start:i]))
			}
			start = i + 1
		}
	}
	if start < len(area) {
		out = append(out, string(area[start:]))
	}
	return out
}

// indexByte returns the index of c in b, or -1 (avoids importing bytes for one
// call in the core ring).
func indexByte(b []byte, c byte) int {
	for i := range b {
		if b[i] == c {
			return i
		}
	}
	return -1
}

// normalizeName upper-cases and trims a NetBIOS/share name to ≤15 bytes, matching
// the legacy normalizeBrowserName so share lookups are case-insensitive.
func normalizeName(name string) string {
	upper := strings.ToUpper(strings.TrimSpace(name))
	if len(upper) > 15 {
		upper = upper[:15]
	}
	return upper
}

// workgroup returns the configured workgroup/domain the NEGOTIATE response
// advertises, defaulting to WORKGROUP when unset.
func (s *Service) workgroup() string {
	s.mu.Lock()
	wg := s.wg
	s.mu.Unlock()
	if wg != "" {
		return wg
	}
	return "WORKGROUP"
}
