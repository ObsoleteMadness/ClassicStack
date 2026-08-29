package smb

import (
	"testing"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"

	protocol "github.com/ObsoleteMadness/ClassicStack/core/protocol/smb"
)

// fsService builds an SMB service with one memfs/ads share named PUBLIC, a
// session, and a TID already bound to the share (FS commands need a tree).
func fsService(t *testing.T) (*Service, *smbSession, uint16) {
	t.Helper()
	svc, sess := newDispatchService(t)
	tid := sess.allocTID(&treeConnect{share: svc.shares[0]})
	return svc, sess, tid
}

// ansiPathArea builds a path byte area: a 0x04 SMB_FORMAT_ASCII buffer-format
// byte then the NUL-terminated ANSI path the engine's extractWirePath expects.
func ansiPathArea(path string) []byte {
	out := []byte{0x04}
	out = append(out, []byte(path)...)
	return append(out, 0)
}

// createFile drives SMB_COM_CREATE and returns the granted FID.
func createFile(t *testing.T, svc *Service, sess *smbSession, tid uint16, path string) uint16 {
	t.Helper()
	words := make([]byte, 6) // FileAttributes(2) CreationTime(4)
	req := smbReq(protocol.CommandCreate, protocol.Flags2NTStatus, tid, 1, words, ansiPathArea(path))
	reply := svc.Dispatch(sess, req)
	h := respHeader(t, reply)
	if h.Status != statusSuccess {
		t.Fatalf("CREATE %q status = %#x", path, h.Status)
	}
	return bp.LE16(reply[protocol.HeaderLen+1 : protocol.HeaderLen+3])
}

// writeAll drives SMB_COM_WRITE at offset 0 and returns the byte count written.
func writeAll(t *testing.T, svc *Service, sess *smbSession, tid, fid uint16, data []byte) int {
	t.Helper()
	words := make([]byte, 10) // FID(2) Count(2) Offset(4) Remaining(2)
	bp.PutLE16(words[0:2], fid)
	bp.PutLE16(words[2:4], uint16(len(data)))
	bp.PutLE32(words[4:8], 0)
	area := make([]byte, 3+len(data))
	area[0] = 0x01 // SMB_FORMAT_DATA
	bp.PutLE16(area[1:3], uint16(len(data)))
	copy(area[3:], data)
	req := smbReq(protocol.CommandWrite, protocol.Flags2NTStatus, tid, 1, words, area)
	reply := svc.Dispatch(sess, req)
	h := respHeader(t, reply)
	if h.Status != statusSuccess {
		t.Fatalf("WRITE status = %#x", h.Status)
	}
	return int(bp.LE16(reply[protocol.HeaderLen+1 : protocol.HeaderLen+3]))
}

// TestFS_CreateWriteReadClose proves a file written through SMB_COM_CREATE/WRITE
// reads back byte-identical via OPEN_ANDX/READ_ANDX, and CLOSE releases the FID.
func TestFS_CreateWriteReadClose(t *testing.T) {
	svc, sess, tid := fsService(t)
	payload := []byte("hello smb world")

	fid := createFile(t, svc, sess, tid, "readme.txt")
	if n := writeAll(t, svc, sess, tid, fid, payload); n != len(payload) {
		t.Fatalf("WRITE wrote %d, want %d", n, len(payload))
	}
	// CLOSE the write handle.
	closeWords := make([]byte, 6)
	bp.PutLE16(closeWords[0:2], fid)
	creq := smbReq(protocol.CommandClose, protocol.Flags2NTStatus, tid, 1, closeWords, nil)
	if h := respHeader(t, svc.Dispatch(sess, creq)); h.Status != statusSuccess {
		t.Fatalf("CLOSE status = %#x", h.Status)
	}
	if _, ok := sess.fileByFID(fid); ok {
		t.Fatal("FID still open after CLOSE")
	}

	// OPEN_ANDX (read) then READ_ANDX the whole file.
	ow := make([]byte, 30)
	ow[0] = protocol.CommandNoAndXCommand
	bp.PutLE16(ow[6:8], 0)      // AccessMode = read
	bp.PutLE16(ow[16:18], 0x01) // OpenFunction = open existing
	oreq := smbReq(protocol.CommandOpenAndX, protocol.Flags2NTStatus, tid, 1, ow, ansiPathArea("readme.txt"))
	oreply := svc.Dispatch(sess, oreq)
	oh := respHeader(t, oreply)
	if oh.Status != statusSuccess {
		t.Fatalf("OPEN_ANDX status = %#x", oh.Status)
	}
	rfid := bp.LE16(oreply[protocol.HeaderLen+5 : protocol.HeaderLen+7])

	rw := make([]byte, 24)
	rw[0] = protocol.CommandNoAndXCommand
	bp.PutLE16(rw[4:6], rfid)
	bp.PutLE32(rw[6:10], 0)                       // Offset
	bp.PutLE16(rw[10:12], uint16(len(payload)+8)) // MaxCount
	rreq := smbReq(protocol.CommandReadAndX, protocol.Flags2NTStatus, tid, 1, rw, nil)
	rreply := svc.Dispatch(sess, rreq)
	rh := respHeader(t, rreply)
	if rh.Status != statusSuccess {
		t.Fatalf("READ_ANDX status = %#x", rh.Status)
	}
	got := readAndXData(t, rreply)
	if string(got) != string(payload) {
		t.Fatalf("read back %q, want %q", got, payload)
	}
}

