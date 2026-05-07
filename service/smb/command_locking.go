package smb

import (
	"encoding/binary"
	"strings"
)

const (
	smbStatusLockNotGranted = 0xC0000055
)

type lockRange struct {
	pid    uint16
	start  int64
	length int64
}

type lockingAndXCommand struct {
	andxCommand byte
	andxOffset  uint16
	fid         uint16
	unlocks     []lockRange
	locks       []lockRange
}

func (s *Service) handleLockingAndX(req []byte, conn *connState) []byte {
	if len(req) < smbHeaderLen+17 {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}

	cmd := req[4]
	cmdOffset := smbHeaderLen
	for {
		switch cmd {
		case CommandLockingAndX:
			lockingCmd, ok := parseLockingAndXAt(req, cmdOffset)
			if !ok {
				return buildSMBErrorResponse(req, smbStatusNotSupported)
			}
			status := s.applyLockingAndX(conn, lockingCmd.fid, lockingCmd.unlocks, lockingCmd.locks)
			if status != smbStatusSuccess {
				return buildSMBErrorResponse(req, status)
			}
			if lockingCmd.andxCommand == CommandNoAndXCommand {
				return buildLockingAndXResponse(req)
			}
			cmd = lockingCmd.andxCommand
			cmdOffset = int(lockingCmd.andxOffset)
			if cmdOffset <= smbHeaderLen || cmdOffset >= len(req) {
				return buildSMBErrorResponse(req, smbStatusNotSupported)
			}

		case CommandClose:
			fid, ok := parseCloseAt(req, cmdOffset)
			if !ok {
				return buildSMBErrorResponse(req, smbStatusNotSupported)
			}
			s.closeFID(conn, fid)
			return buildLockingAndXResponse(req)

		default:
			return buildSMBErrorResponse(req, smbStatusNotSupported)
		}
	}
}

func parseLockingAndXAt(req []byte, cmdOffset int) (lockingAndXCommand, bool) {
	if cmdOffset < smbHeaderLen || cmdOffset+1 > len(req) {
		return lockingAndXCommand{}, false
	}

	wct := int(req[cmdOffset])
	if wct < 8 {
		return lockingAndXCommand{}, false
	}

	wordsOffset := cmdOffset + 1
	wordsLen := wct * 2
	if wordsOffset+wordsLen > len(req) {
		return lockingAndXCommand{}, false
	}
	w := req[wordsOffset : wordsOffset+wordsLen]
	byteCountOffset := wordsOffset + wordsLen
	if byteCountOffset+2 > len(req) {
		return lockingAndXCommand{}, false
	}
	byteCount := int(binary.LittleEndian.Uint16(req[byteCountOffset : byteCountOffset+2]))
	if byteCountOffset+2+byteCount > len(req) {
		return lockingAndXCommand{}, false
	}
	bytesArea := req[byteCountOffset+2 : byteCountOffset+2+byteCount]
	numberOfUnlocks := int(binary.LittleEndian.Uint16(w[12:14]))
	numberOfLocks := int(binary.LittleEndian.Uint16(w[14:16]))
	unlocks, locks, ok := parseLockRanges(bytesArea, numberOfUnlocks, numberOfLocks)
	if !ok {
		return lockingAndXCommand{}, false
	}

	return lockingAndXCommand{
		andxCommand: w[0],
		andxOffset:  binary.LittleEndian.Uint16(w[2:4]),
		fid:         binary.LittleEndian.Uint16(w[4:6]),
		unlocks:     unlocks,
		locks:       locks,
	}, true
}

func parseCloseAt(req []byte, cmdOffset int) (uint16, bool) {
	if cmdOffset < smbHeaderLen || cmdOffset+1 > len(req) {
		return 0, false
	}
	wct := int(req[cmdOffset])
	if wct < 3 {
		return 0, false
	}
	wordsOffset := cmdOffset + 1
	wordsLen := wct * 2
	if wordsOffset+wordsLen > len(req) {
		return 0, false
	}
	return binary.LittleEndian.Uint16(req[wordsOffset : wordsOffset+2]), true
}

func (s *Service) applyLockingAndX(conn *connState, fid uint16, unlockRanges, lockRanges []lockRange) uint32 {
	conn.mu.Lock()
	handle, ok := conn.fids[fid]
	if !ok || handle == nil {
		conn.mu.Unlock()
		return smbStatusNotSupported
	}

	lockKey := lockKeyForHandle(handle)
	table := conn.lockTables[lockKey]
	if table == nil {
		table = &lockTable{}
		conn.lockTables[lockKey] = table
	}
	conn.mu.Unlock()

	if !unlockRangesFromTable(table, fid, unlockRanges) {
		return smbStatusLockNotGranted
	}
	if !lockRangesInTable(table, fid, lockRanges) {
		return smbStatusLockNotGranted
	}
	return smbStatusSuccess
}

