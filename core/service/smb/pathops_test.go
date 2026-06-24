package smb

import (
	"testing"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"

	protocol "github.com/ObsoleteMadness/ClassicStack/core/protocol/smb"
)

// ntReq is smbReq with the NT-status flag set (so error replies carry the
// NTSTATUS form the tests assert on directly).
func ntReq(cmd uint8, tid uint16, words, area []byte) []byte {
	return smbReq(cmd, protocol.Flags2NTStatus, tid, 1, words, area)
}

// TestPathOps_MkdirCheckdirRmdir proves CREATE_DIRECTORY makes a dir,
// CHECK_DIRECTORY confirms it, a second mkdir is idempotent, and
// DELETE_DIRECTORY removes it (after which CHECK_DIRECTORY fails).
func TestPathOps_MkdirCheckdirRmdir(t *testing.T) {
	svc, sess, tid := fsService(t)

	mk := ntReq(protocol.CommandCreateDirectory, tid, nil, ansiPathArea("docs"))
	if h := respHeader(t, svc.Dispatch(sess, mk)); h.Status != statusSuccess {
		t.Fatalf("CREATE_DIRECTORY status = %#x", h.Status)
	}
	// Idempotent second mkdir.
	if h := respHeader(t, svc.Dispatch(sess, mk)); h.Status != statusSuccess {
		t.Fatalf("second CREATE_DIRECTORY status = %#x, want idempotent success", h.Status)
	}

	chk := ntReq(protocol.CommandCheckDirectory, tid, nil, ansiPathArea("docs"))
	if h := respHeader(t, svc.Dispatch(sess, chk)); h.Status != statusSuccess {
		t.Fatalf("CHECK_DIRECTORY status = %#x", h.Status)
	}

	rm := ntReq(protocol.CommandDeleteDirectory, tid, nil, ansiPathArea("docs"))
	if h := respHeader(t, svc.Dispatch(sess, rm)); h.Status != statusSuccess {
		t.Fatalf("DELETE_DIRECTORY status = %#x", h.Status)
	}
	if h := respHeader(t, svc.Dispatch(sess, chk)); h.Status != statusObjectPathNotFound {
		t.Fatalf("CHECK_DIRECTORY after rmdir status = %#x, want PATH_NOT_FOUND", h.Status)
	}
}

// TestPathOps_DeleteFile proves DELETE removes a file and a second DELETE fails
// with OBJECT_NAME_NOT_FOUND.
func TestPathOps_DeleteFile(t *testing.T) {
	svc, sess, tid := fsService(t)
	fid := createFile(t, svc, sess, tid, "tmp.dat")
	sess.closeFID(fid)

	del := ntReq(protocol.CommandDelete, tid, make([]byte, 2), ansiPathArea("tmp.dat"))
	if h := respHeader(t, svc.Dispatch(sess, del)); h.Status != statusSuccess {
		t.Fatalf("DELETE status = %#x", h.Status)
	}
	if h := respHeader(t, svc.Dispatch(sess, del)); h.Status != statusObjectNameNotFound {
		t.Fatalf("second DELETE status = %#x, want NAME_NOT_FOUND", h.Status)
	}
}

// TestPathOps_Rename proves RENAME moves a file: the old name no longer opens and
// the new name does.
func TestPathOps_Rename(t *testing.T) {
	svc, sess, tid := fsService(t)
	fid := createFile(t, svc, sess, tid, "old.txt")
	writeAll(t, svc, sess, tid, fid, []byte("data"))
	sess.closeFID(fid)

	// RENAME byte area: two buffer-format-prefixed names.
	area := append(ansiPathArea("old.txt"), ansiPathArea("new.txt")...)
	ren := ntReq(protocol.CommandRename, tid, make([]byte, 2), area)
	if h := respHeader(t, svc.Dispatch(sess, ren)); h.Status != statusSuccess {
		t.Fatalf("RENAME status = %#x", h.Status)
	}

	// Old gone, new present.
	if st := openExisting(t, svc, sess, tid, "old.txt"); st != statusObjectNameNotFound {
		t.Fatalf("old name after rename status = %#x, want NAME_NOT_FOUND", st)
	}
	if st := openExisting(t, svc, sess, tid, "new.txt"); st != statusSuccess {
		t.Fatalf("new name after rename status = %#x, want success", st)
	}
}

// openExisting drives OPEN_ANDX (open-existing, read) and returns the reply
// status, closing the FID on success.
func openExisting(t *testing.T, svc *Service, sess *smbSession, tid uint16, path string) uint32 {
	t.Helper()
	ow := make([]byte, 30)
	ow[0] = protocol.CommandNoAndXCommand
	bp.PutLE16(ow[16:18], 0x01) // open existing, no create
	reply := svc.Dispatch(sess, ntReq(protocol.CommandOpenAndX, tid, ow, ansiPathArea(path)))
	h := respHeader(t, reply)
	if h.Status == statusSuccess {
		sess.closeFID(bp.LE16(reply[protocol.HeaderLen+5 : protocol.HeaderLen+7]))
	}
	return h.Status
}

// TestPathOps_QueryInformation proves QUERY_INFORMATION returns a file's size and
// the archive attribute.
func TestPathOps_QueryInformation(t *testing.T) {
	svc, sess, tid := fsService(t)
	fid := createFile(t, svc, sess, tid, "size.bin")
	writeAll(t, svc, sess, tid, fid, []byte("0123456789"))
	sess.closeFID(fid)

	reply := svc.Dispatch(sess, ntReq(protocol.CommandQueryInformation, tid, nil, ansiPathArea("size.bin")))
	h := respHeader(t, reply)
	if h.Status != statusSuccess {
		t.Fatalf("QUERY_INFORMATION status = %#x", h.Status)
	}
	w := reply[protocol.HeaderLen+1:]
	attrs := bp.LE16(w[0:2])
	size := bp.LE32(w[6:10])
	if attrs&attrArchive == 0 {
		t.Errorf("attrs = %#x, want archive bit set", attrs)
	}
	if size != 10 {
		t.Errorf("size = %d, want 10", size)
	}
}

// TestPathOps_ReadOnlyShareDeniesMutation proves a mutating command on a
// read-only share is refused with STATUS_ACCESS_DENIED.
func TestPathOps_ReadOnlyShareDeniesMutation(t *testing.T) {
	sh := newReadOnlyShare(t)
	svc := &Service{shares: []*Share{sh}}
	sess := newSession()
	tid := sess.allocTID(&treeConnect{share: sh})

	mk := ntReq(protocol.CommandCreateDirectory, tid, nil, ansiPathArea("docs"))
	if h := respHeader(t, svc.Dispatch(sess, mk)); h.Status != statusAccessDenied {
		t.Fatalf("mkdir on read-only share status = %#x, want ACCESS_DENIED", h.Status)
	}
}

// TestClamp16 proves the legacy SMB disk-info unit fields SATURATE at the 16-bit
// maximum rather than wrapping: a disk whose unit count exceeds 0xFFFF must report
// a full 0xFFFF units, never a wrapped smaller value.
func TestClamp16(t *testing.T) {
	cases := []struct {
		in   uint64
		want uint16
	}{
		{0, 0},
		{1, 1},
		{0xFFFF, 0xFFFF},
		{0x10000, 0xFFFF}, // one over → capped
		{1 << 40, 0xFFFF}, // enormous → capped, not wrapped to 0
	}
	for _, tc := range cases {
		if got := clamp16(tc.in); got != tc.want {
			t.Errorf("clamp16(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}