// readAndXData extracts the data block from a READ_ANDX reply using its
// DataOffset/DataLength fields.
func readAndXData(t *testing.T, reply []byte) []byte {
	t.Helper()
	w := reply[protocol.HeaderLen+1:]
	dataLen := int(bp.LE16(w[10:12]))
	dataOff := int(bp.LE16(w[12:14]))
	if dataOff+dataLen > len(reply) {
		t.Fatalf("READ_ANDX data out of range: off=%d len=%d total=%d", dataOff, dataLen, len(reply))
	}
	return reply[dataOff : dataOff+dataLen]
}

// TestFS_WriteToReadOnlyHandleDenied proves a WRITE to a read-opened FID is
// refused with STATUS_ACCESS_DENIED.
func TestFS_WriteToReadOnlyHandleDenied(t *testing.T) {
	svc, sess, tid := fsService(t)
	fid := createFile(t, svc, sess, tid, "ro.bin")
	writeAll(t, svc, sess, tid, fid, []byte("seed"))
	sess.closeFID(fid)

	// Re-open read-only.
	ow := make([]byte, 30)
	ow[0] = protocol.CommandNoAndXCommand
	bp.PutLE16(ow[6:8], 0)      // read
	bp.PutLE16(ow[16:18], 0x01) // open existing
	oreply := svc.Dispatch(sess, smbReq(protocol.CommandOpenAndX, protocol.Flags2NTStatus, tid, 1, ow, ansiPathArea("ro.bin")))
	rfid := bp.LE16(oreply[protocol.HeaderLen+5 : protocol.HeaderLen+7])

	words := make([]byte, 10)
	bp.PutLE16(words[0:2], rfid)
	bp.PutLE16(words[2:4], 4)
	area := []byte{0x01, 4, 0, 'n', 'o', 'p', 'e'}
	reply := svc.Dispatch(sess, smbReq(protocol.CommandWrite, protocol.Flags2NTStatus, tid, 1, words, area))
	if h := respHeader(t, reply); h.Status != statusAccessDenied {
		t.Fatalf("WRITE to read-only handle status = %#x, want ACCESS_DENIED", h.Status)
	}
}

// TestFS_WriteAndClose proves SMB_COM_WRITE_AND_CLOSE (0x2C, the OS/2 Workplace
// Shell write path) persists the data and closes the FID in one round trip.
func TestFS_WriteAndClose(t *testing.T) {
	svc, sess, tid := fsService(t)
	payload := []byte("workplace shell state")
	fid := createFile(t, svc, sess, tid, "wpstate.dat")

	words := make([]byte, 12) // FID(2) Count(2) Offset(4) LastWriteTime(4)
	bp.PutLE16(words[0:2], fid)
	bp.PutLE16(words[2:4], uint16(len(payload)))
	bp.PutLE32(words[4:8], 0)
	area := make([]byte, 1+len(payload))
	copy(area[1:], payload)
	req := smbReq(protocol.CommandWriteAndClose, protocol.Flags2NTStatus, tid, 1, words, area)
	reply := svc.Dispatch(sess, req)
	h := respHeader(t, reply)
	if h.Status != statusSuccess {
		t.Fatalf("WRITE_AND_CLOSE status = %#x", h.Status)
	}
	if n := int(bp.LE16(reply[protocol.HeaderLen+1 : protocol.HeaderLen+3])); n != len(payload) {
		t.Fatalf("WRITE_AND_CLOSE wrote %d, want %d", n, len(payload))
	}
	if _, ok := sess.fileByFID(fid); ok {
		t.Fatal("FID still open after WRITE_AND_CLOSE")
	}

	// Re-open and read back to confirm the data landed.
	ow := make([]byte, 30)
	ow[0] = protocol.CommandNoAndXCommand
	bp.PutLE16(ow[6:8], 0)      // read
	bp.PutLE16(ow[16:18], 0x01) // open existing
	oreply := svc.Dispatch(sess, smbReq(protocol.CommandOpenAndX, protocol.Flags2NTStatus, tid, 1, ow, ansiPathArea("wpstate.dat")))
	if oh := respHeader(t, oreply); oh.Status != statusSuccess {
		t.Fatalf("OPEN_ANDX status = %#x", oh.Status)
	}
	rfid := bp.LE16(oreply[protocol.HeaderLen+5 : protocol.HeaderLen+7])

	rw := make([]byte, 24)
	rw[0] = protocol.CommandNoAndXCommand
	bp.PutLE16(rw[4:6], rfid)
	bp.PutLE32(rw[6:10], 0)
	bp.PutLE16(rw[10:12], uint16(len(payload)+8))
	rreply := svc.Dispatch(sess, smbReq(protocol.CommandReadAndX, protocol.Flags2NTStatus, tid, 1, rw, nil))
	if rh := respHeader(t, rreply); rh.Status != statusSuccess {
		t.Fatalf("READ_ANDX status = %#x", rh.Status)
	}
	if got := readAndXData(t, rreply); string(got) != string(payload) {
		t.Fatalf("read back %q, want %q", got, payload)
	}
}

