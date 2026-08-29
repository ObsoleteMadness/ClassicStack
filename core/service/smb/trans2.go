package smb

import (
	stdfs "io/fs"
	"strings"
	"unicode/utf16"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"

	protocol "github.com/ObsoleteMadness/ClassicStack/core/protocol/smb"
)

// --- SMB_COM_TRANSACTION2 subcommands over the §9 share seam: FIND_FIRST2 /
// FIND_NEXT2 (directory enumeration, the modern dir-listing path Win9x and the
// classic-Mac SMB client use), FIND_CLOSE2, and QUERY_PATH_INFORMATION /
// QUERY_FILE_INFORMATION (stat by path / by FID). The enumeration snapshots the
// directory at FIND_FIRST2 time into a per-session searchHandle and streams it
// across FIND_NEXT2 calls; entry names are packed in the request's wire charset
// via the share codec (UTF-16 for an NT client, OEM/ANSI for a DOS client). ---

const (
	trans2FindFirst2     = 0x0001
	trans2FindNext2      = 0x0002
	trans2QueryFSInfo    = 0x0003 // TRANS2_QUERY_FS_INFORMATION
	trans2QueryPathInfo  = 0x0005
	trans2SetPathInfo    = 0x0006 // TRANS2_SET_PATH_INFORMATION
	trans2QueryFileInfo  = 0x0007
	trans2SetFileInfo    = 0x0008 // TRANS2_SET_FILE_INFORMATION
	infoStandard         = 0x0001 // SMB_INFO_STANDARD (LANMAN2.0 find level — OS/2, DOS LANMAN)
	infoQueryEaSize      = 0x0002 // SMB_INFO_QUERY_EA_SIZE (SMB_INFO_STANDARD + EaSize)
	infoQueryEasFromList = 0x0003 // SMB_INFO_QUERY_EAS_FROM_LIST (OS/2 WPS folder-view EA probe)
	infoQueryAllEAs      = 0x0004 // SMB_INFO_QUERY_ALL_EAS (QUERY_PATH/FILE_INFO only)
	infoFileBothDirInfo  = 0x0104 // SMB_FIND_FILE_BOTH_DIRECTORY_INFO
	infoQueryFileBasic   = 0x0101 // SMB_QUERY_FILE_BASIC_INFO
	infoQueryFileStd     = 0x0102 // SMB_QUERY_FILE_STANDARD_INFO
	infoQueryFileEA      = 0x0103 // SMB_QUERY_FILE_EA_INFO
	infoQueryFileName    = 0x0104 // SMB_QUERY_FILE_NAME_INFO (FileNameInformation)
	infoQueryFileAllInfo = 0x0107 // SMB_QUERY_FILE_ALL_INFO
	infoSetFileBasic     = 0x0101 // SMB_SET_FILE_BASIC_INFO (FileBasicInformation)
	infoSetEAs           = 0x0002 // SMB_INFO_SET_EAS (TRANS2_SET_PATH/FILE_INFORMATION)

	// TRANS2_QUERY_FS_INFORMATION information levels ([smb6.0] 4118 table;
	// [MS-CIFS] §2.2.2.3.4). Levels ≥ 0x102 "are mapped to corresponding calls
	// to NtQueryVolumeInformationFile" ([smb6.0] 4116), so their strings are
	// Unicode regardless of the request charset.
	fsInfoAllocation     = 0x0001 // SMB_INFO_ALLOCATION
	fsInfoVolume         = 0x0002 // SMB_INFO_VOLUME
	fsQueryVolumeInfo    = 0x0102 // SMB_QUERY_FS_VOLUME_INFO (FileFsVolumeInformation)
	fsQuerySizeInfo      = 0x0103 // SMB_QUERY_FS_SIZE_INFO (FileFsSizeInformation)
	fsQueryDeviceInfo    = 0x0104 // SMB_QUERY_FS_DEVICE_INFO (FileFsDeviceInformation)
	fsQueryAttributeInfo = 0x0105 // SMB_QUERY_FS_ATTRIBUTE_INFO (FileFsAttributeInformation)

	// SMB_QUERY_FS_DEVICE_INFO fields ([MS-CIFS] §2.2.8.2.5).
	fileDeviceDisk      = 0x00000007 // FILE_DEVICE_DISK
	fileDeviceIsMounted = 0x00000020 // FILE_DEVICE_IS_MOUNTED

	// SMB_QUERY_FS_ATTRIBUTE_INFO FileSystemAttributes ([MS-CIFS] §2.2.8.2.6).
	fileCasePreservedNames = 0x00000002 // FILE_CASE_PRESERVED_NAMES

	findCloseAfterRequest = 0x0001 // SMB_FIND_CLOSE_AFTER_REQUEST
	findCloseAtEOS        = 0x0002 // SMB_FIND_CLOSE_AT_EOS
	findReturnResumeKeys  = 0x0004 // SMB_FIND_RETURN_RESUME_KEYS

	// The synthetic disk geometry every space-reporting reply uses: 512-byte
	// sectors, 64 sectors per allocation unit (32 KiB units) — matching
	// SMB_COM_QUERY_INFORMATION_DISK (pathops.go).
	fsBytesPerSector = 512
	fsSectorsPerUnit = 64

	// defaultClientMaxBufferSize is the cap a TRANS2 response is chunked to before
	// the client's own SESSION_SETUP_ANDX MaxBufferSize has been observed ([MS-CIFS]
	// "MaxBufferSize": "The server SHOULD provide a MaxBufferSize of 4356 bytes").
	// Confirmed as the real figure a live IBM Peer server chunks large TRANS2
	// responses to, independent of what MaxDataCount the client's request offered —
	// captures/ibm-peer-clients.pcapng frames 633 (request, MaxDataCount 65523) /
	// 637+641 (response split TotalDataCount=4797 into DataCount 4288+509 at
	// DataDisplacement 0/4288; Wireshark's own reassembly annotation on frame 637
	// reads "Reassembled NetBIOS length: 4356").
	defaultClientMaxBufferSize = 4356
)

// trans2Request is the parsed TRANS2 sub-request: the subcommand plus its
// parameter and data blocks. The SET_*_INFORMATION subcommands carry the
// information level + target in the params and the FileBasicInfo payload in the
// data block, so both are surfaced. totalParams/totalData are the transaction's
// TotalParameterCount/TotalDataCount — when the primary request carries fewer
// bytes than those, the remainder arrives in SMB_COM_TRANSACTION2_SECONDARY
// messages and the transaction reassembles on the session first.
type trans2Request struct {
	sub         uint16
	params      []byte
	data        []byte
	totalParams int
	totalData   int
	maxData     int // MaxDataCount: the largest data block the client will accept in the reply
}

// incomplete reports whether the request carries fewer parameter/data bytes
// than the transaction totals — [MS-CIFS] §2.2.4.46.1: the client sends the
// rest in SMB_COM_TRANSACTION2_SECONDARY messages after the server's interim
// response.
func (t2 trans2Request) incomplete() bool {
	return len(t2.params) < t2.totalParams || len(t2.data) < t2.totalData
}

// parseTransaction2 decodes the SMB_COM_TRANSACTION2 wrapper ([MS-CIFS]
// §2.2.4.46.1): WCT≥14, with ParameterCount/Offset and SetupCount in the words,
// the first setup word being the subcommand. The param block is sliced at its
// header-relative offset.
func parseTransaction2(req []byte) (trans2Request, bool) {
	words, _, ok := reqBody(req)
	if !ok || len(words) < 28 {
		return trans2Request{}, false
	}
	paramCount := int(bp.LE16(words[18:20]))
	paramOffset := int(bp.LE16(words[20:22]))
	setupCount := int(words[26])
	if setupCount < 1 || 28+2*setupCount > len(words) {
		return trans2Request{}, false
	}
	sub := bp.LE16(words[28:30])
	if paramCount < 0 || paramOffset < protocol.HeaderLen || paramOffset+paramCount > len(req) {
		return trans2Request{}, false
	}
	t2 := trans2Request{
		sub:         sub,
		params:      req[paramOffset : paramOffset+paramCount],
		totalParams: int(bp.LE16(words[0:2])),
		totalData:   int(bp.LE16(words[2:4])),
		maxData:     int(bp.LE16(words[6:8])), // MaxDataCount ([MS-CIFS] §2.2.4.46.1)
	}
	// The data block (DataCount/DataOffset, words[22:24]/[24:26]) carries the
	// SET_*_INFORMATION payload; surface it when present and in-bounds.
	dataCount := int(bp.LE16(words[22:24]))
	dataOffset := int(bp.LE16(words[24:26]))
	if dataCount > 0 && dataOffset >= protocol.HeaderLen && dataOffset+dataCount <= len(req) {
		t2.data = req[dataOffset : dataOffset+dataCount]
	}
	return t2, true
}

