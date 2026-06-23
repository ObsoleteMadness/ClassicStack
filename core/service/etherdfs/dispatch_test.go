package etherdfs

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	proto "github.com/ObsoleteMadness/ClassicStack/core/protocol/etherdfs"
)

// newTestService builds a service with a single drive E backed by a temp
// local_fs directory (NameEngine "short" so 8.3 names are derived). It bypasses
// the port (the dispatch is exercised directly) so the wire/link half is not
// needed.
func newTestService(t *testing.T) (*Service, string) {
	t.Helper()
	dir := t.TempDir()
	s := &Service{
		drives:   make(map[uint8]*Drive),
		sessions: newSessionTable(),
	}
	spec := DriveSpec{Name: "E", Share: fs.ShareSpec{
		FSType:     "local_fs",
		NameEngine: "short",
		Metastore:  "mem",
		Path:       dir,
	}}
	if err := s.ReconcileDrives([]DriveSpec{spec}); err != nil {
		t.Fatalf("ReconcileDrives: %v", err)
	}
	return s, dir
}

// req builds a request Frame for drive E (number 4) with the given opcode/payload.
func req(seq uint8, op uint8, payload []byte) proto.Frame {
	return proto.Frame{
		SrcMAC:   [6]byte{0x02, 0, 0, 0, 0, 0x10},
		Sequence: seq,
		Drive:    4, // E
		Opcode:   op,
		Payload:  payload,
	}
}

// ax reads the leading 2-byte status word from a reply.
func ax(reply []byte) uint16 {
	if len(reply) < 2 {
		return 0xFFFF
	}
	return uint16(reply[0]) | uint16(reply[1])<<8
}

func TestInstallChk(t *testing.T) {
	s, _ := newTestService(t)
	s.SetServerName("TESTSRV")
	reply := s.dispatch(req(1, proto.OpInstallChk, nil))
	if ax(reply) != proto.ErrNone {
		t.Fatalf("install check status = %#x", ax(reply))
	}
	if !bytes.Contains(reply, []byte("TESTSRV")) {
		t.Errorf("install check reply missing server name: %q", reply)
	}
}

func TestUnknownDrive(t *testing.T) {
	s, _ := newTestService(t)
	r := req(1, proto.OpDiskspace, nil)
	r.Drive = 9 // unconfigured
	if ax(s.dispatch(r)) != proto.ErrPathNotFound {
		t.Fatal("unknown drive should be path-not-found")
	}
}

func TestDiskSpace(t *testing.T) {
	s, _ := newTestService(t)
	reply := s.dispatch(req(1, proto.OpDiskspace, nil))
	if ax(reply) != proto.ErrNone {
		t.Fatalf("diskspace status = %#x", ax(reply))
	}
	if len(reply) != 10 {
		t.Fatalf("diskspace reply len = %d, want 10", len(reply))
	}
}

func TestMkdirGetAttrChdirRmdir(t *testing.T) {
	s, dir := newTestService(t)

	if ax(s.dispatch(req(1, proto.OpMkdir, []byte(`SUB`)))) != proto.ErrNone {
		t.Fatal("mkdir failed")
	}
	if _, err := os.Stat(filepath.Join(dir, "SUB")); err != nil {
		t.Fatalf("dir not created: %v", err)
	}

	// GETATTR on the directory reports the directory bit.
	reply := s.dispatch(req(2, proto.OpGetattr, []byte(`SUB`)))
	if len(reply) != 9 {
		t.Fatalf("getattr reply len = %d", len(reply))
	}
	if reply[8]&proto.AttrDirectory == 0 {
		t.Errorf("directory attr not set: %#x", reply[8])
	}

	if ax(s.dispatch(req(3, proto.OpChdir, []byte(`SUB`)))) != proto.ErrNone {
		t.Fatal("chdir into existing dir failed")
	}
	if ax(s.dispatch(req(4, proto.OpChdir, []byte(`NOPE`)))) != proto.ErrPathNotFound {
		t.Fatal("chdir into missing dir should fail")
	}
	if ax(s.dispatch(req(5, proto.OpRmdir, []byte(`SUB`)))) != proto.ErrNone {
		t.Fatal("rmdir failed")
	}
}

