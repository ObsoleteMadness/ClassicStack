package smb

import (
	"testing"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"

	protocol "github.com/ObsoleteMadness/ClassicStack/core/protocol/smb"
)

// --- Regression tests for the CORE-dialect SMB commands ported from main:
// SMB_COM_SEARCH (0x81), SMB_COM_LOCKING_ANDX (0x24), and the multiplexed / raw
// transfer fall-backs (READ_MPX / WRITE_RAW / WRITE_MPX) + SMB_COM_SEEK. These
// were dropped in the refactor (falling through to STATUS_NOT_SUPPORTED) and are
// the DOS LAN Manager / WfW 3.11 browse-and-lock path. ---

// searchArea builds a SMB_COM_SEARCH byte area: BufferFormat(0x04) FileName NUL,
// then (when resume is set) BufferFormat(0x05) ResumeKeyLen(2) ResumeKey[21].
func searchArea(pattern string, resume []byte) []byte {
	out := []byte{0x04}
	out = append(out, []byte(pattern)...)
	out = append(out, 0)
	if resume != nil {
		out = append(out, 0x05)
		l := make([]byte, 2)
		bp.PutLE16(l, uint16(len(resume)))
		out = append(out, l...)
		out = append(out, resume...)
	}
	return out
}

// coreSearch drives one SMB_COM_SEARCH round and returns the reply.
func coreSearch(svc *Service, sess *smbSession, tid uint16, maxCount int, attrs uint16, area []byte) []byte {
	words := make([]byte, 4)
	bp.PutLE16(words[0:2], uint16(maxCount))
	bp.PutLE16(words[2:4], attrs)
	req := smbReq(protocol.CommandSearch, 0, tid, 1, words, area)
	return svc.Dispatch(sess, req)
}

// coreSearchRecords walks a SEARCH reply and returns the per-record resume keys
// and the packed 8.3 names.
func coreSearchRecords(t *testing.T, reply []byte) (resumeKeys [][]byte, names []string) {
	t.Helper()
	w := reply[protocol.HeaderLen+1:]
	count := int(bp.LE16(w[0:2]))
	// byte area: BufferFormat(1) DataLength(2) records.
	bccOff := protocol.HeaderLen + 1 + 2
	dataOff := bccOff + 2 + 1 + 2 // BCC(2) + BufferFormat(1) + DataLength(2)
	data := reply[dataOff:]
	for i := range count {
		rec := data[i*coreSearchRecordLen : (i+1)*coreSearchRecordLen]
		rk := append([]byte(nil), rec[0:21]...)
		resumeKeys = append(resumeKeys, rk)
		name := rec[30:43]
		if nul := indexByte(name, 0); nul >= 0 {
			name = name[:nul]
		}
		names = append(names, string(name))
	}
	return resumeKeys, names
}

// TestSearch_PagesAndEnds proves SMB_COM_SEARCH lists files across paged
// continuations (MaxCount per response) and answers STATUS_NO_MORE_FILES when
// exhausted — the WfW 3.11 browse loop.
func TestSearch_PagesAndEnds(t *testing.T) {
	svc, sess, tid := fsService(t)
	for _, name := range []string{"AAA.TXT", "BBB.TXT", "CCC.TXT"} {
		createFile(t, svc, sess, tid, name)
	}

	// First request: pattern "*.*", MaxCount=2 → 2 records + a live SID.
	reply := coreSearch(svc, sess, tid, 2, attrArchive, searchArea("*.*", nil))
	h := respHeader(t, reply)
	if h.Status != statusSuccess {
		t.Fatalf("SEARCH#1 status = %#x, want success", h.Status)
	}
	rks, names := coreSearchRecords(t, reply)
	if len(names) != 2 {
		t.Fatalf("SEARCH#1 returned %d names %v, want 2", len(names), names)
	}
	lastRK := rks[len(rks)-1]

	// Continuation: empty filename + the last resume key → the final record.
	reply = coreSearch(svc, sess, tid, 2, attrArchive, searchArea("", lastRK))
	if h := respHeader(t, reply); h.Status != statusSuccess {
		t.Fatalf("SEARCH#2 status = %#x, want success", h.Status)
	}
	_, names2 := coreSearchRecords(t, reply)
	if len(names2) != 1 {
		t.Fatalf("SEARCH#2 returned %d names %v, want 1", len(names2), names2)
	}

	// Third request continues from #2's key: exhausted → ERRnofiles.
	rks2, _ := coreSearchRecords(t, reply)
	reply = coreSearch(svc, sess, tid, 2, attrArchive, searchArea("", rks2[len(rks2)-1]))
	h = respHeader(t, reply)
	wantWire := toWireStatus(0, statusNoMoreFiles) // ERRDOS/ERRnofiles
	if h.Status != wantWire {
		t.Fatalf("SEARCH#3 status = %#x, want ERRnofiles %#x", h.Status, wantWire)
	}
}