// handleTransaction2 answers SMB_COM_TRANSACTION2. A request whose
// ParameterCount/DataCount equal the transaction totals dispatches immediately;
// one that carries less is parked on the session and answered with the interim
// response (an empty WCT=0/BCC=0 success, [MS-CIFS] §2.2.4.46.2) that tells the
// client to send its SMB_COM_TRANSACTION2_SECONDARY fragments — OS/2 WPS splits
// an SMB_INFO_SET_EAS carrying a multi-KB .ICON EA this way (netbeui.pcap
// 2026-07-14 frame 242: DataCount 4240 of TotalDataCount 6848; answering with
// an error there aborts the transfer and the icon is never set, frame 243).
func (s *Service) handleTransaction2(sess *smbSession, h protocol.Header, req []byte) []byte {
	sh, st := s.treeFor(sess, h)
	if st != statusSuccess {
		return errResponse(h, st)
	}
	t2, ok := parseTransaction2(req)
	if !ok {
		return errResponse(h, statusNotSupported)
	}
	if t2.incomplete() {
		if !sess.stashTrans2(trans2Key(h), newPendingTrans2(h, t2)) {
			return errResponse(h, statusUnsuccessful)
		}
		return successNoData(h) // interim response: transaction accepted, send secondaries
	}
	return s.dispatchTrans2(sess, sh, h, t2)
}

// dispatchTrans2 routes a fully-assembled TRANS2 request to its subcommand
// handler. The data block rides along to the find/query handlers because the
// SMB_INFO_QUERY_EAS_FROM_LIST level carries its SMB_GEA_LIST name filter
// there.
func (s *Service) dispatchTrans2(sess *smbSession, sh *Share, h protocol.Header, t2 trans2Request) []byte {
	switch t2.sub {
	case trans2FindFirst2:
		return s.findFirst2(sess, sh, h, t2.params, t2.data, t2.maxData)
	case trans2FindNext2:
		return s.findNext2(sess, sh, h, t2.params, t2.data, t2.maxData)
	case trans2QueryFSInfo:
		return s.queryFSInfo(sh, h, t2.params)
	case trans2QueryPathInfo:
		return s.queryPathInfo(sess, sh, h, t2.params, t2.data)
	case trans2QueryFileInfo:
		return s.queryFileInfo(sess, h, t2.params, t2.data)
	case trans2SetPathInfo:
		return s.setPathInfo(sess, sh, h, t2.params, t2.data)
	case trans2SetFileInfo:
		return s.setFileInfo(sess, h, t2.params, t2.data)
	default:
		return errResponse(h, statusNotSupported)
	}
}

// trans2Key derives the session-map key a transaction reassembles under. The
// PID and MID MUST be the same for all requests of one transaction ([MS-CIFS]
// §2.2.4.46.1), and one PID+MID pair carries at most one transaction at a time.
func trans2Key(h protocol.Header) uint32 {
	return uint32(h.PIDLow)<<16 | uint32(h.MID)
}

// newPendingTrans2 parks an incomplete primary request: buffers are allocated
// at the transaction totals and the primary's bytes land at displacement 0.
// The bytes are copied out of req because the transport owns that buffer.
func newPendingTrans2(h protocol.Header, t2 trans2Request) *pendingTrans2 {
	p := &pendingTrans2{
		sub:         t2.sub,
		tid:         h.TID,
		params:      make([]byte, t2.totalParams),
		data:        make([]byte, t2.totalData),
		totalParams: t2.totalParams,
		totalData:   t2.totalData,
	}
	p.paramGot = copy(p.params, t2.params)
	p.dataGot = copy(p.data, t2.data)
	return p
}

// handleTransaction2Secondary serves SMB_COM_TRANSACTION2_SECONDARY (0x33,
// [MS-CIFS] §2.2.4.47.1, WCT=9): one fragment of a transaction parked by
// handleTransaction2. Its bytes are copied into the pending buffers at their
// Parameter/DataDisplacement; the fragment itself is never answered (no
// response is defined for a secondary), and the final fragment executes the
// assembled transaction, whose response goes out as a normal
// SMB_COM_TRANSACTION2 response ("the Command for all responses MUST be
// SMB_COM_TRANSACTION2", §2.2.4.46). A secondary with no transaction in
// progress, a wrong TID, or out-of-bounds fragment geometry is dropped —
// abandoning the reassembly in the malformed cases so a broken client times
// out rather than committing a torn transaction.
func (s *Service) handleTransaction2Secondary(sess *smbSession, h protocol.Header, req []byte) []byte {
	words, _, ok := reqBody(req)
	if !ok || len(words) < 18 { // WCT=9 → 18 param bytes
		return nil
	}
	key := trans2Key(h)
	p, found := sess.pendingTrans2For(key)
	if !found || h.TID != p.tid {
		return nil
	}
	// Words: TotalParameterCount(2) TotalDataCount(2) ParameterCount(2)
	// ParameterOffset(2) ParameterDisplacement(2) DataCount(2) DataOffset(2)
	// DataDisplacement(2) FID(2, 0xFFFF = none; unused — the parked subcommand
	// already carries its target).
	totalParams := int(bp.LE16(words[0:2]))
	totalData := int(bp.LE16(words[2:4]))
	paramCount := int(bp.LE16(words[4:6]))
	paramOffset := int(bp.LE16(words[6:8]))
	paramDisp := int(bp.LE16(words[8:10]))
	dataCount := int(bp.LE16(words[10:12]))
	dataOffset := int(bp.LE16(words[12:14]))
	dataDisp := int(bp.LE16(words[14:16]))

	// A secondary MAY reduce the totals, never grow them (§2.2.4.47.1).
	if totalParams < p.totalParams {
		p.totalParams = totalParams
	}
	if totalData < p.totalData {
		p.totalData = totalData
	}

	if !copyTrans2Fragment(p.params, paramDisp, req, paramOffset, paramCount) ||
		!copyTrans2Fragment(p.data, dataDisp, req, dataOffset, dataCount) {
		sess.dropTrans2(key)
		return nil
	}
	p.paramGot += paramCount
	p.dataGot += dataCount
	if !p.complete() {
		return nil
	}

	sess.dropTrans2(key)
	sh, st := s.treeFor(sess, h)
	if st != statusSuccess {
		return errResponse(h, st)
	}
	h.Command = protocol.CommandTransaction2 // the final response is a TRANS2 response
	return s.dispatchTrans2(sess, sh, h, trans2Request{
		sub:         p.sub,
		params:      p.params[:p.totalParams],
		data:        p.data[:p.totalData],
		totalParams: p.totalParams,
		totalData:   p.totalData,
	})
}

// copyTrans2Fragment places one secondary's parameter or data block into the
// reassembly buffer at its displacement, reporting false when the block's
// offset/count fall outside the request or the displacement outside the
// buffer. A zero-count block is trivially fine (its offset may be 0).
func copyTrans2Fragment(dst []byte, disp int, req []byte, off, count int) bool {
	if count == 0 {
		return true
	}
	if off < protocol.HeaderLen || off+count > len(req) || disp+count > len(dst) {
		return false
	}
	copy(dst[disp:], req[off:off+count])
	return true
}

// setPathInfo serves TRANS2_SET_PATH_INFORMATION. Params ([MS-CIFS] §2.2.6.7.1):
// InformationLevel(2) Reserved(4) FileName(SMB_STRING). The data block holds the
// FileBasicInfo whose attribute word (offset 32) is persisted through the share's
// DOS-attribute store, so a client setting Hidden/System/ReadOnly/Archive sticks
// even on a host filesystem that cannot represent those bits. A zero attribute
// word means "no change" ([MS-FSCC] FileBasicInformation), so it is ignored.
func (s *Service) setPathInfo(sess *smbSession, sh *Share, h protocol.Header, params, data []byte) []byte {
	_ = sess // SET responses are always tiny (EaErrorOffset only) — never chunked
	if len(params) < 6 {
		return errResponse(h, statusUnsuccessful)
	}
	level := bp.LE16(params[0:2])
	store, st := resolvePath(sh, params[6:], h.Flags2)
	if st != statusSuccess {
		return errResponse(h, st)
	}
	if level == infoSetEAs {
		return applySetEAs(sh, h, store, data)
	}
	return s.applySetBasicInfo(sh, h, store, level, data)
}