func TestCreateWriteReadCloseDelete(t *testing.T) {
	s, dir := newTestService(t)

	// AL_CREATE: attr(2) + name.
	createBody := append([]byte{0x20, 0x00}, []byte("HELLO.TXT")...)
	reply := s.dispatch(req(1, proto.OpCreate, createBody))
	// AL_CREATE reply: attr(1) + fcb(11) + time(4) + size(4) + fileid(2) + mode(1) = 23.
	if len(reply) != 23 {
		t.Fatalf("create reply len = %d, want 23", len(reply))
	}
	// File ID is at offset 1+11+8 (attr, fcb, time(4), size(4)).
	fidOff := 1 + proto.FCBNameLen + 8
	fid := uint16(reply[fidOff]) | uint16(reply[fidOff+1])<<8

	// AL_WRITEFIL: offset(4) + fileid(2) + data.
	data := []byte("the quick brown fox")
	writeBody := []byte{0, 0, 0, 0, byte(fid), byte(fid >> 8)}
	writeBody = append(writeBody, data...)
	wreply := s.dispatch(req(2, proto.OpWritefil, writeBody))
	if got := uint16(wreply[0]) | uint16(wreply[1])<<8; int(got) != len(data) {
		t.Fatalf("wrote %d bytes, want %d", got, len(data))
	}
	if b, err := os.ReadFile(filepath.Join(dir, "HELLO.TXT")); err != nil || !bytes.Equal(b, data) {
		t.Fatalf("file content = %q (err %v), want %q", b, err, data)
	}

	// AL_READFIL: offset(4) + fileid(2) + length(2).
	readBody := []byte{0, 0, 0, 0, byte(fid), byte(fid >> 8), byte(len(data)), 0}
	rreply := s.dispatch(req(3, proto.OpReadfil, readBody))
	if !bytes.Equal(rreply, data) {
		t.Fatalf("read = %q, want %q", rreply, data)
	}

	// AL_CLSFIL: file id in the body.
	s.dispatch(req(4, proto.OpClsfil, []byte{byte(fid), byte(fid >> 8)}))

	// AL_DELETE.
	if ax(s.dispatch(req(5, proto.OpDelete, []byte("HELLO.TXT")))) != proto.ErrNone {
		t.Fatal("delete failed")
	}
	if _, err := os.Stat(filepath.Join(dir, "HELLO.TXT")); !os.IsNotExist(err) {
		t.Fatal("file not deleted")
	}
}