// TestSearch_DirectoryAttrFilter proves a directory is returned only when the
// SearchAttributes filter sets ATTR_DIRECTORY.
func TestSearch_DirectoryAttrFilter(t *testing.T) {
	svc, sess, tid := fsService(t)
	mk := smbReq(protocol.CommandCreateDirectory, protocol.Flags2NTStatus, tid, 1, nil, ansiPathArea("SUB"))
	if h := respHeader(t, svc.Dispatch(sess, mk)); h.Status != statusSuccess {
		t.Fatalf("mkdir SUB status = %#x", h.Status)
	}

	// Without ATTR_DIRECTORY the directory is filtered out → no files.
	reply := coreSearch(svc, sess, tid, 10, attrArchive, searchArea("*.*", nil))
	if h := respHeader(t, reply); h.Status != toWireStatus(0, statusNoMoreFiles) {
		t.Fatalf("SEARCH(no dir attr) status = %#x, want ERRnofiles", h.Status)
	}

	// With ATTR_DIRECTORY the directory shows up.
	reply = coreSearch(svc, sess, tid, 10, attrDirectory|attrArchive, searchArea("*.*", nil))
	if h := respHeader(t, reply); h.Status != statusSuccess {
		t.Fatalf("SEARCH(dir attr) status = %#x, want success", h.Status)
	}
	_, names := coreSearchRecords(t, reply)
	if len(names) != 1 || names[0] != "SUB" {
		t.Fatalf("SEARCH(dir attr) names = %v, want [SUB]", names)
	}
}

// lockRangeBytes builds one LOCKING_ANDX lock/unlock record: Pid(2) Offset(4)
// Length(4).
func lockRangeBytes(pid uint16, offset, length uint32) []byte {
	b := make([]byte, 10)
	bp.PutLE16(b[0:2], pid)
	bp.PutLE32(b[2:6], offset)
	bp.PutLE32(b[6:10], length)
	return b
}

// lockingAndX drives one LOCKING_ANDX with the given unlock/lock ranges and the
// PID stamped in the header.
func lockingAndX(svc *Service, sess *smbSession, tid, fid, pid uint16, unlocks, locks [][]byte) []byte {
	words := make([]byte, 16)
	words[0] = protocol.CommandNoAndXCommand // AndXCommand
	bp.PutLE16(words[4:6], fid)
	bp.PutLE16(words[12:14], uint16(len(unlocks)))
	bp.PutLE16(words[14:16], uint16(len(locks)))
	var area []byte
	for _, r := range unlocks {
		area = append(area, r...)
	}
	for _, r := range locks {
		area = append(area, r...)
	}
	h := protocol.Header{Command: protocol.CommandLockingAndX, Flags2: protocol.Flags2NTStatus, TID: tid, UID: 1, MID: 1, PIDLow: pid}
	out := h.Encode(nil)
	out = append(out, byte(len(words)/2))
	out = append(out, words...)
	out = append(out, byte(len(area)), byte(len(area)>>8))
	out = append(out, area...)
	return svc.Dispatch(sess, out)
}