// setFileInfo serves TRANS2_SET_FILE_INFORMATION. Params ([MS-CIFS] §2.2.6.9.1):
// FID(2) InformationLevel(2) Reserved(2). The target is the open handle's store
// path; the data block is the same FileBasicInfo as setPathInfo.
func (s *Service) setFileInfo(sess *smbSession, h protocol.Header, params, data []byte) []byte {
	if len(params) < 4 {
		return errResponse(h, statusUnsuccessful)
	}
	fid := bp.LE16(params[0:2])
	level := bp.LE16(params[2:4])
	hnd, ok := sess.fileByFID(fid)
	if !ok {
		return errResponse(h, statusInvalidHandle)
	}
	if level == infoSetEAs {
		return applySetEAs(hnd.share, h, hnd.path, data)
	}
	return s.applySetBasicInfo(hnd.share, h, hnd.path, level, data)
}

// applySetEAs serves the SMB_INFO_SET_EAS level of TRANS2_SET_PATH/
// FILE_INFORMATION ([MS-CIFS] §2.2.8.4.2): the data block is an
// SMB_FEA_LIST that fully replaces the stored EA list for store. A malformed
// list is rejected with STATUS_UNSUCCESSFUL/ERRbadealist ([MS-CIFS] §2.2.6.7.2
// error table); the client-facing EaErrorOffset is left at its zero default
// (no existing trans2.go error path threads a custom parameter block either).
func applySetEAs(sh *Share, h protocol.Header, store string, data []byte) []byte {
	eas, ok, _ := parseFEAList(data)
	if !ok {
		return errResponse(h, statusUnsuccessful)
	}
	if err := sh.SetEAs(store, eas); err != nil {
		return errResponse(h, statusUnsuccessful)
	}
	return buildTrans2InfoResponse(nil, h, nil)
}

// applySetBasicInfo persists the attribute word from a FileBasicInfo data block
// (the level must be a basic-info level) through the share's DOS-attribute store.
// The FileBasicInfo layout ([MS-FSCC] §2.4.7): Creation/LastAccess/LastWrite/
// Change FILETIME (4×8 bytes) then FileAttributes(4) at offset 32. The timestamps
// are accepted-and-ignored (the host mtime is authoritative); only the attribute
// word is persisted. A reply is an empty TRANS2 info response (success).
func (s *Service) applySetBasicInfo(sh *Share, h protocol.Header, store string, level uint16, data []byte) []byte {
	if level != infoSetFileBasic {
		// Other set-info levels (allocation, disposition, rename) are not modelled;
		// answer success so a client's housekeeping set does not fail the operation.
		return buildTrans2InfoResponse(nil, h, nil)
	}
	if len(data) >= 36 {
		attrs := uint16(bp.LE32(data[32:36]) & 0xFFFF)
		// A zero attribute word means "do not change attributes" (FileBasicInformation).
		if attrs != 0 {
			if err := sh.SetAttrs(store, attrs); err != nil {
				return errResponse(h, statusUnsuccessful)
			}
		}
	}
	return buildTrans2InfoResponse(nil, h, nil)
}

// findFirst2 serves TRANS2_FIND_FIRST2. Params ([MS-CIFS] §2.2.6.2.1):
// SearchAttributes(2) SearchCount(2) Flags(2) InformationLevel(2)
// SearchStorageType(4) FileName(SMB_STRING, the wire-charset search path with a
// trailing wildcard). The directory is resolved through the codec, its entries
// filtered by the wildcard, snapshotted, and the first batch packed. reqData is
// the request's Trans2_Data block — the SMB_GEA_LIST name filter when the level
// is SMB_INFO_QUERY_EAS_FROM_LIST ([MS-CIFS] §2.2.6.2.1 "MUST be included" for
// that level), empty otherwise.
func (s *Service) findFirst2(sess *smbSession, sh *Share, h protocol.Header, params, reqData []byte, maxData int) []byte {
	if len(params) < 12 {
		return errResponse(h, statusNotSupported)
	}
	searchCount := clampSearchCount(int(bp.LE16(params[2:4])))
	flags := bp.LE16(params[4:6])
	infoLevel := bp.LE16(params[6:8])
	if !supportedFindLevel(infoLevel) {
		return errResponse(h, statusNotSupported)
	}
	geaNames, ok := parseGEAList(reqData)
	if infoLevel == infoQueryEasFromList && !ok {
		return errResponse(h, statusUnsuccessful)
	}

	dirStore, pattern, st := s.resolveSearchPath(sh, params[12:], h.Flags2)
	if st != statusSuccess {
		return errResponse(h, st)
	}
	rows, st := s.listDir(sh, dirStore, pattern)
	if st != statusSuccess {
		return errResponse(h, st)
	}

	data, returned, lastNameOff := packFindEntriesBudget(sh, rows, searchCount, findDataBudget(maxData), infoLevel, flags&findReturnResumeKeys != 0, geaNames, h.Flags2)
	endOfSearch := returned >= len(rows)

	sid := sess.allocSID(&searchHandle{rows: nil, flags2: h.Flags2})
	if !endOfSearch {
		sess.mu.Lock()
		sess.searches[sid].rows = append([]findRow(nil), rows[returned:]...)
		sess.mu.Unlock()
	} else if flags&(findCloseAfterRequest|findCloseAtEOS) != 0 {
		sess.dropSearch(sid)
	}

	return buildFindResponse(sess, h, true, sid, returned, endOfSearch, data, lastNameOff)
}

// findNext2 serves TRANS2_FIND_NEXT2. Params ([MS-CIFS] §2.2.6.3.1): SID(2)
// SearchCount(2) InformationLevel(2) ResumeKey(4) Flags(2) FileName(SMB_STRING).
// It streams the next batch from the snapshotted searchHandle. reqData carries
// the SMB_GEA_LIST name filter for the SMB_INFO_QUERY_EAS_FROM_LIST level (the
// client re-sends it on every FIND_NEXT2, [MS-CIFS] §2.2.6.3.1).
func (s *Service) findNext2(sess *smbSession, sh *Share, h protocol.Header, params, reqData []byte, maxData int) []byte {
	if len(params) < 12 {
		return errResponse(h, statusNotSupported)
	}
	sid := bp.LE16(params[0:2])
	searchCount := clampSearchCount(int(bp.LE16(params[2:4])))
	infoLevel := bp.LE16(params[4:6])
	if !supportedFindLevel(infoLevel) {
		return errResponse(h, statusNotSupported)
	}
	geaNames, ok := parseGEAList(reqData)
	if infoLevel == infoQueryEasFromList && !ok {
		return errResponse(h, statusUnsuccessful)
	}
	flags := bp.LE16(params[10:12])

	shndl, ok := sess.search(sid)
	if !ok {
		return errResponse(h, statusNoMoreFiles)
	}
	sess.mu.Lock()
	rows := shndl.rows
	sess.mu.Unlock()
	if len(rows) == 0 {
		if flags&(findCloseAfterRequest|findCloseAtEOS) != 0 {
			sess.dropSearch(sid)
		}
		return errResponse(h, statusNoMoreFiles)
	}

	data, returned, lastNameOff := packFindEntriesBudget(sh, rows, searchCount, findDataBudget(maxData), infoLevel, flags&findReturnResumeKeys != 0, geaNames, h.Flags2)
	endOfSearch := returned >= len(rows)

	sess.mu.Lock()
	shndl.rows = append([]findRow(nil), rows[returned:]...)
	remaining := len(shndl.rows)
	sess.mu.Unlock()
	if (endOfSearch && flags&findCloseAtEOS != 0) || flags&findCloseAfterRequest != 0 || remaining == 0 && endOfSearch {
		sess.dropSearch(sid)
	}

	return buildFindResponse(sess, h, false, 0, returned, endOfSearch, data, lastNameOff)
}

