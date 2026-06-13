package smb

import (
	"testing"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"

	protocol "github.com/ObsoleteMadness/ClassicStack/core/protocol/smb"
)

// ntCreateReq builds an SMB_COM_NT_CREATE_ANDX request (WCT=24) for an ANSI path,
// with the given DesiredAccess, CreateDisposition, and CreateOptions. The name
// rides the BCC area NUL-terminated (NameLength counts the bytes excluding the
// terminator, as a real client sends).
func ntCreateReq(tid uint16, path string, desiredAccess, disposition, options uint32) []byte {
	name := append([]byte(path), 0)
	words := make([]byte, 48)
	words[0] = protocol.CommandNoAndXCommand
	bp.PutLE16(words[5:7], uint16(len(path))) // NameLength (excl. terminator)
	bp.PutLE32(words[15:19], desiredAccess)
	bp.PutLE32(words[35:39], disposition)
	bp.PutLE32(words[39:43], options)
	return smbReq(protocol.CommandNtCreateAndX, protocol.Flags2NTStatus, tid, 1, words, name)
}

// ntCreate drives NT_CREATE_ANDX and returns the response header + the granted FID
// and CreateAction (0 if the call failed).
func ntCreate(t *testing.T, svc *Service, sess *smbSession, req []byte) (protocol.Header, uint16, uint32) {
	t.Helper()
	reply := svc.Dispatch(sess, req)
	h := respHeader(t, reply)
	if h.Status != statusSuccess {
		return h, 0, 0
	}
	fid := bp.LE16(reply[protocol.HeaderLen+6 : protocol.HeaderLen+8])     // FID at words[5:7]
	action := bp.LE32(reply[protocol.HeaderLen+8 : protocol.HeaderLen+12]) // CreateAction at words[7:11]
	return h, fid, action
}

const (
	fileReadData  = 0x00000001
	fileWriteData = 0x00000002
)

// TestNtCreate_CreatesNewFile proves CREATE disposition makes a new file, grants a
// writable FID, and reports CREATED; a second CREATE for the same name collides.
func TestNtCreate_CreatesNewFile(t *testing.T) {
	svc, sess, tid := fsService(t)

	h, fid, action := ntCreate(t, svc, sess, ntCreateReq(tid, "new.txt", fileWriteData, ntDispositionCreate, 0))
	if h.Status != statusSuccess {
		t.Fatalf("NT_CREATE create status = %#x", h.Status)
	}
	if action != ntActionCreated {
		t.Fatalf("CreateAction = %d, want CREATED(%d)", action, ntActionCreated)
	}
	if hnd, ok := sess.fileByFID(fid); !ok || !hnd.writable {
		t.Fatal("expected an open writable FID after CREATE")
	}

	// Second CREATE for the same name collides.
	h2, _, _ := ntCreate(t, svc, sess, ntCreateReq(tid, "new.txt", fileWriteData, ntDispositionCreate, 0))
	if h2.Status != statusObjectNameCollision {
		t.Fatalf("second CREATE status = %#x, want OBJECT_NAME_COLLISION", h2.Status)
	}
}

// TestNtCreate_OpenExisting proves OPEN disposition opens a file written through
// the CORE path and reports OPENED, while OPEN of a missing name fails.
func TestNtCreate_OpenExisting(t *testing.T) {
	svc, sess, tid := fsService(t)
	fid := createFile(t, svc, sess, tid, "doc.txt")
	writeAll(t, svc, sess, tid, fid, []byte("payload"))

	h, _, action := ntCreate(t, svc, sess, ntCreateReq(tid, "doc.txt", fileReadData, ntDispositionOpen, 0))
	if h.Status != statusSuccess {
		t.Fatalf("NT_CREATE open status = %#x", h.Status)
	}
	if action != ntActionOpened {
		t.Fatalf("CreateAction = %d, want OPENED(%d)", action, ntActionOpened)
	}

	hMiss, _, _ := ntCreate(t, svc, sess, ntCreateReq(tid, "ghost.txt", fileReadData, ntDispositionOpen, 0))
	if hMiss.Status != statusObjectNameNotFound {
		t.Fatalf("OPEN missing status = %#x, want OBJECT_NAME_NOT_FOUND", hMiss.Status)
	}
}