// TestLockingAndX_GrantConflictUnlock proves a byte-range lock is granted, an
// overlapping lock from a different PID is refused (STATUS_LOCK_NOT_GRANTED), and
// after unlock the range is grantable again.
func TestLockingAndX_GrantConflictUnlock(t *testing.T) {
	svc, sess, tid := fsService(t)
	fid := createFile(t, svc, sess, tid, "DB.DAT")

	// PID 100 locks [0,16).
	reply := lockingAndX(svc, sess, tid, fid, 100, nil, [][]byte{lockRangeBytes(100, 0, 16)})
	if h := respHeader(t, reply); h.Status != statusSuccess {
		t.Fatalf("LOCK grant status = %#x, want success", h.Status)
	}

	// PID 200 tries to lock the overlapping [8,16) → refused.
	reply = lockingAndX(svc, sess, tid, fid, 200, nil, [][]byte{lockRangeBytes(200, 8, 8)})
	h := respHeader(t, reply)
	if h.Status != toWireStatus(protocol.Flags2NTStatus, statusLockNotGranted) {
		t.Fatalf("conflicting LOCK status = %#x, want LOCK_NOT_GRANTED", h.Status)
	}

	// PID 100 unlocks [0,16).
	reply = lockingAndX(svc, sess, tid, fid, 100, [][]byte{lockRangeBytes(100, 0, 16)}, nil)
	if h := respHeader(t, reply); h.Status != statusSuccess {
		t.Fatalf("UNLOCK status = %#x, want success", h.Status)
	}

	// Now PID 200 can lock the previously-conflicting range.
	reply = lockingAndX(svc, sess, tid, fid, 200, nil, [][]byte{lockRangeBytes(200, 8, 8)})
	if h := respHeader(t, reply); h.Status != statusSuccess {
		t.Fatalf("re-LOCK after unlock status = %#x, want success", h.Status)
	}
}

// TestLockingAndX_CloseReleasesLocks proves closing the FID drops its locks so a
// different PID can then lock the same range.
func TestLockingAndX_CloseReleasesLocks(t *testing.T) {
	svc, sess, tid := fsService(t)
	fid := createFile(t, svc, sess, tid, "DB.DAT")

	if h := respHeader(t, lockingAndX(svc, sess, tid, fid, 100, nil, [][]byte{lockRangeBytes(100, 0, 32)})); h.Status != statusSuccess {
		t.Fatalf("LOCK status = %#x", h.Status)
	}
	// CLOSE the FID.
	cw := make([]byte, 6)
	bp.PutLE16(cw[0:2], fid)
	creq := smbReq(protocol.CommandClose, protocol.Flags2NTStatus, tid, 1, cw, nil)
	if h := respHeader(t, svc.Dispatch(sess, creq)); h.Status != statusSuccess {
		t.Fatalf("CLOSE status = %#x", h.Status)
	}

	// A fresh FID + different PID may now lock the same range.
	fid2 := createFile(t, svc, sess, tid, "DB.DAT")
	if h := respHeader(t, lockingAndX(svc, sess, tid, fid2, 200, nil, [][]byte{lockRangeBytes(200, 0, 32)})); h.Status != statusSuccess {
		t.Fatalf("LOCK after close status = %#x, want success (locks not released)", h.Status)
	}
}

// TestMPXAndRaw_Fallback proves READ_MPX and WRITE_RAW steer a CORE client back
// to standard read/write: READ_MPX → ERRSRV/ERRuseSTD, WRITE_RAW → Count=0.
func TestMPXAndRaw_Fallback(t *testing.T) {
	svc, sess, tid := fsService(t)

	// READ_MPX → USE_STANDARD (in DOS-error wire form for a non-NT client).
	rreq := smbReq(protocol.CommandReadMPX, 0, tid, 1, make([]byte, 16), nil)
	h := respHeader(t, svc.Dispatch(sess, rreq))
	if h.Status != 0x00FB0002 {
		t.Fatalf("READ_MPX status = %#x, want ERRSRV/ERRuseSTD 0x00FB0002", h.Status)
	}

	// WRITE_RAW → success with a zero-count Final Response (WCT=1).
	wreq := smbReq(protocol.CommandWriteRaw, protocol.Flags2NTStatus, tid, 1, make([]byte, 24), nil)
	reply := svc.Dispatch(sess, wreq)
	h = respHeader(t, reply)
	if h.Status != statusSuccess {
		t.Fatalf("WRITE_RAW status = %#x, want success", h.Status)
	}
	if wct := reply[protocol.HeaderLen]; wct != 1 {
		t.Fatalf("WRITE_RAW WCT = %d, want 1", wct)
	}
	if count := bp.LE16(reply[protocol.HeaderLen+1 : protocol.HeaderLen+3]); count != 0 {
		t.Fatalf("WRITE_RAW Count = %d, want 0", count)
	}
}