// resolveSearchPath splits a FIND_FIRST2 wire search path into its directory
// store path and the (store-charset) wildcard pattern of its last element. The
// directory is resolved through the codec; the last element is taken as the
// pattern when it contains a wildcard, else the whole path is the directory and
// the pattern is "*".
//
// A last element with no wildcard is matched exactly against its PARENT's
// listing regardless of whether it names a file or a directory — [MS-CIFS]
// §2.2.6.2 "a search for file(s) within a directory OR FOR A DIRECTORY": a
// bare "\DRIVER" asks "does an entry named DRIVER exist here", answered with
// that one entry (attrs report Directory), not a listing of DRIVER's
// contents. A directory-copy client relies on this to tell files from
// directories without opening them — it opens the returned entry only when
// attrs say file, and recurses with an explicit "\DRIVER\*" otherwise
// (real Windows 98 does exactly this: spec/captures/nwlink-win98.pcap frames
// 182-183, FIND_FIRST2 "\DRIVER" → one entry, "DRIVER", Directory attrs set).
// This function previously special-cased an IsDir leaf to list ITS contents
// instead, which silently swapped in the child names — a copy client reading
// that response never learns "Disk Copy (v4.2)" is a directory at all, and
// then fails trying to open it directly ("Cannot find the specified path").
func (s *Service) resolveSearchPath(sh *Share, wire []byte, flags2 uint16) (dirStore, pattern string, status uint32) {
	raw, _, ok := extractWirePath(wire, flags2)
	if !ok {
		return "", "*", statusSuccess // empty → list share root
	}
	store, err := sh.ResolvePath(raw, flags2)
	if err != nil {
		return "", "", statusObjectNameInvalid
	}
	parent, leaf := storeParent(store)
	return parent, leaf, statusSuccess
}

// listDir reads dirStore and returns the entries matching the wildcard pattern as
// findRows (name, derived short name, info), sorted by ReadDir order. An entry
// whose .LONGNAME EA is set (the OS/2 HPFS convention for a true long name
// over an 8.3 host name) is reported under that name — matched against
// pattern in place of the host name — with the host name demoted to the
// row's short name.
func (s *Service) listDir(sh *Share, dirStore, pattern string) ([]findRow, uint32) {
	entries, err := sh.FS().ReadDir(dirStore)
	if err != nil {
		return nil, statusObjectPathNotFound
	}
	rows := make([]findRow, 0, len(entries))
	for _, e := range entries {
		hostName := e.Name()
		full := hostName
		if dirStore != "" {
			full = dirStore + "/" + hostName
		}
		name := hostName
		short := hostName
		if long := sh.longNameFor(full); long != "" {
			name = long
		} else if sn, err := sh.FS().ShortName(full); err == nil && sn != "" {
			short = sn
		}
		if !wildcardMatch(name, pattern) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		rows = append(rows, findRow{name: name, shortName: short, store: full, info: info})
	}
	return rows, statusSuccess
}

// queryPathInfo serves TRANS2_QUERY_PATH_INFORMATION. Params ([MS-CIFS]
// §2.2.6.6.1): InformationLevel(2) Reserved(4) FileName(SMB_STRING). data is
// the request's Trans2_Data block — the SMB_GEA_LIST name filter when the
// level is SMB_INFO_QUERY_EAS_FROM_LIST, empty otherwise.
func (s *Service) queryPathInfo(sess *smbSession, sh *Share, h protocol.Header, params, data []byte) []byte {
	if len(params) < 6 {
		return errResponse(h, statusUnsuccessful)
	}
	infoLevel := bp.LE16(params[0:2])
	store, st := resolvePath(sh, params[6:], h.Flags2)
	if st != statusSuccess {
		return errResponse(h, st)
	}
	info, err := sh.FS().Stat(store)
	if err != nil {
		return errResponse(h, statusObjectNameNotFound)
	}
	if infoLevel == infoQueryFileName {
		return buildTrans2InfoResponse(sess, h, packFileNameInfo(store))
	}
	if infoLevel == infoQueryAllEAs {
		return buildTrans2InfoResponse(sess, h, packFEAList(sh.EAs(store)))
	}
	if infoLevel == infoQueryEasFromList {
		names, ok := parseGEAList(data)
		if !ok {
			return errResponse(h, statusUnsuccessful)
		}
		return buildTrans2InfoResponse(sess, h, packFEAList(filterEAs(sh.EAs(store), names)))
	}
	out, ok := packQueryInfo(infoLevel, info, sh.AttrsFor(store, info))
	if !ok {
		return errResponse(h, statusNotSupported)
	}
	return buildTrans2InfoResponse(sess, h, out)
}

// queryFileInfo serves TRANS2_QUERY_FILE_INFORMATION. Params ([MS-CIFS]
// §2.2.6.8.1): FID(2) InformationLevel(2). The FID is re-Stat'd live. data is
// the request's Trans2_Data block — the SMB_GEA_LIST name filter when the
// level is SMB_INFO_QUERY_EAS_FROM_LIST, empty otherwise.
func (s *Service) queryFileInfo(sess *smbSession, h protocol.Header, params, data []byte) []byte {
	if len(params) < 4 {
		return errResponse(h, statusUnsuccessful)
	}
	fid := bp.LE16(params[0:2])
	infoLevel := bp.LE16(params[2:4])
	hnd, ok := sess.fileByFID(fid)
	if !ok {
		return errResponse(h, statusInvalidHandle)
	}
	info, err := hnd.share.FS().Stat(hnd.path)
	if err != nil {
		return errResponse(h, statusObjectNameNotFound)
	}
	if infoLevel == infoQueryFileName {
		return buildTrans2InfoResponse(sess, h, packFileNameInfo(hnd.path))
	}
	if infoLevel == infoQueryAllEAs {
		return buildTrans2InfoResponse(sess, h, packFEAList(hnd.share.EAs(hnd.path)))
	}
	if infoLevel == infoQueryEasFromList {
		names, ok := parseGEAList(data)
		if !ok {
			return errResponse(h, statusUnsuccessful)
		}
		return buildTrans2InfoResponse(sess, h, packFEAList(filterEAs(hnd.share.EAs(hnd.path), names)))
	}
	out, ok := packQueryInfo(infoLevel, info, hnd.share.AttrsFor(hnd.path, info))
	if !ok {
		return errResponse(h, statusNotSupported)
	}
	return buildTrans2InfoResponse(sess, h, out)
}

