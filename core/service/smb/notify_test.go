package smb

import (
	"testing"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/smb"
)

// buildNotifyChangeRequest frames an NT_TRANSACT NOTIFY_CHANGE primary request:
// header (with the given tid/uid/mid) + WordCount(19) + the NT_TRANSACT words with
// SetupCount=4, Function=NOTIFY_CHANGE, and the Setup
// (CompletionFilter, FID, WatchTree, Reserved).
func buildNotifyChangeRequest(tid, uid, mid uint16, filter uint32, fid uint16, watchTree bool) []byte {
	h := smb.Header{Command: smb.CommandNtTransact, Flags2: smb.Flags2Unicode | smb.Flags2NTStatus, TID: tid, UID: uid, MID: mid}
	out := h.Encode(nil)

	const setupCount = 4              // CompletionFilter(2 words) + FID(1) + WatchTree/Reserved(1)
	const wordCount = 19 + setupCount // 18 fixed + Function + the Setup words
	out = append(out, wordCount)
	out = append(out, 0)        // MaxSetupCount
	out = append(out, 0, 0)     // Reserved1
	out = bp.AppendLE32(out, 0) // TotalParameterCount
	out = bp.AppendLE32(out, 0) // TotalDataCount
	out = bp.AppendLE32(out, 0) // MaxParameterCount
	out = bp.AppendLE32(out, 0) // MaxDataCount
	out = bp.AppendLE32(out, 0) // ParameterCount
	out = bp.AppendLE32(out, 0) // ParameterOffset
	out = bp.AppendLE32(out, 0) // DataCount
	out = bp.AppendLE32(out, 0) // DataOffset
	out = append(out, setupCount)
	out = bp.AppendLE16(out, ntTransactNotifyChange) // Function
	// Setup: CompletionFilter(4) FID(2) WatchTree(1) Reserved(1) = 4 words.
	out = bp.AppendLE32(out, filter)
	out = bp.AppendLE16(out, fid)
	wt := byte(0)
	if watchTree {
		wt = 1
	}
	out = append(out, wt, 0)
	out = bp.AppendLE16(out, 0) // ByteCount (no params/data)
	return out
}

// TestParseNtTransactSetup extracts Function + Setup from a built request.
func TestParseNtTransactSetup(t *testing.T) {
	req := buildNotifyChangeRequest(1, 2, 3, 0x17, 0x4002, true)
	fn, setup, ok := parseNtTransactSetup(req)
	if !ok {
		t.Fatal("parseNtTransactSetup ok=false")
	}
	if fn != ntTransactNotifyChange {
		t.Fatalf("Function = %#x, want NOTIFY_CHANGE", fn)
	}
	if len(setup) != 8 {
		t.Fatalf("setup len = %d, want 8", len(setup))
	}
	if got := bp.LE32(setup[0:4]); got != 0x17 {
		t.Fatalf("CompletionFilter = %#x, want 0x17", got)
	}
}

// TestNotifyChangeHeldThenCompleted: a NOTIFY_CHANGE on a bound disk tree gets NO
// immediate reply (it is held), and a subsequent FS change on that share pushes a
// completion frame over the circuit.
func TestNotifyChangeHeldThenCompleted(t *testing.T) {
	svc := &Service{shares: []*Share{newNamedTestShare(t, "PUBLIC")}, sessions: map[*smbSession]struct{}{}}
	conn := svc.NewConn()
	var pushed [][]byte
	conn.SetPushWriter(func(b []byte) { pushed = append(pushed, b) })
	sess := conn.sess
	tid := sess.allocTID(&treeConnect{share: svc.shares[0]})

	req := buildNotifyChangeRequest(tid, 1, 7, fileNotifyChangeModifiedFilter, 0, true)
	if reply := svc.Dispatch(sess, req); reply != nil {
		t.Fatalf("NOTIFY_CHANGE should be held (nil reply), got %d bytes", len(reply))
	}

	// A foreign FS mutation under the share completes the held watch.
	svc.notifyFSChange("PUBLIC", fs.Event{Op: fs.OpCreate, HostPath: "/srv/public/newfile.txt", Origin: "afp"})

	if len(pushed) != 1 {
		t.Fatalf("got %d pushed frames, want 1", len(pushed))
	}
	assertNotifyCompletion(t, pushed[0], tid, 7, fileActionAdded, "newfile.txt")
}