func TestFindFirstNextShortNames(t *testing.T) {
	s, dir := newTestService(t)
	// Two long names that collide on the same 8.3 stem exercise the ~N suffixing.
	for _, name := range []string{"ReportFinal2024.xlsx", "ReportFinalDraft.xlsx", "a.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// AL_FINDFIRST: attr(1) + "*.*" search path.
	reply := s.dispatch(req(1, proto.OpFindFirst, append([]byte{0x00}, []byte("*.*")...)))
	if ax(reply) == proto.ErrNoMoreFiles {
		t.Fatal("findfirst found nothing")
	}

	names := map[string]bool{}
	collect := func(reply []byte) (uint16, uint16, bool) {
		if ax(reply) == proto.ErrNoMoreFiles {
			return 0, 0, false
		}
		var fcb [proto.FCBNameLen]byte
		copy(fcb[:], reply[1:1+proto.FCBNameLen])
		names[proto.FCBToFilename(fcb)] = true
		o := 1 + proto.FCBNameLen + 8
		dirID := uint16(reply[o]) | uint16(reply[o+1])<<8
		pos := uint16(reply[o+2]) | uint16(reply[o+3])<<8
		return dirID, pos, true
	}
	dirID, pos, ok := collect(reply)
	seq := uint8(2)
	for ok {
		// AL_FINDNEXT: dirid(2) + pos(2) + attr(1) + fcbmask(11). Each request uses a
		// fresh sequence number (as a real client does) so the per-client retransmit
		// cache does not replay the previous reply.
		body := []byte{byte(dirID), byte(dirID >> 8), byte(pos), byte(pos >> 8), 0x00}
		mask := proto.FilenameToFCB("*.*") // wildcard-less; dispatch re-derives from cursor
		body = append(body, mask[:]...)
		seq++
		nreply := s.dispatch(req(seq, proto.OpFindNext, body))
		dirID, pos, ok = collect(nreply)
	}

	// All three files must appear, with the two colliding names mapped to distinct
	// 8.3 stems (REPORT~1 / REPORT~2).
	if len(names) != 3 {
		t.Fatalf("found %d names, want 3: %v", len(names), names)
	}
	var report int
	for n := range names {
		if bytes.HasPrefix([]byte(n), []byte("REPORT~")) {
			report++
		}
	}
	if report != 2 {
		t.Errorf("expected 2 REPORT~N short names, got %d: %v", report, names)
	}
}

func TestRename(t *testing.T) {
	s, dir := newTestService(t)
	if err := os.WriteFile(filepath.Join(dir, "OLD.TXT"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// AL_RENAME: srclen(1) + src + dst.
	body := append([]byte{7}, []byte("OLD.TXT")...)
	body = append(body, []byte("NEW.TXT")...)
	if ax(s.dispatch(req(1, proto.OpRename, body))) != proto.ErrNone {
		t.Fatal("rename failed")
	}
	if _, err := os.Stat(filepath.Join(dir, "NEW.TXT")); err != nil {
		t.Fatalf("renamed file missing: %v", err)
	}
}

func TestSequenceDedup(t *testing.T) {
	s, dir := newTestService(t)
	// First MKDIR creates the dir; a replayed frame (same sequence) must NOT error
	// with "already exists" — it replays the cached success reply.
	r := req(7, proto.OpMkdir, []byte("ONCE"))
	if ax(s.dispatch(r)) != proto.ErrNone {
		t.Fatal("first mkdir failed")
	}
	if ax(s.dispatch(r)) != proto.ErrNone {
		t.Fatal("replayed mkdir should return cached success, not an error")
	}
	if _, err := os.Stat(filepath.Join(dir, "ONCE")); err != nil {
		t.Fatalf("dir missing: %v", err)
	}
}

func TestSetGetAttrPersists(t *testing.T) {
	s, dir := newTestService(t)
	if err := os.WriteFile(filepath.Join(dir, "DOC.TXT"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// AL_SETATTR: attr(1) + name. Set Hidden|System|Archive — Hidden/System cannot
	// be represented on a POSIX host, so they MUST come from the DOS-attr store.
	want := byte(proto.AttrHidden | proto.AttrSystem | proto.AttrArchive)
	if ax(s.dispatch(req(1, proto.OpSetattr, append([]byte{want}, []byte("DOC.TXT")...)))) != proto.ErrNone {
		t.Fatal("setattr failed")
	}
	// AL_GETATTR reads them back.
	reply := s.dispatch(req(2, proto.OpGetattr, []byte("DOC.TXT")))
	if len(reply) != 9 {
		t.Fatalf("getattr reply len = %d", len(reply))
	}
	got := reply[8]
	if got&proto.AttrHidden == 0 || got&proto.AttrSystem == 0 {
		t.Errorf("Hidden/System not persisted: got %#x", got)
	}
}

func TestReadOnlyDriveRejectsWrites(t *testing.T) {
	dir := t.TempDir()
	s := &Service{drives: make(map[uint8]*Drive), sessions: newSessionTable()}
	spec := DriveSpec{Name: "E", Share: fs.ShareSpec{
		FSType: "local_fs", NameEngine: "short", Metastore: "mem", Path: dir, ReadOnly: true,
	}}
	if err := s.ReconcileDrives([]DriveSpec{spec}); err != nil {
		t.Fatal(err)
	}
	if ax(s.dispatch(req(1, proto.OpMkdir, []byte("X")))) != proto.ErrAccessDenied {
		t.Fatal("read-only drive should reject mkdir")
	}
	if ax(s.dispatch(req(2, proto.OpCreate, append([]byte{0, 0}, []byte("F.TXT")...)))) != proto.ErrAccessDenied {
		t.Fatal("read-only drive should reject create")
	}
}