// queryFSInfo serves TRANS2_QUERY_FS_INFORMATION ([smb6.0] 4097; [MS-CIFS]
// §2.2.6.4): "the filesystem is identified by Tid in the SMB header"; the
// 2-byte param block carries the InformationLevel and the response returns the
// level-dependent structure in the Data block with NO parameter bytes
// ([MS-CIFS] §2.2.6.4.2). NT 3.51 issues SMB_QUERY_FS_VOLUME_INFO right after
// opening a share (netbeui.pcap frame 491) and treats an error reply as a
// failed share access, so every level a period client asks for is served.
func (s *Service) queryFSInfo(sh *Share, h protocol.Header, params []byte) []byte {
	if len(params) < 2 {
		return errResponse(h, statusUnsuccessful)
	}
	level := bp.LE16(params[0:2])

	total, free, err := sh.FS().DiskUsage("")
	if err != nil {
		total, free = 0, 0
	}
	const unitBytes = fsBytesPerSector * fsSectorsPerUnit
	totalUnits := total / unitBytes
	freeUnits := free / unitBytes
	if totalUnits == 0 {
		// A backend that cannot report usage still presents a mounted,
		// non-empty volume (matching SMB_COM_QUERY_INFORMATION_DISK).
		totalUnits = 1
	}
	label := sh.Name()

	var data []byte
	switch level {
	case fsInfoAllocation:
		// idFileSystem(4, "NT server always returns 0") cSectorUnit(4) cUnit(4)
		// cUnitAvail(4) cbSector(2) — [smb6.0] 4130.
		data = make([]byte, 18)
		bp.PutLE32(data[4:8], fsSectorsPerUnit)
		bp.PutLE32(data[8:12], clamp32(totalUnits))
		bp.PutLE32(data[12:16], clamp32(freeUnits))
		bp.PutLE16(data[16:18], fsBytesPerSector)
	case fsInfoVolume:
		// ulVsn(4) cch(1) Label(STRING, wire charset) — [smb6.0] 4141. The
		// pre-NT form: Win9x asks this level when CAP_NT_SMBS is off.
		wire, err := sh.EncodeName(label, h.Flags2)
		if err != nil {
			wire = nil
		}
		data = make([]byte, 5+len(wire))
		bp.PutLE32(data[0:4], volumeSerial(label))
		data[4] = byte(len(label))
		copy(data[5:], wire)
	case fsQueryVolumeInfo:
		// FileFsVolumeInformation: VolumeCreationTime FILETIME(8, unknown=0)
		// SerialNumber(4) VolumeLabelSize(4) Reserved(2) VolumeLabel(WCHAR —
		// "the Unicode-encoded volume label", [MS-CIFS] §2.2.8.2.3, regardless
		// of the request charset).
		lab := utf16LEBytes(label)
		data = make([]byte, 18+len(lab))
		bp.PutLE32(data[8:12], volumeSerial(label))
		bp.PutLE32(data[12:16], uint32(len(lab)))
		copy(data[18:], lab)
	case fsQuerySizeInfo:
		// TotalAllocationUnits(8) TotalFreeAllocationUnits(8)
		// SectorsPerAllocationUnit(4) BytesPerSector(4) — [MS-CIFS] §2.2.8.2.4.
		data = make([]byte, 24)
		bp.PutLE64(data[0:8], totalUnits)
		bp.PutLE64(data[8:16], freeUnits)
		bp.PutLE32(data[16:20], fsSectorsPerUnit)
		bp.PutLE32(data[20:24], fsBytesPerSector)
	case fsQueryDeviceInfo:
		// DeviceType(4) DeviceCharacteristics(4) — [MS-CIFS] §2.2.8.2.5.
		data = make([]byte, 8)
		bp.PutLE32(data[0:4], fileDeviceDisk)
		bp.PutLE32(data[4:8], fileDeviceIsMounted)
	case fsQueryAttributeInfo:
		// FileSystemAttributes(4) MaxFileNameLengthInBytes(4)
		// LengthOfFileSystemName(4) FileSystemName(WCHAR, always Unicode) —
		// [MS-CIFS] §2.2.8.2.6. "NTFS" advertises long, case-preserved names
		// (the share seam preserves case and is not 8.3-limited); reporting
		// FAT would make NT-family clients apply 8.3 name rules.
		name := utf16LEBytes("NTFS")
		data = make([]byte, 12+len(name))
		bp.PutLE32(data[0:4], fileCasePreservedNames)
		bp.PutLE32(data[4:8], 255)
		bp.PutLE32(data[8:12], uint32(len(name)))
		copy(data[12:], name)
	default:
		return errResponse(h, statusNotSupported)
	}
	return buildTrans2Response(nil, h, nil, data)
}

// packFileNameInfo serializes SMB_QUERY_FILE_NAME_INFO ([MS-CIFS] §2.2.8.3.9):
// FileNameLength(4) + FileName — "the name of the file in Unicode" (always
// UTF-16LE, independent of the request charset). The name is the '\'-separated
// path from the share root; the root itself is "\".
func packFileNameInfo(store string) []byte {
	name := "\\" + strings.ReplaceAll(store, "/", "\\")
	wire := utf16LEBytes(name)
	buf := make([]byte, 4+len(wire))
	bp.PutLE32(buf[0:4], uint32(len(wire)))
	copy(buf[4:], wire)
	return buf
}

// utf16LEBytes encodes s as UTF-16LE without a terminator — the encoding the
// NT information levels mandate for their strings whatever the negotiated
// wire charset ([smb6.0] 4116: levels above 0x102 map to the
// NtQueryVolumeInformationFile structures).
func utf16LEBytes(s string) []byte {
	units := utf16.Encode([]rune(s))
	out := make([]byte, 2*len(units))
	for i, u := range units {
		bp.PutLE16(out[2*i:2*i+2], u)
	}
	return out
}

// volumeSerial derives a stable volume serial number from the share name
// (FNV-1a). Period clients only require the value to be consistent across
// requests to the same share.
func volumeSerial(name string) uint32 {
	h := uint32(2166136261)
	for i := 0; i < len(name); i++ {
		h ^= uint32(name[i])
		h *= 16777619
	}
	return h
}

// clamp32 caps a count at the 32-bit maximum (the SMB_INFO_ALLOCATION fields).
func clamp32(v uint64) uint32 {
	if v > 0xFFFFFFFF {
		return 0xFFFFFFFF
	}
	return uint32(v)
}

// handleFindClose2 answers SMB_COM_FIND_CLOSE2 (0x34): release a search SID.
// Request words (WCT=1): SID(2). Reply WCT=0.
func (s *Service) handleFindClose2(sess *smbSession, h protocol.Header, req []byte) []byte {
	words, _, ok := reqBody(req)
	if !ok || len(words) < 2 {
		return successNoData(h)
	}
	sess.dropSearch(bp.LE16(words[0:2]))
	return successNoData(h)
}

// --- packing ---

// packQueryInfo serializes a FileInfo into the requested QUERY_*_INFO level, or
// (nil,false) for an unsupported level ([MS-CIFS] §2.2.8.3). attrs is the DOS
// attribute word the caller computed store-aware (Share.AttrsFor), so persisted
// Hidden/System bits the host cannot represent are reported.
func packQueryInfo(level uint16, info stdfs.FileInfo, attrs uint16) ([]byte, bool) {
	switch level {
	case infoStandard, infoQueryEaSize:
		// SMB_INFO_STANDARD / SMB_INFO_QUERY_EA_SIZE ([MS-CIFS] §2.2.8.3.1/
		// §2.2.8.3.2, LANMAN2.0): SMB_DATE/SMB_TIME creation/access/write
		// pairs, FileDataSize(4), AllocationSize(4), Attributes(2), and for
		// the EA_SIZE level a trailing EaSize(4, always 0) — no name, no
		// ResumeKey, unlike the FIND_FIRST2 records in packFindStandard.
		// OS/2 WPS issues TRANS2_QUERY_PATH_INFORMATION at this level to
		// populate folder views; returning statusNotSupported here makes it
		// treat the whole share as inaccessible even though `dir`/`copy`
		// (which never ask for this level) work fine.
		size := 22
		if level == infoQueryEaSize {
			size = 26
		}
		buf := make([]byte, size)
		// smbServerTimeDate returns (smbTime, smbDate) — SMB_DATE/SMB_TIME pairs on
		// the wire are Date-then-Time ([MS-CIFS] §2.2.8.1.1 SMB_INFO_STANDARD), so
		// the two must be swapped here, not passed through in call order (a prior
		// cd,ct := smbServerTimeDate(...) bug packed time bits into the date slot
		// and vice versa — Wireshark decoded the result as e.g. "2046-13-11", an
		// invalid DOS date, on every TRANS2_QUERY_PATH_INFORMATION SMB_INFO_STANDARD
		// reply; netbeui.pcap 2026-07-15 frame 752, `\` root query).
		ct, cd := smbServerTimeDate(info.ModTime())
		for _, off := range []int{0, 4, 8} {
			bp.PutLE16(buf[off:off+2], cd)
			bp.PutLE16(buf[off+2:off+4], ct)
		}
		fsize := fileSize(info)
		bp.PutLE32(buf[12:16], uint32(fsize))
		bp.PutLE32(buf[16:20], uint32(allocSize(fsize, info.IsDir())))
		bp.PutLE16(buf[20:22], attrs)
		return buf, true
	case infoQueryFileBasic:
		buf := make([]byte, 40)
		ft := fileTime(info.ModTime())
		bp.PutLE64(buf[0:8], ft)   // CreationTime
		bp.PutLE64(buf[8:16], ft)  // LastAccessTime
		bp.PutLE64(buf[16:24], ft) // LastWriteTime
		bp.PutLE64(buf[24:32], ft) // ChangeTime
		bp.PutLE32(buf[32:36], uint32(attrs))
		return buf, true
	case infoQueryFileStd:
		buf := make([]byte, 24)
		size := fileSize(info)
		bp.PutLE64(buf[0:8], allocSize(size, info.IsDir()))
		bp.PutLE64(buf[8:16], size)
		bp.PutLE32(buf[16:20], 1) // NumberOfLinks
		if info.IsDir() {
			buf[21] = 1 // Directory
		}
		return buf, true
	case infoQueryFileEA:
		return make([]byte, 4), true // EaSize = 0
	case infoQueryFileAllInfo:
		basic, _ := packQueryInfo(infoQueryFileBasic, info, attrs)
		std, _ := packQueryInfo(infoQueryFileStd, info, attrs)
		ea, _ := packQueryInfo(infoQueryFileEA, info, attrs)
		out := make([]byte, 0, len(basic)+len(std)+len(ea))
		out = append(out, basic...)
		out = append(out, std...)
		return append(out, ea...), true
	default:
		return nil, false
	}
}