// TestFS_WriteAndCloseTruncatesShorterOverwrite proves WRITE_AND_CLOSE shrinks
// the file when the new content is shorter than what was there before, even
// though the file was reopened WITHOUT a truncate OpenFunction. This mirrors
// OS/2 Workplace Shell rewriting its "\WP ROOT. SF" state file: it reopens
// with OpenFunction 0x0011 (open-existing, no truncate) on a fresh FID each
// time and issues a single WRITE_AND_CLOSE of the new content from offset 0,
// relying on WRITE_AND_CLOSE alone to resize the file — it never sends a
// separate SetEndOfFile/SetFileSize. Before this fix, a shorter rewrite left
// stale trailing bytes from the previous (longer) write past the new EOF
// (netbeui.pcap 2026-07-15: 383-byte write, then a 346-byte write on a new
// FID, file stayed 383 bytes).
func TestFS_WriteAndCloseTruncatesShorterOverwrite(t *testing.T) {
	svc, sess, tid := fsService(t)
	long := []byte("this is the original, longer payload contents")
	short := []byte("shorter now")
	fid := createFile(t, svc, sess, tid, "WP ROOT. SF")

	writeClose := func(fid uint16, payload []byte) []byte {
		words := make([]byte, 12) // FID(2) Count(2) Offset(4) LastWriteTime(4)
		bp.PutLE16(words[0:2], fid)
		bp.PutLE16(words[2:4], uint16(len(payload)))
		bp.PutLE32(words[4:8], 0)
		area := make([]byte, 1+len(payload))
		copy(area[1:], payload)
		req := smbReq(protocol.CommandWriteAndClose, protocol.Flags2NTStatus, tid, 1, words, area)
		reply := svc.Dispatch(sess, req)
		if h := respHeader(t, reply); h.Status != statusSuccess {
			t.Fatalf("WRITE_AND_CLOSE status = %#x", h.Status)
		}
		return reply
	}
	writeClose(fid, long)

	// Reopen WITHOUT truncate (OpenFunction 0x0011: open-existing, no resize) —
	// the exact WPS pattern — and overwrite with shorter content from offset 0.
	openExisting := func() uint16 {
		ow := make([]byte, 30)
		ow[0] = protocol.CommandNoAndXCommand
		bp.PutLE16(ow[6:8], 0x0121) // AccessMode: read/write
		bp.PutLE16(ow[16:18], 0x11) // OpenFunction: open-existing, no truncate
		oreply := svc.Dispatch(sess, smbReq(protocol.CommandOpenAndX, protocol.Flags2NTStatus, tid, 1, ow, ansiPathArea("WP ROOT. SF")))
		if oh := respHeader(t, oreply); oh.Status != statusSuccess {
			t.Fatalf("OPEN_ANDX status = %#x", oh.Status)
		}
		return bp.LE16(oreply[protocol.HeaderLen+5 : protocol.HeaderLen+7])
	}
	fid2 := openExisting()
	writeClose(fid2, short)

	// Re-open once more and read back: must be exactly `short`, no stale tail.
	fid3 := openExisting()
	rw := make([]byte, 24)
	rw[0] = protocol.CommandNoAndXCommand
	bp.PutLE16(rw[4:6], fid3)
	bp.PutLE32(rw[6:10], 0)
	bp.PutLE16(rw[10:12], uint16(len(long)+8))
	rreply := svc.Dispatch(sess, smbReq(protocol.CommandReadAndX, protocol.Flags2NTStatus, tid, 1, rw, nil))
	if rh := respHeader(t, rreply); rh.Status != statusSuccess {
		t.Fatalf("READ_ANDX status = %#x", rh.Status)
	}
	if got := readAndXData(t, rreply); string(got) != string(short) {
		t.Fatalf("read back %q, want %q (file must be truncated to the shorter write, no stale trailing bytes)", got, short)
	}
}

