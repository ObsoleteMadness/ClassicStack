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
// local_fs directory (the metastore MetaEngine derives 8.3 names). It bypasses
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
		FSType:      "local_fs",
		MetaBackend: "metastore",
		Metastore:   "mem",
		Path:        dir,
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

func TestInstallChk(t *testing.T) {
	s, _ := newTestService(t)
	s.SetServerName("TESTSRV")
	status, payload, _ := s.dispatch(req(1, proto.OpInstallChk, nil))
	if status != proto.ErrNone {
		t.Fatalf("install check status = %#x", status)
	}
	if !bytes.Contains(payload, []byte("TESTSRV")) {
		t.Errorf("install check reply missing server name: %q", payload)
	}
}

// TestAutoDiscoveryProbe mirrors the reference client's auto-discovery: it
// broadcasts an ordinary AL_DISKSPACE query for the drive it is about to map
// (there is no dedicated wire opcode for discovery) and learns the server's MAC
// from whichever reply arrives. Two wire details are load-bearing here (both
// caught the hard way against a real client — see spec/errata.md): AX must be
// readable at its header position (58-59), not smuggled as leading payload
// bytes; and AL_DISKSPACE's payload must be exactly 6 bytes (BX/CX/DX) with AX
// carrying the fixed DiskSpaceStatus DATA word (not ErrNone) — the reference
// client's sendquery() call site checks the reply length `== 6` literally
// (`if (sendquery(AL_DISKSPACE, i, 0, &answer, &ax, 1) != 6) { "no server found" }`),
// so an 8-byte (or otherwise wrong-length) reply is silently treated as no
// answer at all and discovery fails with "No EtherDFS server found on the LAN".
func TestAutoDiscoveryProbe(t *testing.T) {
	s, _ := newTestService(t)
	status, payload, ok := s.dispatch(req(1, proto.OpDiskspace, nil))
	if !ok {
		t.Fatal("auto-discovery probe got no reply")
	}
	if status != proto.DiskSpaceStatus {
		t.Fatalf("auto-discovery probe status = %#x, want DiskSpaceStatus (%#x)", status, proto.DiskSpaceStatus)
	}
	if len(payload) != 6 {
		t.Fatalf("diskspace payload len = %d, want 6 (BX/CX/DX only)", len(payload))
	}
}

func TestUnknownDrive(t *testing.T) {
	s, _ := newTestService(t)
	r := req(1, proto.OpDiskspace, nil)
	r.Drive = 9 // unconfigured
	status, _, _ := s.dispatch(r)
	if status != proto.ErrPathNotFound {
		t.Fatal("unknown drive should be path-not-found")
	}
}

func TestDiskSpace(t *testing.T) {
	s, _ := newTestService(t)
	status, payload, _ := s.dispatch(req(1, proto.OpDiskspace, nil))
	if status != proto.DiskSpaceStatus {
		t.Fatalf("diskspace status = %#x, want DiskSpaceStatus (%#x)", status, proto.DiskSpaceStatus)
	}
	if len(payload) != 6 {
		t.Fatalf("diskspace reply len = %d, want 6", len(payload))
	}
}

func TestMkdirGetAttrChdirRmdir(t *testing.T) {
	s, dir := newTestService(t)

	if status, _, _ := s.dispatch(req(1, proto.OpMkdir, []byte(`SUB`))); status != proto.ErrNone {
		t.Fatal("mkdir failed")
	}
	if _, err := os.Stat(filepath.Join(dir, "SUB")); err != nil {
		t.Fatalf("dir not created: %v", err)
	}

	// GETATTR on the directory reports the directory bit.
	status, payload, _ := s.dispatch(req(2, proto.OpGetattr, []byte(`SUB`)))
	if status != proto.ErrNone {
		t.Fatalf("getattr status = %#x", status)
	}
	if len(payload) != 9 {
		t.Fatalf("getattr reply len = %d", len(payload))
	}
	if payload[8]&proto.AttrDirectory == 0 {
		t.Errorf("directory attr not set: %#x", payload[8])
	}

	if status, _, _ := s.dispatch(req(3, proto.OpChdir, []byte(`SUB`))); status != proto.ErrNone {
		t.Fatal("chdir into existing dir failed")
	}
	if status, _, _ := s.dispatch(req(4, proto.OpChdir, []byte(`NOPE`))); status != proto.ErrPathNotFound {
		t.Fatal("chdir into missing dir should fail")
	}
	if status, _, _ := s.dispatch(req(5, proto.OpRmdir, []byte(`SUB`))); status != proto.ErrNone {
		t.Fatal("rmdir failed")
	}
}