func parseLockRanges(bytesArea []byte, numberOfUnlocks, numberOfLocks int) (unlocks []lockRange, locks []lockRange, ok bool) {
	const recordLen = 10 // Pid(2) + ByteOffset(4) + LengthInBytes(4)
	required := (numberOfUnlocks + numberOfLocks) * recordLen
	if numberOfUnlocks < 0 || numberOfLocks < 0 || len(bytesArea) < required {
		return nil, nil, false
	}

	readRange := func(b []byte) lockRange {
		return lockRange{
			pid:    binary.LittleEndian.Uint16(b[0:2]),
			start:  int64(binary.LittleEndian.Uint32(b[2:6])),
			length: int64(binary.LittleEndian.Uint32(b[6:10])),
		}
	}

	off := 0
	unlocks = make([]lockRange, 0, numberOfUnlocks)
	for i := 0; i < numberOfUnlocks; i++ {
		r := readRange(bytesArea[off : off+recordLen])
		off += recordLen
		if r.length <= 0 {
			continue
		}
		unlocks = append(unlocks, r)
	}

	locks = make([]lockRange, 0, numberOfLocks)
	for i := 0; i < numberOfLocks; i++ {
		r := readRange(bytesArea[off : off+recordLen])
		off += recordLen
		if r.length <= 0 {
			continue
		}
		locks = append(locks, r)
	}

	return unlocks, locks, true
}

func lockRangesInTable(table *lockTable, fid uint16, ranges []lockRange) bool {
	table.mu.Lock()
	defer table.mu.Unlock()

	for _, r := range ranges {
		for _, existing := range table.locks {
			if existing.pid == r.pid && existing.fid == fid {
				continue
			}
			if rangesOverlap(existing.start, existing.length, r.start, r.length) {
				return false
			}
		}
	}

	for _, r := range ranges {
		table.locks = append(table.locks, lockEntry{
			fid:    fid,
			pid:    r.pid,
			start:  r.start,
			length: r.length,
		})
	}
	return true
}

func unlockRangesFromTable(table *lockTable, fid uint16, ranges []lockRange) bool {
	table.mu.Lock()
	defer table.mu.Unlock()

	for _, r := range ranges {
		idx := -1
		for i, existing := range table.locks {
			if existing.fid == fid && existing.pid == r.pid && existing.start == r.start && existing.length == r.length {
				idx = i
				break
			}
		}
		if idx < 0 {
			return false
		}
		table.locks = append(table.locks[:idx], table.locks[idx+1:]...)
	}
	return true
}

func rangesOverlap(startA, lenA, startB, lenB int64) bool {
	endA := startA + lenA
	endB := startB + lenB
	return startA < endB && startB < endA
}

func lockKeyForHandle(h *fileHandle) string {
	return strings.ToLower(h.path)
}

func (s *Service) releaseLocksForFIDLocked(conn *connState, fid uint16) {
	for key, table := range conn.lockTables {
		table.mu.Lock()
		filtered := table.locks[:0]
		for _, lk := range table.locks {
			if lk.fid != fid {
				filtered = append(filtered, lk)
			}
		}
		table.locks = filtered
		empty := len(table.locks) == 0
		table.mu.Unlock()
		if empty {
			delete(conn.lockTables, key)
		}
	}
}

func buildLockingAndXResponse(req []byte) []byte {
	if len(req) < smbHeaderLen || string(req[0:4]) != "\xffSMB" {
		return nil
	}

	out := make([]byte, smbHeaderLen+1+(2*2)+2)
	copy(out[:smbHeaderLen], req[:smbHeaderLen])
	binary.LittleEndian.PutUint32(out[smbOffStatus:smbOffStatus+4], smbStatusSuccess)
	out[smbOffFlags] |= 0x80
	out[smbHeaderLen] = 2 // WCT
	w := out[smbHeaderLen+1:]
	w[0] = CommandNoAndXCommand
	w[1] = 0
	binary.LittleEndian.PutUint16(w[2:4], 0)
	binary.LittleEndian.PutUint16(w[4:6], 0) // ByteCount
	return out
}
