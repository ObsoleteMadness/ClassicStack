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
	negotiateSecurityMode  = 0x01               // SECURITY_MODE_USER_SECURITY, no challenge
	negotiateMaxMpxCount   = 1                  // single-request server
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

// toWireStatus maps an NTSTATUS to the value to put on the wire: the NTSTATUS
// itself when the request set SMB_FLAGS2_NT_STATUS, otherwise the equivalent DOS
// class/code (the form CORE-dialect clients expect). Only the spine's statuses
// are mapped; the FS engine extends this in its slice.
func toWireStatus(reqFlags2 uint16, status uint32) uint32 {
	if reqFlags2&protocol.Flags2NTStatus != 0 {
		return status
	}
	switch status {
	case statusSuccess:
		return statusSuccess
	case statusBadNetworkName:
		return 0x00060001 // ERRSRV/ERRinvnetname
	case statusAccessDenied:
		return 0x00050001 // ERRSRV/ERRaccess
	case statusNotSupported:
		return 0x00010001 // ERRDOS/ERRbadfunc
	case statusSMBBadTID:
		return 0x00050002 // ERRSRV/ERRinvtid (already DOS-form)
	case statusObjectNameNotFound, statusObjectPathNotFound:
		return 0x00020001 // ERRDOS/ERRbadfile
	case statusObjectNameCollision:
		return 0x00050001 // ERRSRV/ERRaccess (file exists → access-denied for CORE clients)
	case statusObjectNameInvalid:
		return 0x0002000C // ERRDOS/ERRbadpath
	case statusFileIsADirectory, statusNotADirectory, statusDirectoryNotEmpty:
		return 0x00050001 // ERRSRV/ERRaccess
	case statusInvalidHandle:
		return 0x00010006 // ERRDOS/ERRbadfid
	case statusNoMoreFiles:
		return 0x00010012 // ERRDOS/ERRnofiles
	case statusUnsuccessful:
		return 0x00010001 // ERRDOS/ERRbadfunc
	default:
		return status
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

// handleNegotiate answers SMB_COM_NEGOTIATE, accepting the NT LM 0.12 dialect
// (WCT=17). SecurityMode is user-level with no challenge so a client may send
// plain-text credentials we accept as a guest session. The response advertises
// the conservative capability/buffer set tuned for vintage clients.
func (s *Service) handleNegotiate(h protocol.Header, req []byte) []byte {
	dialectIdx := findNegotiateDialect(req, protocol.DialectNTLM)
	if dialectIdx < 0 {
		dialectIdx = 0
	}
	domain := normalizeName(s.workgroup())
	domainBytes := append([]byte(domain), 0)

	rh := responseHeader(h, statusSuccess)
	out := rh.Encode(nil)
	out = append(out, 17) // WordCount

	w := make([]byte, 34) // 17 words
	bp.PutLE16(w[0:2], uint16(dialectIdx))
	w[2] = negotiateSecurityMode
	bp.PutLE16(w[3:5], negotiateMaxMpxCount)
	bp.PutLE16(w[5:7], negotiateMaxNumberVcs)
	bp.PutLE32(w[7:11], negotiateMaxBufferSize)
	bp.PutLE32(w[11:15], negotiateMaxRawSize)
	bp.PutLE32(w[15:19], 0) // SessionKey
	bp.PutLE32(w[19:23], negotiateCapabilities)
	ft := uint64(time.Now().UTC().UnixNano()/100) + windowsFiletimeOffset
	bp.PutLE32(w[23:27], uint32(ft))     // SystemTimeLow
	bp.PutLE32(w[27:31], uint32(ft>>32)) // SystemTimeHigh
	bp.PutLE16(w[31:33], 0)              // ServerTimeZone
	w[33] = 0                            // EncryptionKeyLength = 0 (no challenge)
	out = append(out, w...)

	bcc := make([]byte, 2)
	bp.PutLE16(bcc, uint16(len(domainBytes)))
	out = append(out, bcc...)
	out = append(out, domainBytes...)
	return out
}

// handleSessionSetup answers SMB_COM_SESSION_SETUP_ANDX by granting a guest
// session (UID=1, Action=0x0001 guest logon). WCT=3: AndXCommand/AndXReserved/
// AndXOffset + Action; ByteCount=2 (empty NativeOS/NativeLM).
func (s *Service) handleSessionSetup(sess *smbSession, h protocol.Header, req []byte) []byte {
	sess.mu.Lock()
	sess.uid = sessionGuestUID
	sess.mu.Unlock()

	h.UID = sessionGuestUID
	rh := responseHeader(h, statusSuccess)
	out := rh.Encode(nil)
	out = append(out, 3) // WordCount

	w := make([]byte, 6)
	w[0] = protocol.CommandNoAndXCommand // AndXCommand = no chaining
	w[1] = 0x00                          // AndXReserved
	bp.PutLE16(w[2:4], 0)                // AndXOffset
	bp.PutLE16(w[4:6], 0x0001)           // Action = guest logon
	out = append(out, w...)

	out = append(out, 2, 0)       // ByteCount = 2
	out = append(out, 0x00, 0x00) // NativeOS="" NativeLM="" (two NULs)
	return out
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
	if !found {
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
		if !found {
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

// findNegotiateDialect returns the 0-based index of the named dialect in a
// NEGOTIATE request's dialect list, or -1 if absent. Each dialect is a 0x02
// buffer-format byte then a NUL-terminated ASCII string.
func findNegotiateDialect(req []byte, name string) int {
	if len(req) < protocol.HeaderLen+3 {
		return -1
	}
	bcc := int(bp.LE16(req[protocol.HeaderLen+1 : protocol.HeaderLen+3]))
	start := protocol.HeaderLen + 3
	if len(req) < start+bcc {
		return -1
	}
	rest := req[start : start+bcc]
	idx := 0
	for len(rest) >= 2 {
		if rest[0] != 0x02 {
			break
		}
		rest = rest[1:]
		nul := indexByte(rest, 0)
		if nul < 0 {
			break
		}
		if string(rest[:nul]) == name {
			return idx
		}
		rest = rest[nul+1:]
		idx++
	}
	return -1
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