// eaDataSize is the EaSize field value ([MS-CIFS] §2.2.8.1.2/§2.2.8.3.2): the
// byte length of a file's EA information, 0 when it has none. Unlike
// packFEAList's wire length, an empty list reports 0 here, not the 4-byte
// SizeOfListInBytes header — EaSize measures the EAs themselves, not a
// container.
func eaDataSize(eas []fs.EA) int {
	if len(eas) == 0 {
		return 0
	}
	return len(packFEAList(eas))
}

// packFEAList renders eas as an SMB_FEA_LIST ([MS-CIFS] §2.2.1.2.2/§2.2.1.2.2.1):
// ULONG SizeOfListInBytes (counts itself) followed by concatenated SMB_FEA
// records — ExtendedAttributeFlag(1) AttributeNameLengthInBytes(1)
// AttributeValueLengthInBytes(2) AttributeName[Len+1, NUL-padded]
// AttributeValue[Len]. Names/values are OEM bytes per spec — EA names are
// ASCII well-known tags (".LONGNAME" etc.), never subject to the client's
// Unicode session flag, so no wire-charset transcoding applies here.
func packFEAList(eas []fs.EA) []byte {
	size := 4
	for _, e := range eas {
		size += 4 + len(e.Name) + 1 + len(e.Value)
	}
	out := make([]byte, size)
	bp.PutLE32(out[0:4], uint32(size))
	off := 4
	for _, e := range eas {
		if e.NeedEA {
			out[off] = 0x80 // FILE_NEED_EA
		}
		out[off+1] = byte(len(e.Name))
		bp.PutLE16(out[off+2:off+4], uint16(len(e.Value)))
		off += 4
		off += copy(out[off:], e.Name)
		off++ // NUL pad after AttributeName, not counted in AttributeNameLengthInBytes
		off += copy(out[off:], e.Value)
	}
	return out
}

// parseFEAList decodes an SMB_FEA_LIST written by a client (TRANS2_SET_PATH/
// FILE_INFORMATION SMB_INFO_SET_EAS). errOffset is the byte offset of the
// first malformed SMB_FEA record when ok is false, for EaErrorOffset
// ([MS-CIFS] §2.2.6.7.2).
func parseFEAList(b []byte) (eas []fs.EA, ok bool, errOffset uint16) {
	if len(b) < 4 {
		return nil, false, 0
	}
	total := int(bp.LE32(b[0:4]))
	if total < 4 || total > len(b) {
		total = len(b)
	}
	off := 4
	for off < total {
		if off+4 > total {
			return nil, false, uint16(off)
		}
		needEA := b[off]&0x80 != 0
		nameLen := int(b[off+1])
		valueLen := int(bp.LE16(b[off+2 : off+4]))
		rec := off
		off += 4
		if off+nameLen+1+valueLen > total {
			return nil, false, uint16(rec)
		}
		name := string(b[off : off+nameLen])
		off += nameLen + 1 // skip the NUL pad byte
		value := append([]byte(nil), b[off:off+valueLen]...)
		off += valueLen
		eas = append(eas, fs.EA{Name: name, Value: value, NeedEA: needEA})
	}
	return eas, true, 0
}

// parseGEAList decodes an SMB_GEA_LIST ([MS-CIFS] §2.2.1.2.1) — the
// SMB_INFO_QUERY_EAS_FROM_LIST request's name filter: ULONG SizeOfListInBytes
// (counting itself) then SMB_GEA entries, each AttributeNameLengthInBytes(1)
// AttributeName[len] NUL(1). Returns the requested names in request order. An
// EMPTY block parses as no names (ok, nil) — the lenient path for a client
// that sent the level without its list — while a present-but-torn list is
// rejected.
func parseGEAList(b []byte) (names []string, ok bool) {
	if len(b) == 0 {
		return nil, true
	}
	if len(b) < 4 {
		return nil, false
	}
	total := int(bp.LE32(b[0:4]))
	if total < 4 || total > len(b) {
		total = len(b)
	}
	off := 4
	for off < total {
		nameLen := int(b[off])
		if off+1+nameLen+1 > total {
			return nil, false
		}
		names = append(names, string(b[off+1:off+1+nameLen]))
		off += 1 + nameLen + 1 // length byte + name + NUL
	}
	return names, true
}

// filterEAs returns one FEA entry per requested GEA name, in request order —
// SMB_INFO_QUERY_EAS_FROM_LIST returns "pairs where the AttributeName field
// values match those that were provided in the request" ([MS-CIFS] §2.2.8.3.3),
// NOT the file's whole list. The match is case-insensitive (OS/2 EA names are
// caseless, uppercase by convention). A requested name with no stored EA still
// contributes a zero-length placeholder entry, not nothing: the real IBM Peer
// server (captures/ibm-peer-clients.pcapng frames 505/507 and 1428/1432, real
// OS/2-to-OS/2 traffic) always answers with one FEA record per requested name,
// EA Data Length 0 for ones the file doesn't have — WPS's own GetEAList
// consumer expects the response positionally keyed to its request, not a
// variably-shorter list. Honouring the *name* filter (not returning EAs the
// client didn't ask for) is still load-bearing: OS/2 WPS probes files one name
// at a time (.ICON1, .SUBJECT — netbeui.pcap 2026-07-14 frame 334) with a
// 4356-byte client buffer, so returning an unrequested multi-KB .ICON would
// overflow every probe once an icon is stored.
func filterEAs(eas []fs.EA, names []string) []fs.EA {
	var out []fs.EA
	for _, n := range names {
		found := false
		for _, e := range eas {
			if strings.EqualFold(e.Name, n) {
				out = append(out, e)
				found = true
				break
			}
		}
		if !found {
			out = append(out, fs.EA{Name: n})
		}
	}
	return out
}

// supportedFindLevel reports whether a FIND_FIRST2/FIND_NEXT2 information level
// is one the packers below can encode.
func supportedFindLevel(level uint16) bool {
	return level == infoStandard || level == infoQueryEaSize || level == infoQueryEasFromList || level == infoFileBothDirInfo
}

// packFindEntriesBudget packs up to maxEntries findRows at the requested information
// level: the NT SMB_FIND_FILE_BOTH_DIRECTORY_INFO or the pre-NT LANMAN2.0 levels
// (SMB_INFO_STANDARD / SMB_INFO_QUERY_EA_SIZE) that OS/2 LAN Server and DOS LANMAN
// redirectors ask for (netbeui.pcap 2026-07-10 frames 308/316 — rejecting them leaves
// OS/2 unable even to read its message file → SYS0318).
//
// maxBytes is an explicit data-block BYTE budget: packing stops once the accumulated
// records would exceed it (0 = no byte cap, entry-count-only). A connectionless
// transport (direct SMB over IPX) has no reply reassembly, so the whole FIND data block
// must fit one datagram; the client's MaxDataCount ([MS-CIFS] §2.2.4.46.1) drives this
// cap and the client pages the rest via FIND_NEXT2. A stream transport (TCP/NBT) sends
// MaxDataCount 0xFFFF, which yields no byte cap, leaving the single-message behaviour
// unchanged. At least one record is always packed (a lone over-budget entry still goes
// out) so a search never stalls with zero progress.
func packFindEntriesBudget(sh *Share, rows []findRow, maxEntries, maxBytes int, infoLevel uint16, resumeKeys bool, geaNames []string, flags2 uint16) (data []byte, returned int, lastNameOffset uint16) {
	if infoLevel == infoFileBothDirInfo {
		return packFindBothDir(sh, rows, maxEntries, maxBytes, flags2)
	}
	return packFindStandard(sh, rows, maxEntries, maxBytes, infoLevel, resumeKeys, geaNames, flags2)
}

// findDataBudget converts a request's MaxDataCount into the byte budget the FIND
// packer fills, reserving headroom for the TRANS2 response's parameter block and
// fixed framing so the WHOLE assembled message — not just the data block — fits the
// client's advertised buffer (and, over IPX, one datagram). A zero or absurdly large
// MaxDataCount (the 0xFFFF a reassembling TCP/NBT client sends) yields 0 = "no byte
// cap", preserving the single-message reply on stream transports.
func findDataBudget(maxData int) int {
	if maxData <= 0 || maxData >= 0xFFFF {
		return 0 // no cap: entry-count-only (stream transport / unbounded client)
	}
	const trans2ReplyOverhead = 64 // header + WCT/words/BCC + param block + pad slack
	budget := maxData - trans2ReplyOverhead
	if budget < 1 {
		budget = 1
	}
	return budget
}

