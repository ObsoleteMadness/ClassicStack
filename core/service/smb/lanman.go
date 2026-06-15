package smb

import (
	"strings"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"

	protocol "github.com/ObsoleteMadness/ClassicStack/core/protocol/smb"
)

// lanman.go is the SMB RAP layer over the IPC$ \PIPE\LANMAN named pipe (inside an
// SMB_COM_TRANSACTION). Two calls are served:
//
//   - NetServerEnum2 ("get server list") — the browse list. SMB asks the
//     datagram-layer browser service through the BrowseProvider seam (§3-ter, the
//     ONE place the SMB session layer meets the browser); SMB holds no browser/
//     election logic and the browser holds no SMB logic.
//   - NetShareEnum ("get share list") — this server's own shares (every bound disk
//     share + the virtual IPC$ pipe), answered straight from SMB state with no
//     browser involved.

// RAP function codes ([MS-RAP]): NetShareEnum lists this server's shares,
// NetServerEnum2 lists the servers the browser has observed.
const (
	rapNetShareEnum   uint16 = 0x0000
	rapNetServerEnum2 uint16 = 0x0068
)

// SHARE_INFO_1 share-type bits ([MS-SRVS] STYPE_*): a disk tree vs the IPC$ pipe.
const (
	shareTypeDisktree uint16 = 0x0000
	shareTypeIPC      uint16 = 0x0003
)

// RAP status codes ([MS-ERREF] Win32) returned in the 2-byte RAP Status param.
const (
	rapStatusInvalidFunction uint16 = 1  // ERROR_INVALID_FUNCTION
	rapStatusReqNotAccepted  uint16 = 71 // ERROR_REQ_NOT_ACCEP (a potential browser)
)

// SV_TYPE_* bits NetServerEnum2 filters on ([MS-SRVS]).
const (
	svTypeDomainEnum uint32 = 0x80000000
)

const lanmanPipe = "\\PIPE\\LANMAN"

// BrowseServer is one browse-list row the BrowseProvider supplies: a server name,
// its SV_TYPE_* bits, and an optional comment. It mirrors browser.ServerEntry so
// SMB depends on this small local type, not the browser package.
type BrowseServer struct {
	Name    string
	Type    uint32
	Comment string
}

// BrowseProvider is the read-only browse-list source the IPC$ NetServerEnum2
// handler consumes (the browser service satisfies it via an adapter). Available
// reports whether the browser can serve a list (false → a potential browser, which
// must answer ERROR_REQ_NOT_ACCEP); ServerEntries is the current browse list.
type BrowseProvider interface {
	Available() bool
	ServerEntries() []BrowseServer
}

// SetBrowseProvider installs the browse-list source (the browser service). Compose
// calls it during wiring; with none installed, NetServerEnum2 answers an empty
// success (no browser running → no list, but the pipe still responds cleanly).
func (s *Service) SetBrowseProvider(p BrowseProvider) {
	s.mu.Lock()
	s.browser = p
	s.mu.Unlock()
}

// browseProvider returns the installed provider under the service lock.
func (s *Service) browseProvider() BrowseProvider {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.browser
}

// handleTransaction answers SMB_COM_TRANSACTION. Only the IPC$ \PIPE\LANMAN RAP
// calls are served — NetServerEnum2 (browse list, from the browser) and
// NetShareEnum (share list, from our own shares); any other transaction — or a
// TRANSACTION on a non-IPC$ tree — answers STATUS_NOT_SUPPORTED so the client gets
// a definite reply rather than a drop.
func (s *Service) handleTransaction(sess *smbSession, h protocol.Header, req []byte) []byte {
	tc, ok := sess.tree(h.TID)
	if !ok {
		return errResponse(h, statusSMBBadTID)
	}
	if !tc.ipc {
		return errResponse(h, statusNotSupported) // RAP rides the IPC$ pipe only
	}
	area, ok := transactionBytes(req)
	if !ok {
		return errResponse(h, statusNotSupported)
	}
	fn, ok := parseLANMANFunction(area)
	if !ok {
		return errResponse(h, statusNotSupported)
	}
	switch fn {
	case rapNetServerEnum2:
		return s.handleNetServerEnum2(h, area)
	case rapNetShareEnum:
		return s.handleNetShareEnum(h, sess.user)
	}
	return errResponse(h, statusNotSupported)
}

// transactionBytes returns the SMB_COM_TRANSACTION byte (data) area, regardless of
// the request word-count shape.
func transactionBytes(req []byte) ([]byte, bool) {
	if len(req) < protocol.HeaderLen+1 {
		return nil, false
	}
	wct := int(req[protocol.HeaderLen])
	bccOff := protocol.HeaderLen + 1 + 2*wct
	if bccOff+2 > len(req) {
		return nil, false
	}
	bcc := int(bp.LE16(req[bccOff : bccOff+2]))
	start := bccOff + 2
	if start+bcc > len(req) {
		return nil, false
	}
	return req[start : start+bcc], true
}

