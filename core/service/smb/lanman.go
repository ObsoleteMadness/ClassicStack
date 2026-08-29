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

// RAP function codes ([MS-RAP]) we answer with real data: NetShareEnum lists this
// server's shares, NetServerEnum2 lists the servers the browser has observed, and
// NetWkstaGetInfo (level 10) reports this server's own workstation identity. Every
// OTHER RAP function (NetServerGetInfo 0x000D, …) is answered with an empty-success
// TRANSACTION reply rather than data — see handleTransaction.
const (
	rapNetShareEnum    uint16 = 0x0000
	rapNetServerEnum2  uint16 = 0x0068
	rapNetWkstaGetInfo uint16 = 0x003F // NetWkstaGetInfo ([MS-RAP] 63) — workstation identity
)

// wkstaInfoLevel10 is the WKSTA_INFO detail level Win98/WfW requests when a user
// opens \\server (ReturnDesc "zzzBBzz"). We answer this level with a real record.
const wkstaInfoLevel10 uint16 = 10

// SHARE_INFO_1 share-type bits ([MS-SRVS] STYPE_*): a disk tree vs the IPC$ pipe.
const (
	shareTypeDisktree uint16 = 0x0000
	shareTypeIPC      uint16 = 0x0003
)

// RAP status codes ([MS-ERREF] Win32) returned in the 2-byte RAP Status param.
const (
	rapStatusReqNotAccepted uint16 = 71 // ERROR_REQ_NOT_ACCEP (a potential browser)
)

// SV_TYPE_* bits NetServerEnum2 filters on ([MS-SRVS]).
const (
	svTypeWorkstation uint32 = 0x00000001 // SV_TYPE_WORKSTATION
	svTypeServer      uint32 = 0x00000002 // SV_TYPE_SERVER
	svTypeDomainEnum  uint32 = 0x80000000 // SV_TYPE_DOMAIN_ENUM
)

const lanmanPipe = "\\PIPE\\LANMAN"

// Version reported in RAP records (SERVER_INFO_1 sv1_version_* and WKSTA_INFO_10
// wki10_ver_*). We present ourselves as a LAN Manager 4.x-era server, which is
// what the browse list and \\server workstation query expect.
const (
	smbVerMajor byte = 4
	smbVerMinor byte = 0
)

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
	case rapNetWkstaGetInfo:
		// Win98/WfW issue this when a user opens \\server, and (over NetBEUI) will
		// NOT accept an empty-success reply — they re-issue it forever, hanging
		// Explorer (captures/netbeui.pcap frames 128→192, 339→363). Only the level-10
		// WKSTA_INFO record is understood; any other level falls through to
		// empty-success.
		if lvl, ok := parseRAPDetailLevel(area); ok && lvl == wkstaInfoLevel10 {
			return s.handleNetWkstaGetInfo(h, sess.user)
		}
	}
	// Any OTHER RAP call — including NetServerGetInfo (0x000D), which Win98 issues when
	// opening \\server — gets an empty-success TRANSACTION reply (SMB status SUCCESS,
	// WCT=10, zero params/data), NOT an error and NOT a synthesized info record:
	//   - Returning ERRDOS/ERRbadfunc makes a CORE-dialect client (Win9x/WfW, Flags2=0)
	//     abandon the server (it stops listing \\CLASSICSTACK). This was the refactor
	//     regression: the handler answered STATUS_NOT_SUPPORTED for unknown functions.
	//   - Returning a hand-built SERVER_INFO_1 record for NetServerGetInfo corrupts the
	//     client: a Win98 redirector fed a non-empty NetServerGetInfo reply BLUESCREENS
	//     (captures/ipx.pcap). So NetServerGetInfo stays empty-success.
	// NOTE: NetWkstaGetInfo (0x003F) is handled ABOVE with a real WKSTA_INFO_10 record
	// because a Win98/WfW NetBEUI client rejects the empty-success form and loops
	// (captures/netbeui.pcap). The IPX-path BLUESCREEN warning above was recorded for
	// NetServerGetInfo's record and (historically) applied to WKSTA too; re-verify the
	// WKSTA_INFO_10 reply against a live Win98-over-IPX client — see spec/errata.md.
	return buildTransactionResponse(h, nil, nil)
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

// parseRAPDetailLevel best-effort extracts the info-level word a RAP "GetInfo"
// call requests. It sits after the function code, ParamDesc and ReturnDesc
// (both NUL-terminated strings): \PIPE\LANMAN\0 Function(2) ParamDesc\0 ReturnDesc\0
// Level(2). A parse miss returns (0, false).
func parseRAPDetailLevel(area []byte) (uint16, bool) {
	marker := lanmanPipe + "\x00"
	idx := indexFold(area, marker)
	if idx < 0 {
		return 0, false
	}
	p := idx + len(marker) + 2 // past the function code
	for range 2 {              // skip ParamDesc + ReturnDesc (NUL-terminated)
		n := indexByte(area[p:], 0)
		if n < 0 {
			return 0, false
		}
		p += n + 1
	}
	if p+2 > len(area) {
		return 0, false
	}
	return bp.LE16(area[p : p+2]), true
}

