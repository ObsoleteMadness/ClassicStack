package smb

import (
	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"

	protocol "github.com/ObsoleteMadness/ClassicStack/core/protocol/smb"
)

// --- SMB_COM_LOCKING_ANDX (0x24): byte-range locking. A client that opens a
// file for shared write (Excel, Access, many DOS databases) locks a region
// before writing and expects the server to refuse an overlapping lock held by a
// different PID/FID. We model this in-memory per session, keyed by store path so
// two FIDs on the same file within one session still conflict, and release a
// FID's locks on CLOSE (including the LOCKING_ANDX-chained CLOSE) and on session
// teardown. Oplock-only requests (no ranges, an oplock-break ack) succeed
// without state change. Ported from the legacy service/smb command_locking.go. ---

// statusLockNotGranted is STATUS_LOCK_NOT_GRANTED ([MS-ERREF]); returned when a
// requested range overlaps a lock held by another PID/FID, or an unlock names a
// range that is not held.
const statusLockNotGranted uint32 = 0xC0000055

// lockRange is one byte range from a LOCKING_ANDX request: the owning PID plus
// the region offset and length.
type lockRange struct {
	pid    uint16
	start  int64
	length int64
}

// lockEntry is one granted byte-range lock in a lockTable.
type lockEntry struct {
	fid    uint16
	pid    uint16
	start  int64
	length int64
}

// lockTable holds the granted byte-range locks for one store path. It is guarded
// by the owning session's mutex (sess.mu), so it carries no lock of its own.
type lockTable struct {
	locks []lockEntry
}

// lockingAndXRequest is the parsed LOCKING_ANDX word/data block.
type lockingAndXRequest struct {
	andxCommand byte
	andxOffset  uint16
	fid         uint16
	unlocks     []lockRange
	locks       []lockRange
}

// handleLockingAndX answers SMB_COM_LOCKING_ANDX (0x24). Request words (WCT=8):
// AndXCommand(1) AndXReserved(1) AndXOffset(2) FID(2) LockType(1) OplockLevel(1)
// Timeout(4) NumberOfUnlocks(2) NumberOfLocks(2); the byte area carries the
// unlock ranges then the lock ranges, each Pid(2) Offset(4) Length(4). A chained
// CLOSE (the common Win9x "unlock and close" idiom) is honoured. Reply WCT=2
// (AndXCommand=0xFF, AndXOffset=0).
func (s *Service) handleLockingAndX(sess *smbSession, h protocol.Header, req []byte) []byte {
	sh, st := s.treeFor(sess, h)
	if st != statusSuccess {
		return errResponse(h, st)
	}
	_ = sh

	lreq, ok := parseLockingAndX(req, protocol.HeaderLen)
	if !ok {
		return errResponse(h, statusNotSupported)
	}
	if st := s.applyLockingAndX(sess, lreq.fid, lreq.unlocks, lreq.locks); st != statusSuccess {
		return errResponse(h, st)
	}

	// Honour a chained CLOSE (AndXCommand == SMB_COM_CLOSE): Win9x sends
	// LOCKING_ANDX → CLOSE to unlock-and-close in one round trip.
	if lreq.andxCommand == protocol.CommandClose {
		if fid, ok := parseChainedCloseFID(req, int(lreq.andxOffset)); ok {
			sess.closeFID(fid)
		}
	}
	return buildLockingAndXResponse(h)
}

// parseLockingAndX decodes a LOCKING_ANDX block at the given header-relative
// offset (offset == HeaderLen for the primary command).
func parseLockingAndX(req []byte, off int) (lockingAndXRequest, bool) {
	if off < protocol.HeaderLen || off >= len(req) {
		return lockingAndXRequest{}, false
	}
	wct := int(req[off])
	if wct < 8 {
		return lockingAndXRequest{}, false
	}
	wStart := off + 1
	bccOff := wStart + 2*wct
	if bccOff+2 > len(req) {
		return lockingAndXRequest{}, false
	}
	w := req[wStart:bccOff]
	bcc := int(bp.LE16(req[bccOff : bccOff+2]))
	dataOff := bccOff + 2
	if dataOff+bcc > len(req) {
		return lockingAndXRequest{}, false
	}
	area := req[dataOff : dataOff+bcc]

	numUnlocks := int(bp.LE16(w[12:14]))
	numLocks := int(bp.LE16(w[14:16]))
	unlocks, locks, ok := parseLockRanges(area, numUnlocks, numLocks)
	if !ok {
		return lockingAndXRequest{}, false
	}
	return lockingAndXRequest{
		andxCommand: w[0],
		andxOffset:  bp.LE16(w[2:4]),
		fid:         bp.LE16(w[4:6]),
		unlocks:     unlocks,
		locks:       locks,
	}, true
}

// parseChainedCloseFID reads the FID from a CLOSE (0x04) block chained after a
// LOCKING_ANDX. Request words (WCT≥3): FID(2) LastWriteTime(4).
func parseChainedCloseFID(req []byte, off int) (uint16, bool) {
	if off < protocol.HeaderLen || off >= len(req) {
		return 0, false
	}
	wct := int(req[off])
	if wct < 3 {
		return 0, false
	}
	wStart := off + 1
	if wStart+2 > len(req) {
		return 0, false
	}
	return bp.LE16(req[wStart : wStart+2]), true
}