func TestCreateWriteReadCloseDelete(t *testing.T) {
	s, dir := newTestService(t)

	// AL_CREATE: attr(2) + action(2) + openmode(2) + name (the fixed 6-byte
	// SS/CC/MM prefix is always present, even for AL_CREATE, which ignores CC/MM).
	createBody := append([]byte{0x20, 0x00, 0, 0, 0, 0}, []byte("HELLO.TXT")...)
	status, payload, _ := s.dispatch(req(1, proto.OpCreate, createBody))
	if status != proto.ErrNone {
		t.Fatalf("create status = %#x", status)
	}
	// AL_CREATE reply: attr(1) + fcb(11) + time(4) + size(4) + fileid(2) + action(2) + mode(1) = 25.
	if len(payload) != 25 {
		t.Fatalf("create reply len = %d, want 25", len(payload))
	}
	// File ID is at offset 1+11+8 (attr, fcb, time(4), size(4)).
	fidOff := 1 + proto.FCBNameLen + 8
	fid := uint16(payload[fidOff]) | uint16(payload[fidOff+1])<<8

	// AL_WRITEFIL: offset(4) + fileid(2) + data.
	data := []byte("the quick brown fox")
	writeBody := []byte{0, 0, 0, 0, byte(fid), byte(fid >> 8)}
	writeBody = append(writeBody, data...)
	wstatus, wpayload, _ := s.dispatch(req(2, proto.OpWritefil, writeBody))
	if wstatus != proto.ErrNone {
		t.Fatalf("write status = %#x", wstatus)
	}
	if got := uint16(wpayload[0]) | uint16(wpayload[1])<<8; int(got) != len(data) {
		t.Fatalf("wrote %d bytes, want %d", got, len(data))
	}
	if b, err := os.ReadFile(filepath.Join(dir, "HELLO.TXT")); err != nil || !bytes.Equal(b, data) {
		t.Fatalf("file content = %q (err %v), want %q", b, err, data)
	}

	// AL_READFIL: offset(4) + fileid(2) + length(2).
	readBody := []byte{0, 0, 0, 0, byte(fid), byte(fid >> 8), byte(len(data)), 0}
	rstatus, rpayload, _ := s.dispatch(req(3, proto.OpReadfil, readBody))
	if rstatus != proto.ErrNone {
		t.Fatalf("read status = %#x", rstatus)
	}
	if !bytes.Equal(rpayload, data) {
		t.Fatalf("read = %q, want %q", rpayload, data)
	}

	// AL_CLSFIL: file id in the body.
	s.dispatch(req(4, proto.OpClsfil, []byte{byte(fid), byte(fid >> 8)}))

	// AL_DELETE.
	if status, _, _ := s.dispatch(req(5, proto.OpDelete, []byte("HELLO.TXT"))); status != proto.ErrNone {
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
	status, payload, _ := s.dispatch(req(1, proto.OpFindFirst, append([]byte{0x00}, []byte("*.*")...)))
	if status == proto.ErrNoMoreFiles {
		t.Fatal("findfirst found nothing")
	}

	names := map[string]bool{}
	collect := func(status uint16, payload []byte) (uint16, uint16, bool) {
		if status == proto.ErrNoMoreFiles {
			return 0, 0, false
		}
		var fcb [proto.FCBNameLen]byte
		copy(fcb[:], payload[1:1+proto.FCBNameLen])
		names[proto.FCBToFilename(fcb)] = true
		o := 1 + proto.FCBNameLen + 8
		dirID := uint16(payload[o]) | uint16(payload[o+1])<<8
		pos := uint16(payload[o+2]) | uint16(payload[o+3])<<8
		return dirID, pos, true
	}
	dirID, pos, ok := collect(status, payload)
	seq := uint8(2)
	for ok {
		// AL_FINDNEXT: dirid(2) + pos(2) + attr(1) + fcbmask(11). Each request uses a
		// fresh sequence number (as a real client does) so the per-client retransmit
		// cache does not replay the previous reply.
		body := []byte{byte(dirID), byte(dirID >> 8), byte(pos), byte(pos >> 8), 0x00}
		mask := proto.FilenameToFCB("*.*") // wildcard-less; dispatch re-derives from cursor
		body = append(body, mask[:]...)
		seq++
		nstatus, npayload, _ := s.dispatch(req(seq, proto.OpFindNext, body))
		dirID, pos, ok = collect(nstatus, npayload)
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
	if status, _, _ := s.dispatch(req(1, proto.OpRename, body)); status != proto.ErrNone {
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
	if status, _, _ := s.dispatch(r); status != proto.ErrNone {
		t.Fatal("first mkdir failed")
	}
	if status, _, _ := s.dispatch(r); status != proto.ErrNone {
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
	if status, _, _ := s.dispatch(req(1, proto.OpSetattr, append([]byte{want}, []byte("DOC.TXT")...))); status != proto.ErrNone {
		t.Fatal("setattr failed")
	}
	// AL_GETATTR reads them back.
	status, payload, _ := s.dispatch(req(2, proto.OpGetattr, []byte("DOC.TXT")))
	if status != proto.ErrNone {
		t.Fatalf("getattr status = %#x", status)
	}
	if len(payload) != 9 {
		t.Fatalf("getattr reply len = %d", len(payload))
	}
	got := payload[8]
	if got&proto.AttrHidden == 0 || got&proto.AttrSystem == 0 {
		t.Errorf("Hidden/System not persisted: got %#x", got)
	}
}

func TestReadOnlyDriveRejectsWrites(t *testing.T) {
	dir := t.TempDir()
	s := &Service{drives: make(map[uint8]*Drive), sessions: newSessionTable()}
	spec := DriveSpec{Name: "E", Share: fs.ShareSpec{
		FSType: "local_fs", MetaBackend: "metastore", Metastore: "mem", Path: dir, ReadOnly: true,
	}}
	if err := s.ReconcileDrives([]DriveSpec{spec}); err != nil {
		t.Fatal(err)
	}
	if status, _, _ := s.dispatch(req(1, proto.OpMkdir, []byte("X"))); status != proto.ErrAccessDenied {
		t.Fatal("read-only drive should reject mkdir")
	}
	if status, _, _ := s.dispatch(req(2, proto.OpCreate, append([]byte{0, 0, 0, 0, 0, 0}, []byte("F.TXT")...))); status != proto.ErrAccessDenied {
		t.Fatal("read-only drive should reject create")
	}
}

// TestOpenReplyModeByte pins the reply Mode byte the client's SFT open_mode low
// byte comes from (ETHERDFS.C: "sftptr->open_mode |= answer[24]") — a real DOS
// COPY captured against this server (spec/errata.md) opened its destination via
// AL_SPOPNFIL, got back Mode=0 unconditionally, and then closed the handle
// without ever sending AL_WRITEFIL: DOS treats open_mode's low byte as the
// access code (0=read-only, 1=write-only, 2=read/write) and silently refuses to
// write through a handle it believes is read-only. Each opcode derives Mode
// differently in the reference server: AL_CREATE hardcodes read/write (2);
// AL_SPOPNFIL echoes the request's MM (open-mode) word masked to 7 bits; plain
// AL_OPEN echoes the request's SS word (which carries the requested access mode
// for OPEN, unlike AL_CREATE's SS).
func TestOpenReplyModeByte(t *testing.T) {
	s, dir := newTestService(t)
	modeOff := 1 + proto.FCBNameLen + 12 // attr + fcb + time(4) + size(4) + fileid(2) + action(2)
	fidOff := 1 + proto.FCBNameLen + 8

	closeFile := func(seq uint8, payload []byte) {
		fid := uint16(payload[fidOff]) | uint16(payload[fidOff+1])<<8
		s.dispatch(req(seq, proto.OpClsfil, []byte{byte(fid), byte(fid >> 8)}))
	}

	// AL_CREATE: SS/CC/MM prefix then name; Mode must always be 2 (read/write).
	createBody := append([]byte{0x20, 0, 0, 0, 0, 0}, []byte("A.TXT")...)
	status, payload, _ := s.dispatch(req(1, proto.OpCreate, createBody))
	if status != proto.ErrNone || payload[modeOff] != 2 {
		t.Fatalf("AL_CREATE Mode = %d (status %#x), want 2", payload[modeOff], status)
	}
	closeFile(10, payload)

	// AL_OPEN: SS carries the requested access mode (echoed back verbatim, & 0xff).
	if err := os.WriteFile(filepath.Join(dir, "B.TXT"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	openBody := append([]byte{0x02, 0, 0, 0, 0, 0}, []byte("B.TXT")...) // SS=2 (read/write access)
	status, payload, _ = s.dispatch(req(2, proto.OpOpen, openBody))
	if status != proto.ErrNone || payload[modeOff] != 0x02 {
		t.Fatalf("AL_OPEN Mode = %d (status %#x), want 2", payload[modeOff], status)
	}
	closeFile(11, payload)

	// AL_SPOPNFIL: real capture, "open if exists (low nibble 1), create if
	// missing (high nibble 1)" against an existing file, MM=0x0021 -> Mode=0x21.
	spopnBody := []byte{0x20, 0x00, 0x11, 0x01, 0x21, 0x00}
	spopnBody = append(spopnBody, []byte("B.TXT")...)
	status, payload, _ = s.dispatch(req(3, proto.OpSpopnfil, spopnBody))
	if status != proto.ErrNone || payload[modeOff] != 0x21 {
		t.Fatalf("AL_SPOPNFIL Mode = %#x (status %#x), want 0x21", payload[modeOff], status)
	}
	closeFile(12, payload)
}