// netServerEnum2Params best-effort extracts the detail LEVEL and the SV_TYPE_* filter
// from the RAP NetServerEnum2 request. Layout after \PIPE\LANMAN\0: Function(2),
// ParamDesc\0, DataDesc\0, Level(2), ReceiveBufferLength(2), ServerType(4). A parse miss
// returns (0, 0). The level is the real discriminator between the domain enumeration
// (level 0, returns domains) and the server list (level 1, returns SERVER_INFO_1) — a real
// WfW/Win98 redirector sends servertype 0xFFFFFFFF (DOMAIN_ENUM bit INCLUDED) for the
// level-1 server list, so the type mask alone cannot tell the two calls apart.
func netServerEnum2Params(area []byte) (level uint16, serverType uint32) {
	marker := lanmanPipe + "\x00"
	idx := indexFold(area, marker)
	if idx < 0 {
		return 0, 0
	}
	p := idx + len(marker) + 2 // past the function code
	for range 2 {              // skip ParamDesc + DataDesc (NUL-terminated)
		n := indexByte(area[p:], 0)
		if n < 0 {
			return 0, 0
		}
		p += n + 1
	}
	if p+2+2+4 > len(area) {
		return 0, 0
	}
	level = bp.LE16(area[p : p+2])
	p += 2 + 2 // Level + ReceiveBufferLength
	return level, bp.LE32(area[p : p+4])
}

// handleNetServerEnum2 answers a RAP NetServerEnum2 from the browse list. The DETAIL
// LEVEL is the discriminator, matching a real WfW/Win98 redirector
// (captures/win98nbf-win31nbf.pcapng): level 0 with servertype 0x80000000 is the domain
// enumeration (returns our workgroup); level 1 is the server list — and a real client sends
// servertype 0xFFFFFFFF for it (the DOMAIN_ENUM bit is INCLUDED, not cleared), so we must
// NOT treat that as an invalid mix. A potential browser (no list available) answers
// ERROR_REQ_NOT_ACCEP.
func (s *Service) handleNetServerEnum2(h protocol.Header, area []byte) []byte {
	level, serverType := netServerEnum2Params(area)

	// Level 0 = domain enumeration: report our own workgroup as the one domain. This is
	// the DOMAIN_ENUM call (servertype 0x80000000 at level 0), independent of the browse
	// list, so it is answered even with no browser wired. The level-0 reply uses the
	// name-only "B16" record (16 bytes), NOT the 26-byte SERVER_INFO_1 — a real WfW client
	// sends ReturnDesc "B16" for this call and parses 16-byte records
	// (captures/win98nbf-win31nbf.pcapng frame 47: TotalDataCount=16 for one WORKGROUP entry).
	if level == 0 && serverType&svTypeDomainEnum != 0 {
		return buildDomainEnumResponse(h, []string{s.workgroup()})
	}

	provider := s.browseProvider()
	if provider == nil {
		// No browser wired (e.g. a NetBIOS-less direct-TCP :445 deployment): there is
		// no browse list, but the server can still report ITSELF so a client browsing
		// \\server sees a named entry with its comment (§4-bis). Reported as a
		// workstation+server so it shows in the list.
		self := BrowseServer{Name: s.serverName(), Type: svTypeServer | svTypeWorkstation, Comment: s.description()}
		return buildNetServerEnum2Response(h, []BrowseServer{self})
	}
	if !provider.Available() {
		return buildRAPError(h, rapStatusReqNotAccepted)
	}
	// Level 1 (or anything not the domain enumeration): the authoritative server list.
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

// handleNetWkstaGetInfo answers a RAP NetWkstaGetInfo level-10 call with a
// WKSTA_INFO_10 record describing this server's own workstation identity. Win98/WfW
// over NetBEUI require a real record here — an empty-success reply is rejected and
// re-issued forever, hanging Explorer (captures/netbeui.pcap).
func (s *Service) handleNetWkstaGetInfo(h protocol.Header, user string) []byte {
	return buildNetWkstaGetInfoResponse(h, s.serverName(), user, s.workgroup())
}

// buildNetWkstaGetInfoResponse packs a WKSTA_INFO_10 record (ReturnDesc "zzzBBzz")
// into an SMB_COM_TRANSACTION response. The fixed part is
// computername(z)+username(z)+langroup(z)+ver_major(B)+ver_minor(B)+logon_domain(z)+
// oth_domains(z); each z is a 4-byte data-relative pointer (offset in the low word,
// high word zero — the same convention as NetShareEnum's RemarkOff), and the strings
// live in a NUL-terminated heap after the fixed part.
func buildNetWkstaGetInfoResponse(h protocol.Header, computer, user, workgroup string) []byte {
	const (
		ptrSize   = 4             // a RAP "z" pointer
		fixedSize = ptrSize*5 + 2 // 5 z-pointers + ver_major(1) + ver_minor(1)
	)
	// wki10_username is the logged-on user; a guest session ("") reports empty.
	// wki10_oth_domains is empty (we advertise no other domains).
	strs := []string{computer, user, workgroup, "", ""} // computername, username, langroup, logon_domain, oth_domains

	// Lay out the heap and record each string's data-relative offset.
	offsets := make([]int, len(strs))
	heap := make([]byte, 0)
	off := fixedSize
	for i, str := range strs {
		offsets[i] = off
		heap = append(heap, []byte(str)...)
		heap = append(heap, 0)
		off += len(str) + 1
	}

	data := make([]byte, fixedSize+len(heap))
	bp.PutLE16(data[0:2], uint16(offsets[0]))   // wki10_computername
	bp.PutLE16(data[4:6], uint16(offsets[1]))   // wki10_username
	bp.PutLE16(data[8:10], uint16(offsets[2]))  // wki10_langroup
	data[16] = smbVerMajor                      // wki10_ver_major
	data[17] = smbVerMinor                      // wki10_ver_minor
	bp.PutLE16(data[18:20], uint16(offsets[3])) // wki10_logon_domain
	bp.PutLE16(data[22:24], uint16(offsets[4])) // wki10_oth_domains
	copy(data[fixedSize:], heap)

	const paramLen = 4 // Status(2)+Converter(2) — no Entries* fields for a Get call
	params := make([]byte, paramLen)
	// params[0:2] Status = 0, params[2:4] Converter = 0.

	return buildTransactionResponse(h, params, data)
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
		data[base+16] = smbVerMajor // sv1_version_major
		data[base+17] = smbVerMinor // sv1_version_minor
		bp.PutLE32(data[base+18:base+22], e.Type)
		bp.PutLE32(data[base+22:base+26], uint32(commentOffsets[i]))
	}
	copy(data[commentBase:], commentData)

	return buildTransactionResponse(h, params, data)
}

