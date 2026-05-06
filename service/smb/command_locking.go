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

func (s *Service) handleLockingAndX(req []byte, conn *connState) []byte {
	if len(req) < smbHeaderLen+17 {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}

	wct := int(req[smbHeaderLen])
	if wct < 8 {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}
	w := req[smbHeaderLen+1:]
	fid := binary.LittleEndian.Uint16(w[4:6])
	numberOfUnlocks := int(binary.LittleEndian.Uint16(w[12:14]))
	numberOfLocks := int(binary.LittleEndian.Uint16(w[14:16]))

	bytesArea, ok := smbBytesArea(req)
	if !ok {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}

	unlockRanges, lockRanges, ok := parseLockRanges(bytesArea, numberOfUnlocks, numberOfLocks)
	if !ok {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}

	conn.mu.Lock()
	handle, ok := conn.fids[fid]
	if !ok || handle == nil {
		conn.mu.Unlock()
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}

	lockKey := lockKeyForHandle(handle)
	table := conn.lockTables[lockKey]
	if table == nil {
		table = &lockTable{}
		conn.lockTables[lockKey] = table
	}

	if !unlockRangesFromTable(table, fid, unlockRanges) {
		conn.mu.Unlock()
		return buildSMBErrorResponse(req, smbStatusLockNotGranted)
	}
	if !lockRangesInTable(table, fid, lockRanges) {
		conn.mu.Unlock()
		return buildSMBErrorResponse(req, smbStatusLockNotGranted)
	}
	conn.mu.Unlock()

	return buildLockingAndXResponse(req)
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