// parseLANMANFunction finds the \PIPE\LANMAN marker in the transaction byte area
// and returns the RAP function code that follows its NUL terminator.
func parseLANMANFunction(area []byte) (uint16, bool) {
	marker := lanmanPipe + "\x00"
	idx := indexFold(area, marker)
	if idx < 0 {
		return 0, false
	}
	p := idx + len(marker)
	if p+2 > len(area) {
		return 0, false
	}
	return bp.LE16(area[p : p+2]), true
}

// netServerEnum2Filter best-effort extracts the SV_TYPE_* filter from the RAP
// NetServerEnum2 request (after the function code: ParamDesc + DataDesc strings,
// ReceiveBufferLength, then ServerType). A parse miss returns 0 (no filter).
func netServerEnum2Filter(area []byte) uint32 {
	marker := lanmanPipe + "\x00"
	idx := indexFold(area, marker)
	if idx < 0 {
		return 0
	}
	p := idx + len(marker) + 2 // past the function code
	for range 2 {              // skip ParamDesc + DataDesc (NUL-terminated)
		n := indexByte(area[p:], 0)
		if n < 0 {
			return 0
		}
		p += n + 1
	}
	if p+2+4 > len(area) {
		return 0
	}
	p += 2 // ReceiveBufferLength
	return bp.LE32(area[p : p+4])
}

// handleNetServerEnum2 answers a RAP NetServerEnum2 from the browse list. A
// potential browser (no list available) answers ERROR_REQ_NOT_ACCEP; a
// DOMAIN_ENUM-mixed-with-other-bits request answers ERROR_INVALID_FUNCTION; a plain
// server-list request returns the browse-list entries.
func (s *Service) handleNetServerEnum2(h protocol.Header, area []byte) []byte {
	provider := s.browseProvider()
	if provider == nil {
		// No browser wired — answer an empty success so the client does not hang.
		return buildNetServerEnum2Response(h, nil)
	}
	if !provider.Available() {
		return buildRAPError(h, rapStatusReqNotAccepted)
	}
	serverType := netServerEnum2Filter(area)
	if serverType&svTypeDomainEnum != 0 && serverType != svTypeDomainEnum {
		return buildRAPError(h, rapStatusInvalidFunction)
	}
	if serverType&svTypeDomainEnum != 0 {
		// DOMAIN_ENUM alone: report our own workgroup as the one domain.
		return buildNetServerEnum2Response(h, []BrowseServer{{Name: s.workgroup(), Type: svTypeDomainEnum}})
	}
	return buildNetServerEnum2Response(h, provider.ServerEntries())
}

// shareEntry is one SHARE_INFO_1 row: a share name, its STYPE_*, and an optional
// remark/comment.
type shareEntry struct {
	Name    string
	Type    uint16
	Comment string
}

// shareEntries lists this server's shares for NetShareEnum: every bound disk share
// the session identity may access (held under the service lock — the Manager
// mutates the slice at runtime) plus the always-present virtual IPC$ pipe. A
// restricted share the identity cannot use is omitted, matching the tree-connect
// gate so a guest never sees a share it could not bind.
func (s *Service) shareEntries(user string) []shareEntry {
	s.mu.Lock()
	out := make([]shareEntry, 0, len(s.shares)+1)
	for _, sh := range s.shares {
		if !sh.allows(user) {
			continue
		}
		out = append(out, shareEntry{Name: sh.Name(), Type: shareTypeDisktree, Comment: sh.Description()})
	}
	s.mu.Unlock()
	out = append(out, shareEntry{Name: ipcShareName, Type: shareTypeIPC})
	return out
}

// handleNetShareEnum answers a RAP NetShareEnum (function 0x0000) from this
// server's own shares — no browser involved, since shares are SMB's own state.
// The session identity filters which shares are listed.
func (s *Service) handleNetShareEnum(h protocol.Header, user string) []byte {
	return buildNetShareEnumResponse(h, s.shareEntries(user))
}

// buildNetShareEnumResponse packs the share entries into a RAP NetShareEnum reply
// (SHARE_INFO_1 records + a trailing remark heap) inside an SMB_COM_TRANSACTION
// response. Each record is Name(13)+Pad(1)+Type(2)+RemarkOff(4) = 20 bytes; the
// netname is capped at 12 chars + NUL.
func buildNetShareEnumResponse(h protocol.Header, entries []shareEntry) []byte {
	const entrySize = 20

	remarkBase := len(entries) * entrySize
	remarkOff := remarkBase
	remarkData := make([]byte, 0, len(entries))
	remarkOffsets := make([]int, len(entries))
	for i, e := range entries {
		remarkOffsets[i] = remarkOff
		remarkData = append(remarkData, []byte(e.Comment)...)
		remarkData = append(remarkData, 0)
		remarkOff += len(e.Comment) + 1
	}

	const paramLen = 8
	params := make([]byte, paramLen)
	// params[0:2] Status = 0, params[2:4] Converter = 0.
	bp.PutLE16(params[4:6], uint16(len(entries))) // EntriesReturned
	bp.PutLE16(params[6:8], uint16(len(entries))) // EntriesAvailable

	data := make([]byte, remarkBase+len(remarkData))
	for i, e := range entries {
		base := i * entrySize
		name := e.Name
		if len(name) > 12 {
			name = name[:12]
		}
		copy(data[base:base+12], name) // shi1_netname, NUL-padded to 13
		bp.PutLE16(data[base+14:base+16], e.Type)
		bp.PutLE32(data[base+16:base+20], uint32(remarkOffsets[i]))
	}
	copy(data[remarkBase:], remarkData)

	return buildTransactionResponse(h, params, data)
}