// TestFS_BadTIDRefused proves an FS command on an unbound TID is refused with
// STATUS_SMB_BAD_TID, not a panic or silent drop.
func TestFS_BadTIDRefused(t *testing.T) {
	svc, sess := newDispatchService(t)
	req := smbReq(protocol.CommandCreate, protocol.Flags2NTStatus, 999, 1, make([]byte, 6), ansiPathArea("x"))
	if h := respHeader(t, svc.Dispatch(sess, req)); h.Status != statusSMBBadTID {
		t.Fatalf("CREATE on bad TID status = %#x, want SMB_BAD_TID", h.Status)
	}
}

// TestFS_UnicodePathRoundTrip proves a file created with a UTF-16 (Unicode-flag)
// name opens back under the same Unicode name — the per-request charset is
// threaded through the share codec on both create and open.
func TestFS_UnicodePathRoundTrip(t *testing.T) {
	svc, sess, tid := fsService(t)
	flags2 := protocol.Flags2NTStatus | protocol.Flags2Unicode

	// CREATE with a 0x04-prefixed UTF-16 name (a pad byte aligns the string).
	name := "café.txt"
	area := []byte{0x04, 0x00} // buffer-format + alignment pad
	area = append(area, utf16Wire(name)...)
	area = append(area, 0, 0) // UTF-16 NUL terminator
	creq := smbReq(protocol.CommandCreate, flags2, tid, 1, make([]byte, 6), area)
	if h := respHeader(t, svc.Dispatch(sess, creq)); h.Status != statusSuccess {
		t.Fatalf("Unicode CREATE status = %#x", h.Status)
	}

	// OPEN_ANDX the same Unicode name.
	ow := make([]byte, 30)
	ow[0] = protocol.CommandNoAndXCommand
	bp.PutLE16(ow[16:18], 0x01) // open existing
	oreq := smbReq(protocol.CommandOpenAndX, flags2, tid, 1, ow, area)
	if h := respHeader(t, svc.Dispatch(sess, oreq)); h.Status != statusSuccess {
		t.Fatalf("Unicode OPEN_ANDX status = %#x, want success", h.Status)
	}
}

// TestFS_OpenMissingFileNotFound proves OPEN_ANDX (open-existing) on a missing
// file returns STATUS_OBJECT_NAME_NOT_FOUND.
func TestFS_OpenMissingFileNotFound(t *testing.T) {
	svc, sess, tid := fsService(t)
	ow := make([]byte, 30)
	ow[0] = protocol.CommandNoAndXCommand
	bp.PutLE16(ow[16:18], 0x01) // open existing, no create bit
	reply := svc.Dispatch(sess, smbReq(protocol.CommandOpenAndX, protocol.Flags2NTStatus, tid, 1, ow, ansiPathArea("ghost")))
	if h := respHeader(t, reply); h.Status != statusObjectNameNotFound {
		t.Fatalf("OPEN_ANDX missing status = %#x, want OBJECT_NAME_NOT_FOUND", h.Status)
	}
}

// TestFS_OpenAndXOnDirectoryRefused proves OPEN_ANDX (0x2D) on a directory is
// refused with STATUS_FILE_IS_A_DIRECTORY, matching OPEN/CREATE. Before this,
// OPEN_ANDX had no IsDir check: it handed out a FID over os.OpenFile(dir), which
// succeeded (attrs correctly reported Directory), and only the follow-up READ
// failed — with the generic ERRSRV/ERRerror a CORE-dialect directory-copy client
// can't distinguish from an ordinary I/O fault, so it aborted the whole copy
// instead of recursing (observed as Windows "System error 1026" over an IPX SMB
// capture).
func TestFS_OpenAndXOnDirectoryRefused(t *testing.T) {
	svc, sess, tid := fsService(t)

	mkdirReq := smbReq(protocol.CommandCreateDirectory, protocol.Flags2NTStatus, tid, 1, nil, ansiPathArea("subdir"))
	if h := respHeader(t, svc.Dispatch(sess, mkdirReq)); h.Status != statusSuccess {
		t.Fatalf("CREATE_DIRECTORY status = %#x", h.Status)
	}

	ow := make([]byte, 30)
	ow[0] = protocol.CommandNoAndXCommand
	bp.PutLE16(ow[16:18], 0x01) // open existing, no create bit
	reply := svc.Dispatch(sess, smbReq(protocol.CommandOpenAndX, protocol.Flags2NTStatus, tid, 1, ow, ansiPathArea("subdir")))
	if h := respHeader(t, reply); h.Status != statusFileIsADirectory {
		t.Fatalf("OPEN_ANDX on directory status = %#x, want STATUS_FILE_IS_A_DIRECTORY", h.Status)
	}
	if len(sess.fids) != 0 {
		t.Fatalf("OPEN_ANDX on directory left %d FID(s) open, want 0", len(sess.fids))
	}
}