// packFindBothDir packs findRows as SMB_FIND_FILE_BOTH_DIRECTORY_INFO records
// ([MS-CIFS] §2.2.8.1.7): a 94-byte fixed area then the long file name in the
// request wire charset, each record 4-byte aligned via NextEntryOffset (0 on
// the last). The name carries a NUL terminator; on a non-Unicode session its
// one byte IS counted in FileNameLength ([MS-CIFS] <167>/<168> — NT servers do
// this and the NT 3.51 redirector expects it), on a Unicode session the two
// NUL bytes are uncounted padding. ShortName is the 8.3 alternate name, ALWAYS
// UTF-16LE regardless of session charset ("in Unicode format", §2.2.8.1.7),
// and length 0 when no distinct valid 8.3 name exists. Returns the data block,
// the count packed, and the offset of the last record's FileName field (the
// resume hint).
func packFindBothDir(sh *Share, rows []findRow, maxEntries, maxBytes int, flags2 uint16) (data []byte, returned int, lastNameOffset uint16) {
	out := make([]byte, 0, 128)
	for i := 0; i < len(rows) && returned < maxEntries; i++ {
		row := rows[i]
		nameWire, err := sh.EncodeName(row.name, flags2)
		if err != nil {
			continue // a name the wire charset cannot represent is skipped, not fatal
		}
		term := 1 // NUL terminator width in the wire charset
		nameLenField := len(nameWire) + 1
		if flags2&protocol.Flags2Unicode != 0 {
			term = 2
			nameLenField = len(nameWire)
		}
		shortWire := shortNameUTF16(row.name, row.shortName)

		const fixed = 94
		recLen := fixed + len(nameWire) + term
		pad := (4 - recLen%4) % 4
		// Byte budget: stop before a record that would overflow the client's
		// MaxDataCount, but always pack at least one (a lone over-budget entry still
		// goes out, so a search never stalls). The client pages the rest via FIND_NEXT2.
		if maxBytes > 0 && returned > 0 && len(out)+recLen+pad > maxBytes {
			break
		}
		recStart := len(out)
		last := i == len(rows)-1 || returned == maxEntries-1
		next := uint32(recLen + pad)
		if last {
			next = 0
		}

		rec := make([]byte, recLen+pad)
		bp.PutLE32(rec[0:4], next)
		ft := fileTime(row.info.ModTime())
		bp.PutLE64(rec[8:16], ft)
		bp.PutLE64(rec[16:24], ft)
		bp.PutLE64(rec[24:32], ft)
		bp.PutLE64(rec[32:40], ft)
		size := fileSize(row.info)
		bp.PutLE64(rec[40:48], size)
		bp.PutLE64(rec[48:56], allocSize(size, row.info.IsDir()))
		bp.PutLE32(rec[56:60], uint32(sh.AttrsFor(row.store, row.info)))
		bp.PutLE32(rec[60:64], uint32(nameLenField))
		rec[68] = byte(len(shortWire))
		copy(rec[70:94], shortWire)
		copy(rec[94:], nameWire)

		out = append(out, rec...)
		lastNameOffset = uint16(recStart + fixed)
		returned++
	}
	// If a byte-budget or entry-count stop left the final packed record with a
	// non-zero NextEntryOffset (it was not the row-list's last), clear it so the
	// client's record walk terminates cleanly at the end of this batch.
	if returned > 0 {
		bp.PutLE32(out[uint32(lastNameOffset)-94:uint32(lastNameOffset)-90], 0)
	}
	return out, returned, lastNameOffset
}

// packFindStandard packs findRows as SMB_INFO_STANDARD or (withEA)
// SMB_INFO_QUERY_EA_SIZE / SMB_INFO_QUERY_EAS_FROM_LIST records ([MS-CIFS]
// §2.2.8.1.1/§2.2.8.1.2/§2.2.8.1.3): optional ResumeKey(4, present only when
// SMB_FIND_RETURN_RESUME_KEYS was set in the request Flags), SMB_DATE/SMB_TIME
// creation/access/write pairs, FileDataSize(4), AllocationSize(4),
// Attributes(2), then for EA_SIZE a plain EaSize(4) or for EAS_FROM_LIST an
// SMB_FEA_LIST (ULONG SizeOfListInBytes, ≥4 even when empty, since the field
// counts itself — a bare 0 there reads as a truncated/invalid list), then
// FileNameLength(1) and the name in the request wire charset followed by a
// NUL terminator NOT counted in FileNameLength ([MS-CIFS] <153>/<154>).
// Records are packed back to back with no alignment. EaSize
// (SMB_INFO_QUERY_EA_SIZE) is a plain 4-byte length over ALL of a file's EAs;
// EAS_FROM_LIST embeds the FULL SMB_FEA_LIST (SizeOfListInBytes + the SMB_FEA
// records themselves, [MS-CIFS] §2.2.8.1.3 — unlike the QUERY_PATH/FILE_INFO
// EAS_FROM_LIST level, which is size-only), filtered to the geaNames the
// request's SMB_GEA_LIST asked for (§2.2.8.1.3 returns only the requested
// names). Both draw on the share's real stored EAs (Share.EAs).
func packFindStandard(sh *Share, rows []findRow, maxEntries, maxBytes int, infoLevel uint16, resumeKeys bool, geaNames []string, flags2 uint16) (data []byte, returned int, lastNameOffset uint16) {
	withEaSize := infoLevel == infoQueryEaSize
	withFeaList := infoLevel == infoQueryEasFromList
	out := make([]byte, 0, 128)
	for i := 0; i < len(rows) && returned < maxEntries; i++ {
		row := rows[i]
		nameWire, err := sh.EncodeName(row.name, flags2)
		if err != nil || len(nameWire) > 255 {
			continue // a name the wire charset cannot represent is skipped, not fatal
		}
		term := 1
		if flags2&protocol.Flags2Unicode != 0 {
			term = 2
		}

		var feaList []byte
		var eas []fs.EA
		if withEaSize || withFeaList {
			eas = sh.EAs(row.store)
		}
		if withFeaList {
			feaList = packFEAList(filterEAs(eas, geaNames))
		}

		fixed := 23 // dates/times(12) + FileDataSize(4) + AllocationSize(4) + Attributes(2) + FileNameLength(1)
		if resumeKeys {
			fixed += 4
		}
		if withEaSize {
			fixed += 4 // EaSize: a plain length, not the FEA records themselves
		} else if withFeaList {
			fixed += len(feaList)
		}
		// Byte budget: stop before a record that would overflow the client's
		// MaxDataCount, but always pack at least one so a search never stalls; the
		// client pages the rest via FIND_NEXT2 (see packFindEntriesBudget).
		if maxBytes > 0 && returned > 0 && len(out)+fixed+len(nameWire)+term > maxBytes {
			break
		}
		recStart := len(out)
		rec := make([]byte, fixed+len(nameWire)+term)

		off := 0
		if resumeKeys {
			bp.PutLE32(rec[0:4], uint32(i+1)) // opaque server key; FIND_NEXT2 resumes from the snapshot
			off = 4
		}
		st, sd := smbServerTimeDate(row.info.ModTime())
		for _, fo := range []int{off, off + 4, off + 8} { // creation, access, write — all ModTime
			bp.PutLE16(rec[fo:fo+2], sd)
			bp.PutLE16(rec[fo+2:fo+4], st)
		}
		size := fileSize(row.info)
		bp.PutLE32(rec[off+12:off+16], clamp32(size))
		bp.PutLE32(rec[off+16:off+20], clamp32(allocSize(size, row.info.IsDir())))
		bp.PutLE16(rec[off+20:off+22], sh.AttrsFor(row.store, row.info))
		nameLenOff := off + 22
		switch {
		case withEaSize:
			bp.PutLE32(rec[off+22:off+26], uint32(eaDataSize(eas)))
			nameLenOff = off + 26
		case withFeaList:
			copy(rec[off+22:off+22+len(feaList)], feaList)
			nameLenOff = off + 22 + len(feaList)
		}
		rec[nameLenOff] = byte(len(nameWire))
		copy(rec[nameLenOff+1:], nameWire)

		out = append(out, rec...)
		lastNameOffset = uint16(recStart + fixed)
		returned++
	}
	return out, returned, lastNameOffset
}