// parseLockRanges reads numUnlocks unlock ranges followed by numLocks lock ranges
// from a LOCKING_ANDX byte area. Each record is Pid(2) Offset(4) Length(4).
// Zero-length ranges are skipped (they lock nothing).
func parseLockRanges(area []byte, numUnlocks, numLocks int) (unlocks, locks []lockRange, ok bool) {
	const recordLen = 10
	if numUnlocks < 0 || numLocks < 0 {
		return nil, nil, false
	}
	if len(area) < (numUnlocks+numLocks)*recordLen {
		return nil, nil, false
	}
	read := func(b []byte) lockRange {
		return lockRange{
			pid:    bp.LE16(b[0:2]),
			start:  int64(bp.LE32(b[2:6])),
			length: int64(bp.LE32(b[6:10])),
		}
	}
	off := 0
	unlocks = make([]lockRange, 0, numUnlocks)
	for range numUnlocks {
		r := read(area[off : off+recordLen])
		off += recordLen
		if r.length > 0 {
			unlocks = append(unlocks, r)
		}
	}
	locks = make([]lockRange, 0, numLocks)
	for range numLocks {
		r := read(area[off : off+recordLen])
		off += recordLen
		if r.length > 0 {
			locks = append(locks, r)
		}
	}
	return unlocks, locks, true
}

// applyLockingAndX applies the unlocks then the locks against the session's lock
// table for the FID's file. Unlocks are applied first (per [MS-CIFS]); a failed
// unlock or a conflicting lock returns STATUS_LOCK_NOT_GRANTED with no partial
// change to the requested locks.
func (s *Service) applyLockingAndX(sess *smbSession, fid uint16, unlocks, locks []lockRange) uint32 {
	sess.mu.Lock()
	defer sess.mu.Unlock()

	hnd, ok := sess.fids[fid]
	if !ok || hnd == nil {
		return statusInvalidHandle
	}
	key := lockKeyForHandle(hnd)
	table := sess.locks[key]
	if table == nil {
		table = &lockTable{}
		sess.locks[key] = table
	}

	if !table.unlock(fid, unlocks) {
		return statusLockNotGranted
	}
	if !table.lock(fid, locks) {
		return statusLockNotGranted
	}
	return statusSuccess
}

// lock grants the given ranges for fid if none conflicts with a range held by a
// different PID/FID. All-or-nothing: on any conflict nothing is added.
func (t *lockTable) lock(fid uint16, ranges []lockRange) bool {
	for _, r := range ranges {
		for _, existing := range t.locks {
			if existing.pid == r.pid && existing.fid == fid {
				continue // the same owner may re-lock its own region
			}
			if rangesOverlap(existing.start, existing.length, r.start, r.length) {
				return false
			}
		}
	}
	for _, r := range ranges {
		t.locks = append(t.locks, lockEntry{fid: fid, pid: r.pid, start: r.start, length: r.length})
	}
	return true
}

// unlock releases the given exact ranges for fid. A range not held by fid at that
// exact (pid,start,length) fails the whole request ([MS-CIFS] unlock semantics).
func (t *lockTable) unlock(fid uint16, ranges []lockRange) bool {
	for _, r := range ranges {
		idx := -1
		for i, e := range t.locks {
			if e.fid == fid && e.pid == r.pid && e.start == r.start && e.length == r.length {
				idx = i
				break
			}
		}
		if idx < 0 {
			return false
		}
		t.locks = append(t.locks[:idx], t.locks[idx+1:]...)
	}
	return true
}

// rangesOverlap reports whether [startA,startA+lenA) and [startB,startB+lenB)
// intersect.
func rangesOverlap(startA, lenA, startB, lenB int64) bool {
	return startA < startB+lenB && startB < startA+lenA
}

// lockKeyForHandle keys the lock table by the handle's store path (lower-cased so
// case-insensitive DOS clients that reopen with different casing still collide).
func lockKeyForHandle(h *fileHandle) string {
	return toLowerASCIIStr(h.path)
}

// releaseLocksForFIDLocked drops every lock held by fid across all tables and
// prunes emptied tables. The caller must hold sess.mu.
func (sess *smbSession) releaseLocksForFIDLocked(fid uint16) {
	for key, table := range sess.locks {
		kept := table.locks[:0]
		for _, lk := range table.locks {
			if lk.fid != fid {
				kept = append(kept, lk)
			}
		}
		table.locks = kept
		if len(table.locks) == 0 {
			delete(sess.locks, key)
		}
	}
}

// buildLockingAndXResponse builds the LOCKING_ANDX success reply (WCT=2:
// AndXCommand=0xFF AndXReserved=0 AndXOffset=0; BCC=0).
func buildLockingAndXResponse(h protocol.Header) []byte {
	w := make([]byte, 4)
	w[0] = protocol.CommandNoAndXCommand
	return reply(h, statusSuccess, 2, w, nil)
}

// toLowerASCIIStr lower-cases the ASCII letters of s (store paths are ASCII in
// practice; non-ASCII bytes pass through so the key stays byte-stable).
func toLowerASCIIStr(s string) string {
	var b []byte
	for i := range len(s) {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			if b == nil {
				b = []byte(s)
			}
			b[i] = c + ('a' - 'A')
		}
	}
	if b == nil {
		return s
	}
	return string(b)
}
