package smb

import (
	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"

	protocol "github.com/ObsoleteMadness/ClassicStack/core/protocol/smb"
)

// --- SMB1 path operations over the §9 share seam: DELETE / RENAME /
// CREATE_DIRECTORY / DELETE_DIRECTORY / CHECK_DIRECTORY and the
// QUERY_INFORMATION[_DISK] queries. Each resolves its wire path through the share
// codec and acts via sh.FS(); RENAME/DELETE ride the metadata-carrying
// FS().Rename/Remove (core/fs §9), so SMB never pairs MoveMetadata/DeleteMetadata
// itself. Wildcards in a path op are refused (STATUS_OBJECT_NAME_INVALID) — only
// the TRANS2 find path expands them. ---

// readOnly reports whether the share refuses writes (its FS Capabilities mark it
// read-only). A mutating command on a read-only share is STATUS_ACCESS_DENIED.
func readOnly(sh *Share) bool { return sh.FS().Capabilities().ReadOnly }

// handleDelete answers SMB_COM_DELETE (0x06): remove a regular file. Request
// words (WCT=1): SearchAttributes(2). Reply WCT=0.
func (s *Service) handleDelete(sess *smbSession, h protocol.Header, req []byte) []byte {
	sh, st := s.treeFor(sess, h)
	if st != statusSuccess {
		return errResponse(h, st)
	}
	if readOnly(sh) {
		return errResponse(h, statusAccessDenied)
	}
	_, area, ok := reqBody(req)
	if !ok {
		return errResponse(h, statusObjectNameInvalid)
	}
	store, st := resolvePath(sh, area, h.Flags2)
	if st != statusSuccess {
		return errResponse(h, st)
	}
	info, err := sh.FS().Stat(store)
	if err != nil {
		return errResponse(h, statusObjectNameNotFound)
	}
	if info.IsDir() {
		return errResponse(h, statusFileIsADirectory)
	}
	if err := sh.FS().Remove(store); err != nil {
		return errResponse(h, mapFSErr(err))
	}
	return successNoData(h)
}

// handleRename answers SMB_COM_RENAME (0x07): move OldFileName to NewFileName.
// Request words (WCT=1): SearchAttributes(2). The byte area carries two
// buffer-format-prefixed names. Reply WCT=0.
func (s *Service) handleRename(sess *smbSession, h protocol.Header, req []byte) []byte {
	sh, st := s.treeFor(sess, h)
	if st != statusSuccess {
		return errResponse(h, st)
	}
	if readOnly(sh) {
		return errResponse(h, statusAccessDenied)
	}
	_, area, ok := reqBody(req)
	if !ok {
		return errResponse(h, statusObjectNameInvalid)
	}
	oldRaw, consumed, ok := extractWirePath(area, h.Flags2)
	if !ok {
		return errResponse(h, statusObjectNameInvalid)
	}
	newRaw, _, ok := extractWirePath(area[consumed:], h.Flags2)
	if !ok {
		return errResponse(h, statusObjectNameInvalid)
	}
	oldStore, err := sh.ResolvePath(oldRaw, h.Flags2)
	if err != nil {
		return errResponse(h, statusObjectNameInvalid)
	}
	newStore, err := sh.ResolvePath(newRaw, h.Flags2)
	if err != nil {
		return errResponse(h, statusObjectNameInvalid)
	}
	if _, err := sh.FS().Stat(oldStore); err != nil {
		return errResponse(h, statusObjectNameNotFound)
	}
	if err := sh.FS().Rename(oldStore, newStore); err != nil {
		return errResponse(h, mapFSErr(err))
	}
	return successNoData(h)
}

// handleCreateDirectory answers SMB_COM_CREATE_DIRECTORY (0x00). Request words
// (WCT=0); the byte area carries the directory path. Creating an existing
// directory is idempotent success; an existing file collides. Reply WCT=0.
func (s *Service) handleCreateDirectory(sess *smbSession, h protocol.Header, req []byte) []byte {
	sh, st := s.treeFor(sess, h)
	if st != statusSuccess {
		return errResponse(h, st)
	}
	if readOnly(sh) {
		return errResponse(h, statusAccessDenied)
	}
	_, area, ok := reqBody(req)
	if !ok {
		return errResponse(h, statusObjectNameInvalid)
	}
	store, st := resolvePath(sh, area, h.Flags2)
	if st != statusSuccess {
		return errResponse(h, st)
	}
	if info, err := sh.FS().Stat(store); err == nil {
		if info.IsDir() {
			return successNoData(h) // idempotent mkdir
		}
		return errResponse(h, statusObjectNameCollision)
	}
	if err := sh.FS().CreateDir(store); err != nil {
		// A concurrent create that lost the Stat race surfaces as an exists
		// collision; mkdir is idempotent, so treat it as success.
		if mapFSErr(err) == statusObjectNameCollision {
			return successNoData(h)
		}
		return errResponse(h, mapFSErr(err))
	}
	return successNoData(h)
}