// shortNameUTF16 encodes a row's derived 8.3 alternate name for the BOTH_DIR
// ShortName field: UTF-16LE, uppercase, at most 24 bytes. A short name that is
// absent, identical to the long name, or not a valid 8.3 name yields nil —
// ShortNameLength 0 means "no 8.3 name is present" ([MS-CIFS] §2.2.8.1.7).
func shortNameUTF16(name, short string) []byte {
	if short == "" || strings.EqualFold(short, name) || !is8dot3(short) {
		return nil
	}
	b := utf16LEBytes(strings.ToUpper(short))
	if len(b) > 24 {
		return nil
	}
	return b
}

// is8dot3 reports whether name fits the DOS 8.3 form: 1-8 character base, at
// most one dot, 0-3 character extension, ASCII with none of the characters DOS
// reserves.
func is8dot3(name string) bool {
	base, ext, hasDot := strings.Cut(name, ".")
	if base == "" || len(base) > 8 || (hasDot && (ext == "" || len(ext) > 3)) {
		return false
	}
	for _, r := range name {
		if r <= ' ' || r > '~' || strings.ContainsRune(`+,;=[]*?/\:"<>|`, r) {
			return false
		}
	}
	return strings.Count(name, ".") <= 1
}

// fileSize returns a FileInfo's byte size, 0 for a directory.
func fileSize(info stdfs.FileInfo) uint64 {
	if info.IsDir() {
		return 0
	}
	return uint64(info.Size())
}

// clampSearchCount bounds a requested batch size to [1, 256].
func clampSearchCount(n int) int {
	if n <= 0 {
		return 1
	}
	if n > 256 {
		return 256
	}
	return n
}

// buildFindResponse encodes a FIND_FIRST2 / FIND_NEXT2 reply. FIND_FIRST2 prepends
// a 2-byte SID (10-byte param block); FIND_NEXT2 omits it (8-byte). The param
// block is SID? + SearchCount(2) + EndOfSearch(2) + EaErrorOffset(2) +
// LastNameOffset(2); the data block holds the packed records.
func buildFindResponse(sess *smbSession, h protocol.Header, includeSID bool, sid uint16, count int, endOfSearch bool, data []byte, lastNameOffset uint16) []byte {
	paramLen := 8
	if includeSID {
		paramLen = 10
	}
	p := make([]byte, paramLen)
	off := 0
	if includeSID {
		bp.PutLE16(p[0:2], sid)
		off = 2
	}
	bp.PutLE16(p[off:off+2], uint16(count))
	if endOfSearch {
		bp.PutLE16(p[off+2:off+4], 1)
	}
	bp.PutLE16(p[off+6:off+8], lastNameOffset)
	return buildTrans2Response(sess, h, p, data)
}

// buildTrans2InfoResponse builds a TRANS2 reply with a 2-byte EaErrorOffset param
// and the supplied info-level data block (QUERY_PATH/FILE_INFO).
func buildTrans2InfoResponse(sess *smbSession, h protocol.Header, data []byte) []byte {
	return buildTrans2Response(sess, h, make([]byte, 2), data)
}

// buildTrans2Response frames an SMB_COM_TRANSACTION2 response (WCT=10) carrying a
// parameter block and a data block, each at its own header-relative offset
// ([MS-CIFS] §2.2.4.46.2). When the assembled message would exceed sess's
// maxBufferSize, the reply is split into multiple TRANS2 response messages —
// the first is returned directly (as every caller already expects) and any
// further fragments are queued on sess for ServeMessage to deliver over the
// push channel right after ([MS-CIFS] §2.2.4.46.2's DataDisplacement/
// ParameterDisplacement reassembly; confirmed as real server behaviour,
// independent of the client's offered MaxDataCount, against
// captures/ibm-peer-clients.pcapng frames 633/637/641 — a live IBM Peer server
// split a 4797-byte FIND_FIRST2 EAS_FROM_LIST reply into two TRANS2 response
// messages at its own ~4356-byte outbound buffer limit). sess may be nil (some
// callers have none, e.g. tests exercising the framer directly); a nil sess
// always sends the whole reply in one message, matching the pre-chunking
// behaviour.
func buildTrans2Response(sess *smbSession, h protocol.Header, params, data []byte) []byte {
	wordsLen := 20
	paramOffset := protocol.HeaderLen + 1 + wordsLen + 2
	paramPad := (2 - paramOffset%2) % 2
	firstParamOffset := paramOffset + paramPad

	limit := uint32(0)
	if sess != nil {
		limit = sess.maxBufferSize()
	}
	total := firstParamOffset + len(params) + 2 + len(data) // rough whole-message size, ignoring inter-block padding
	if limit == 0 || uint32(total) <= limit {
		return trans2ResponseFragment(h, uint16(len(params)), uint16(len(data)), params, 0, data, 0)
	}

	// Chunk: fill each fragment's data area up to the budget left after its
	// parameter slice and fixed overhead, draining params first (matching the
	// real IBM Peer server's frame 637: full params + a partial data block in
	// the first fragment) then data-only continuations.
	const fixedOverhead = 64 // header + WCT/words/BCC + pad slack, conservative
	frames := make([][]byte, 0, 2)
	paramSent, dataSent := 0, 0
	for paramSent < len(params) || dataSent < len(data) || (paramSent == 0 && dataSent == 0) {
		budget := int(limit) - fixedOverhead
		if budget < 1 {
			budget = 1
		}
		paramChunk := params[paramSent:]
		if len(paramChunk) > budget {
			paramChunk = paramChunk[:budget]
		}
		budget -= len(paramChunk)
		dataChunk := data[dataSent:]
		if budget > 0 {
			if len(dataChunk) > budget {
				dataChunk = dataChunk[:budget]
			}
		} else {
			dataChunk = nil
		}
		frame := trans2ResponseFragment(h, uint16(len(params)), uint16(len(data)), paramChunk, uint16(paramSent), dataChunk, uint16(dataSent))
		frames = append(frames, frame)
		paramSent += len(paramChunk)
		dataSent += len(dataChunk)
		if len(paramChunk) == 0 && len(dataChunk) == 0 {
			break // budget too small to make progress — stop rather than loop forever
		}
	}

	if sess != nil {
		for _, f := range frames[1:] {
			sess.queueContinuation(f)
		}
	}
	return frames[0]
}

// trans2ResponseFragment builds one SMB_COM_TRANSACTION2 response message
// carrying a slice of the total parameter/data blocks at their displacement
// ([MS-CIFS] §2.2.4.46.2).
func trans2ResponseFragment(h protocol.Header, totalParams, totalData uint16, params []byte, paramDisp uint16, data []byte, dataDisp uint16) []byte {
	rh := responseHeader(h, statusSuccess)
	out := rh.Encode(nil)
	out = append(out, 10) // WCT

	wordsLen := 20
	paramOffset := protocol.HeaderLen + 1 + wordsLen + 2
	paramPad := (2 - paramOffset%2) % 2
	paramOffset += paramPad
	dataOffset := paramOffset + len(params)
	dataPad := (2 - dataOffset%2) % 2
	dataOffset += dataPad

	w := make([]byte, wordsLen)
	bp.PutLE16(w[0:2], totalParams)          // TotalParameterCount
	bp.PutLE16(w[2:4], totalData)            // TotalDataCount
	bp.PutLE16(w[6:8], uint16(len(params)))  // ParameterCount
	bp.PutLE16(w[8:10], uint16(paramOffset)) // ParameterOffset
	bp.PutLE16(w[10:12], paramDisp)          // ParameterDisplacement
	bp.PutLE16(w[12:14], uint16(len(data)))  // DataCount
	bp.PutLE16(w[14:16], uint16(dataOffset)) // DataOffset
	bp.PutLE16(w[16:18], dataDisp)           // DataDisplacement
	out = append(out, w...)

	bcc := paramPad + len(params) + dataPad + len(data)
	out = append(out, byte(bcc), byte(bcc>>8))
	for i := 0; i < paramPad; i++ {
		out = append(out, 0)
	}
	out = append(out, params...)
	for i := 0; i < dataPad; i++ {
		out = append(out, 0)
	}
	out = append(out, data...)
	return out
}