// TestNotifyChangeOneShot: the watch fires exactly once; a second change pushes
// nothing until the client re-arms.
func TestNotifyChangeOneShot(t *testing.T) {
	svc := &Service{shares: []*Share{newNamedTestShare(t, "PUBLIC")}, sessions: map[*smbSession]struct{}{}}
	conn := svc.NewConn()
	var pushed [][]byte
	conn.SetPushWriter(func(b []byte) { pushed = append(pushed, b) })
	sess := conn.sess
	tid := sess.allocTID(&treeConnect{share: svc.shares[0]})
	svc.Dispatch(sess, buildNotifyChangeRequest(tid, 1, 7, fileNotifyChangeModifiedFilter, 0, true))

	svc.notifyFSChange("PUBLIC", fs.Event{Op: fs.OpModify, HostPath: "/srv/public/a", Origin: "afp"})
	svc.notifyFSChange("PUBLIC", fs.Event{Op: fs.OpModify, HostPath: "/srv/public/b", Origin: "afp"})
	if len(pushed) != 1 {
		t.Fatalf("one-shot watch pushed %d frames, want 1", len(pushed))
	}
}

// TestNotifyChangeNoWatchNoPush: a change with no held watch pushes nothing.
func TestNotifyChangeNoWatchNoPush(t *testing.T) {
	svc := &Service{shares: []*Share{newNamedTestShare(t, "PUBLIC")}, sessions: map[*smbSession]struct{}{}}
	conn := svc.NewConn()
	var pushed [][]byte
	conn.SetPushWriter(func(b []byte) { pushed = append(pushed, b) })

	svc.notifyFSChange("PUBLIC", fs.Event{Op: fs.OpCreate, HostPath: "/srv/public/x", Origin: "afp"})
	if len(pushed) != 0 {
		t.Fatalf("no watch should push nothing, got %d", len(pushed))
	}
}

// TestNotifyChangeOnIPCRefused: a NOTIFY_CHANGE on the IPC$ pipe (no disk tree) is
// refused, not held — the client must not wait forever on a tree that cannot notify.
func TestNotifyChangeOnIPCRefused(t *testing.T) {
	svc := &Service{shares: []*Share{newNamedTestShare(t, "PUBLIC")}, sessions: map[*smbSession]struct{}{}}
	sess := newSession()
	tid := sess.allocTID(&treeConnect{ipc: true})
	reply := svc.Dispatch(sess, buildNotifyChangeRequest(tid, 1, 7, fileNotifyChangeModifiedFilter, 0, true))
	if reply == nil {
		t.Fatal("NOTIFY_CHANGE on IPC$ should be refused with a reply, not held")
	}
}

// fileNotifyChangeModifiedFilter is a representative CompletionFilter value (name +
// last-write) a client posts. Matching is share-coarse, so the exact bits don't
// gate; the constant documents intent.
const fileNotifyChangeModifiedFilter uint32 = 0x00000001 | 0x00000010

// newNamedTestShare builds a memfs share with the given tree name and a host-style
// path so reactor path-matching (when used) has a root.
func newNamedTestShare(t *testing.T, name string) *Share {
	t.Helper()
	sh, err := NewShare(ShareSpec{
		Name: name,
		Share: fs.ShareSpec{
			Name:          name,
			FSType:        "memfs",
			ForkBackend:   "ads",
			FilenameCodec: "identity",
			Path:          "/srv/public",
		},
	})
	if err != nil {
		t.Fatalf("NewShare %q: %v", name, err)
	}
	return sh
}

// assertNotifyCompletion checks a pushed NOTIFY_CHANGE completion frame: the header
// echoes tid/mid with the reply flag and success, and the single
// FILE_NOTIFY_INFORMATION record carries the expected action + UTF-16 name.
func assertNotifyCompletion(t *testing.T, frame []byte, tid, mid uint16, action uint32, name string) {
	t.Helper()
	h, err := smb.DecodeHeader(frame)
	if err != nil {
		t.Fatalf("completion header: %v", err)
	}
	if h.Command != smb.CommandNtTransact || h.Flags&smb.FlagReply == 0 || h.Status != statusSuccess {
		t.Fatalf("completion header = %+v", h)
	}
	if h.TID != tid || h.MID != mid {
		t.Fatalf("completion ids tid=%d mid=%d, want %d/%d", h.TID, h.MID, tid, mid)
	}
	// Locate the FILE_NOTIFY_INFORMATION block via ParameterOffset (word 4..7 of NT
	// response words). Simpler: it is the last len(info) bytes; decode the action +
	// name from the tail.
	// info layout: NextEntryOffset(4) Action(4) FileNameLength(4) FileName(UTF-16).
	nameUTF16 := utf16le(name)
	infoLen := 12 + len(nameUTF16)
	if len(frame) < infoLen {
		t.Fatalf("frame too short for info block")
	}
	info := frame[len(frame)-infoLen:]
	if got := bp.LE32(info[4:8]); got != action {
		t.Fatalf("Action = %#x, want %#x", got, action)
	}
	if got := bp.LE32(info[8:12]); int(got) != len(nameUTF16) {
		t.Fatalf("FileNameLength = %d, want %d", got, len(nameUTF16))
	}
	if string(info[12:]) != string(nameUTF16) {
		t.Fatalf("FileName mismatch")
	}
}