// handleDeleteDirectory answers SMB_COM_DELETE_DIRECTORY (0x01): remove an empty
// directory. Request words (WCT=0). Reply WCT=0.
func (s *Service) handleDeleteDirectory(sess *smbSession, h protocol.Header, req []byte) []byte {
	sh, st := s.treeFor(sess, h)
	if st != statusSuccess {
		return errResponse(h, st)
	}
	if readOnly(sh) {
		return errResponse(h, statusAccessDenied)
	}
	_, area, ok := reqBody(req)
	if !ok {
		return errResponse(h, statusObjectNameInvalid)
	}
	store, st := resolvePath(sh, area, h.Flags2)
	if st != statusSuccess {
		return errResponse(h, st)
	}
	info, err := sh.FS().Stat(store)
	if err != nil {
		return errResponse(h, statusObjectNameNotFound)
	}
	if !info.IsDir() {
		return errResponse(h, statusNotADirectory)
	}
	if entries, err := sh.FS().ReadDir(store); err == nil && len(entries) > 0 {
		return errResponse(h, statusDirectoryNotEmpty)
	}
	if err := sh.FS().Remove(store); err != nil {
		return errResponse(h, mapFSErr(err))
	}
	return successNoData(h)
}

// handleCheckDirectory answers SMB_COM_CHECK_DIRECTORY (0x10): verify a path is a
// directory (the client's chdir probe). Request words (WCT=0). The empty path is
// the share root, always a directory. Reply WCT=0.
func (s *Service) handleCheckDirectory(sess *smbSession, h protocol.Header, req []byte) []byte {
	sh, st := s.treeFor(sess, h)
	if st != statusSuccess {
		return errResponse(h, st)
	}
	_, area, ok := reqBody(req)
	if !ok {
		return errResponse(h, statusObjectPathNotFound)
	}
	store, st := resolvePath(sh, area, h.Flags2)
	if st != statusSuccess {
		return errResponse(h, st)
	}
	if store == "" {
		return successNoData(h) // share root
	}
	info, err := sh.FS().Stat(store)
	if err != nil {
		return errResponse(h, statusObjectPathNotFound)
	}
	if !info.IsDir() {
		return errResponse(h, statusNotADirectory)
	}
	return successNoData(h)
}

// handleQueryInformation answers SMB_COM_QUERY_INFORMATION (0x08), the CORE
// attribute query. Request words (WCT=0); the byte area carries the path. Reply
// WCT=10: FileAttributes(2) LastWriteTime(4) FileSize(4) Reserved[5*2].
func (s *Service) handleQueryInformation(sess *smbSession, h protocol.Header, req []byte) []byte {
	sh, st := s.treeFor(sess, h)
	if st != statusSuccess {
		return errResponse(h, st)
	}
	_, area, ok := reqBody(req)
	if !ok {
		return errResponse(h, statusObjectNameNotFound)
	}
	store, st := resolvePath(sh, area, h.Flags2)
	if st != statusSuccess {
		return errResponse(h, st)
	}
	info, err := sh.FS().Stat(store)
	if err != nil {
		return errResponse(h, statusObjectNameNotFound)
	}
	w := make([]byte, 20)
	bp.PutLE16(w[0:2], dosAttrs(info))
	bp.PutLE32(w[2:6], 0) // DOS LastWriteTime — 0 / unknown
	if !info.IsDir() {
		bp.PutLE32(w[6:10], uint32(info.Size()))
	}
	return reply(h, statusSuccess, 10, w, nil)
}

// handleQueryInformationDisk answers SMB_COM_QUERY_INFORMATION_DISK (0x80): the
// share's free/total space, reported as FAT-style allocation units. Reply WCT=5:
// TotalUnits(2) BlocksPerUnit(2) BlockSize(2) FreeUnits(2) Reserved(2).
func (s *Service) handleQueryInformationDisk(sess *smbSession, h protocol.Header, req []byte) []byte {
	_ = req
	sh, st := s.treeFor(sess, h)
	if st != statusSuccess {
		return errResponse(h, st)
	}
	total, free, err := sh.FS().DiskUsage("")
	if err != nil {
		return errResponse(h, statusUnsuccessful)
	}
	// Report 512-byte blocks, 64 blocks/unit (32 KiB units), clamped to the
	// 16-bit unit fields. A backend that returns 0/0 (unknown) reports a single
	// nominal unit so the client sees a mounted, non-empty volume.
	const blockSize, blocksPerUnit = 512, 64
	const unitBytes = blockSize * blocksPerUnit
	totalUnits := total / unitBytes
	freeUnits := free / unitBytes
	if totalUnits == 0 {
		totalUnits = 1
	}
	w := make([]byte, 10)
	bp.PutLE16(w[0:2], clamp16(totalUnits))
	bp.PutLE16(w[2:4], blocksPerUnit)
	bp.PutLE16(w[4:6], blockSize)
	bp.PutLE16(w[6:8], clamp16(freeUnits))
	return reply(h, statusSuccess, 5, w, nil)
}

// clamp16 caps a count at the 16-bit maximum (the legacy disk-info fields).
func clamp16(v uint64) uint16 {
	if v > 0xFFFF {
		return 0xFFFF
	}
	return uint16(v)
}