// TestNtCreate_ReadOnlyHandleRefusesWrite proves a read-only DesiredAccess opens a
// non-writable handle the WRITE path then denies.
func TestNtCreate_ReadOnlyHandleRefusesWrite(t *testing.T) {
	svc, sess, tid := fsService(t)
	pre := createFile(t, svc, sess, tid, "ro.txt")
	writeAll(t, svc, sess, tid, pre, []byte("x"))

	_, fid, _ := ntCreate(t, svc, sess, ntCreateReq(tid, "ro.txt", fileReadData, ntDispositionOpen, 0))
	if hnd, ok := sess.fileByFID(fid); !ok || hnd.writable {
		t.Fatal("expected a non-writable handle for read-only DesiredAccess")
	}
	// WRITE_ANDX to the read-only handle is denied.
	ww := make([]byte, 28)
	ww[0] = protocol.CommandNoAndXCommand
	bp.PutLE16(ww[4:6], fid)
	bp.PutLE16(ww[20:22], 1) // DataLength
	bp.PutLE16(ww[22:24], 0) // DataOffset (computed below path not needed; engine checks writable first)
	wreq := smbReq(protocol.CommandWriteAndX, protocol.Flags2NTStatus, tid, 1, ww, []byte{0x00})
	if h := respHeader(t, svc.Dispatch(sess, wreq)); h.Status != statusAccessDenied {
		t.Fatalf("WRITE to read-only handle status = %#x, want ACCESS_DENIED", h.Status)
	}
}

// TestNtCreate_Directory proves FILE_DIRECTORY_FILE creates a directory, marks the
// response Directory flag, and opens a dir handle carrying no fork file.
func TestNtCreate_Directory(t *testing.T) {
	svc, sess, tid := fsService(t)
	req := ntCreateReq(tid, "subdir", fileReadData, ntDispositionCreate, ntOptionDirectoryFile)
	reply := svc.Dispatch(sess, req)
	h := respHeader(t, reply)
	if h.Status != statusSuccess {
		t.Fatalf("NT_CREATE dir status = %#x", h.Status)
	}
	// Directory flag is the last response param byte (WCT=34 → words[67]).
	dirFlag := reply[protocol.HeaderLen+1+67]
	if dirFlag != 1 {
		t.Fatalf("Directory flag = %d, want 1", dirFlag)
	}
	fid := bp.LE16(reply[protocol.HeaderLen+6 : protocol.HeaderLen+8])
	hnd, ok := sess.fileByFID(fid)
	if !ok || !hnd.isDir || hnd.file != nil {
		t.Fatal("expected an isDir handle with no open fork file")
	}
}

// TestNtCreate_DirectoryFileMismatch proves opening a regular file with
// FILE_DIRECTORY_FILE (and vice-versa) is refused with the matching status.
func TestNtCreate_DirectoryFileMismatch(t *testing.T) {
	svc, sess, tid := fsService(t)
	fid := createFile(t, svc, sess, tid, "plain.txt")
	writeAll(t, svc, sess, tid, fid, []byte("d"))

	// Opening a regular file as a directory → NOT_A_DIRECTORY.
	hDir, _, _ := ntCreate(t, svc, sess, ntCreateReq(tid, "plain.txt", fileReadData, ntDispositionOpen, ntOptionDirectoryFile))
	if hDir.Status != statusNotADirectory {
		t.Fatalf("open file as dir status = %#x, want NOT_A_DIRECTORY", hDir.Status)
	}

	// Make a directory, then open it with FILE_NON_DIRECTORY_FILE → FILE_IS_A_DIRECTORY.
	if r := svc.Dispatch(sess, ntCreateReq(tid, "adir", fileReadData, ntDispositionCreate, ntOptionDirectoryFile)); respHeader(t, r).Status != statusSuccess {
		t.Fatal("dir create failed")
	}
	hFile, _, _ := ntCreate(t, svc, sess, ntCreateReq(tid, "adir", fileReadData, ntDispositionOpen, ntOptionNonDirectoryFile))
	if hFile.Status != statusFileIsADirectory {
		t.Fatalf("open dir as file status = %#x, want FILE_IS_A_DIRECTORY", hFile.Status)
	}
}

// TestNtCreate_BadTID proves a request on an unbound TID is refused with BAD_TID.
func TestNtCreate_BadTID(t *testing.T) {
	svc, sess, _ := fsService(t)
	h, _, _ := ntCreate(t, svc, sess, ntCreateReq(0x7777, "x.txt", fileReadData, ntDispositionOpen, 0))
	if h.Status != statusSMBBadTID {
		t.Fatalf("bad-TID status = %#x, want SMB_BAD_TID", h.Status)
	}
}