// buildRAPError wraps a non-zero RAP status in an SMB_COM_TRANSACTION success frame
// (SMB status SUCCESS; the RAP error rides the 2-byte Status param), matching what
// LANMAN clients expect.
func buildRAPError(h protocol.Header, rapStatus uint16) []byte {
	const paramLen = 8 // Status(2)+Converter(2)+EntriesReturned(2)+EntriesAvailable(2)
	params := make([]byte, paramLen)
	bp.PutLE16(params[0:2], rapStatus)
	return buildTransactionResponse(h, params, nil)
}

// buildNetServerEnum2Response packs the browse-list entries into a RAP
// NetServerEnum2 reply (SERVER_INFO_1 records + a trailing comment heap) inside an
// SMB_COM_TRANSACTION response.
func buildNetServerEnum2Response(h protocol.Header, entries []BrowseServer) []byte {
	const entrySize = 26 // SERVER_INFO_1: Name(16)+VMaj(1)+VMin(1)+Type(4)+CommentOff(4)

	commentBase := len(entries) * entrySize
	commentOff := commentBase
	commentData := make([]byte, 0, len(entries))
	commentOffsets := make([]int, len(entries))
	for i, e := range entries {
		commentOffsets[i] = commentOff
		commentData = append(commentData, []byte(e.Comment)...)
		commentData = append(commentData, 0)
		commentOff += len(e.Comment) + 1
	}

	const paramLen = 8
	params := make([]byte, paramLen)
	// params[0:2] Status = 0, params[2:4] Converter = 0.
	bp.PutLE16(params[4:6], uint16(len(entries))) // EntriesReturned
	bp.PutLE16(params[6:8], uint16(len(entries))) // EntriesAvailable

	data := make([]byte, commentBase+len(commentData))
	for i, e := range entries {
		base := i * entrySize
		name := browserName(e.Name)
		copy(data[base:base+16], name)
		data[base+16] = 4 // sv1_version_major
		bp.PutLE32(data[base+18:base+22], e.Type)
		bp.PutLE32(data[base+22:base+26], uint32(commentOffsets[i]))
	}
	copy(data[commentBase:], commentData)

	return buildTransactionResponse(h, params, data)
}

// buildTransactionResponse assembles an SMB_COM_TRANSACTION response (WCT=10) with
// the given RAP parameter and data blocks at their header-relative offsets.
func buildTransactionResponse(h protocol.Header, params, data []byte) []byte {
	rh := responseHeader(h, statusSuccess)
	out := rh.Encode(nil)
	out = append(out, 10) // WordCount

	// header(32) + WCT(1) + 10 words(20) + ByteCount(2).
	paramOffset := protocol.HeaderLen + 1 + 20 + 2
	dataOffset := paramOffset + len(params)

	w := make([]byte, 20)
	bp.PutLE16(w[0:2], uint16(len(params)))  // TotalParameterCount
	bp.PutLE16(w[2:4], uint16(len(data)))    // TotalDataCount
	bp.PutLE16(w[6:8], uint16(len(params)))  // ParameterCount
	bp.PutLE16(w[8:10], uint16(paramOffset)) // ParameterOffset
	bp.PutLE16(w[12:14], uint16(len(data)))  // DataCount
	bp.PutLE16(w[14:16], uint16(dataOffset)) // DataOffset
	out = append(out, w...)

	bcc := len(params) + len(data)
	out = append(out, byte(bcc), byte(bcc>>8))
	out = append(out, params...)
	out = append(out, data...)
	return out
}

// browserName renders a server name into a 16-byte zero-padded field, upper-cased
// and capped at 15 chars (the NetBIOS limit).
func browserName(name string) []byte {
	n := strings.ToUpper(strings.TrimSpace(name))
	if len(n) > 15 {
		n = n[:15]
	}
	out := make([]byte, 16)
	copy(out, n)
	return out
}

// indexFold returns the index of sub in b, case-insensitively, or -1.
func indexFold(b []byte, sub string) int {
	up := strings.ToUpper(string(b))
	return strings.Index(up, strings.ToUpper(sub))
}