// buildDomainEnumResponse packs a level-0 NetServerEnum2 DOMAIN enumeration reply: each
// domain is a bare 16-byte name record ("B16" ReturnDesc), with NO version/type/comment
// fields and NO comment heap — the shape a real WfW/Win98 client expects for the level-0
// call (captures/win98nbf-win31nbf.pcapng frame 47). The RAP parameter block is the usual
// Status/Converter/EntriesReturned/EntriesAvailable; the data block is just the names.
func buildDomainEnumResponse(h protocol.Header, domains []string) []byte {
	const nameSize = 16 // "B16" record: a 16-byte name only.

	const paramLen = 8
	params := make([]byte, paramLen)
	// params[0:2] Status = 0, params[2:4] Converter = 0 (name-only records carry no pointers).
	bp.PutLE16(params[4:6], uint16(len(domains))) // EntriesReturned
	bp.PutLE16(params[6:8], uint16(len(domains))) // EntriesAvailable

	data := make([]byte, len(domains)*nameSize)
	for i, d := range domains {
		copy(data[i*nameSize:(i+1)*nameSize], browserName(d))
	}
	return buildTransactionResponse(h, params, data)
}

// buildTransactionResponse assembles an SMB_COM_TRANSACTION response (WCT=10) with
// the given RAP parameter and data blocks at their header-relative offsets.
//
// The empty-success case (no params AND no data — an unimplemented RAP function such
// as NetWkstaGetInfo/NetServerGetInfo) is special: the 20-byte parameter block MUST be
// left ALL ZERO, including ParameterOffset and DataOffset. This mirrors the legacy
// buildSMBTransactionEmptySuccess byte-for-byte. A Win98 RAP client over IPC$ rejects a
// zero-count reply whose ParameterOffset/DataOffset are non-zero (it computes a buffer
// past the end of the frame and treats the transaction as incomplete), so it re-issues
// NetWkstaGetInfo forever and never opens \\CLASSICSTACK — the loop seen in
// captures/ipx.pcap. With the offsets zeroed the client accepts the empty reply and
// proceeds. (Do NOT synthesize a WKSTA_INFO_10 record here — that bluescreens Win98;
// see spec/errata.md.)
func buildTransactionResponse(h protocol.Header, params, data []byte) []byte {
	rh := responseHeader(h, statusSuccess)
	out := rh.Encode(nil)
	out = append(out, 10) // WordCount

	w := make([]byte, 20)
	if len(params) > 0 || len(data) > 0 {
		// header(32) + WCT(1) + 10 words(20) + ByteCount(2).
		paramOffset := protocol.HeaderLen + 1 + 20 + 2
		dataOffset := paramOffset + len(params)
		bp.PutLE16(w[0:2], uint16(len(params)))  // TotalParameterCount
		bp.PutLE16(w[2:4], uint16(len(data)))    // TotalDataCount
		bp.PutLE16(w[6:8], uint16(len(params)))  // ParameterCount
		bp.PutLE16(w[8:10], uint16(paramOffset)) // ParameterOffset
		bp.PutLE16(w[12:14], uint16(len(data)))  // DataCount
		bp.PutLE16(w[14:16], uint16(dataOffset)) // DataOffset
	}
	// else: empty-success — the 20-byte block (offsets included) stays zero.
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
