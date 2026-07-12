package ncp

import (
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	ncpproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/ncp"
)

// newTestService builds an NCP service with one memfs volume named SYS and a live
// connection + circuit to drive ServeRequest against.
func newTestService(t *testing.T) (*Service, *Conn) {
	t.Helper()
	svc := New(nil)
	if err := svc.ReconcileVolumes([]VolumeSpec{{
		Name: "SYS",
		Share: fs.ShareSpec{
			Name:          "SYS",
			FSType:        "memfs",
			ForkBackend:   "ads",
			FilenameCodec: "identity",
		},
	}}); err != nil {
		t.Fatalf("ReconcileVolumes: %v", err)
	}
	c, ok := svc.conns.Create([4]byte{0, 0, 0, 1}, [6]byte{1, 2, 3, 4, 5, 6}, [2]byte{0x40, 0x02})
	if !ok {
		t.Fatal("Create connection failed")
	}
	return svc, svc.NewConn(c)
}

// req builds an NCP TypeRequest header for function fn with the given body.
func req(fn uint8, body []byte) *ncpproto.RequestHeader {
	return &ncpproto.RequestHeader{Type: ncpproto.TypeRequest, Function: fn, Body: body}
}

// mux wraps a subfunction + args in the 0x16/0x17/0x22 multiplexed framing:
// 2-byte BE length, then the subfunction byte, then args.
func mux(sf uint8, args []byte) []byte {
	n := 1 + len(args)
	out := []byte{byte(n >> 8), byte(n), sf}
	return append(out, args...)
}

// byteStr length-prefixes a string with a single length byte.
func byteStr(s string) []byte {
	return append([]byte{byte(len(s))}, []byte(s)...)
}

func TestServeRequest_GetServerInfo(t *testing.T) {
	svc, cn := newTestService(t)
	svc.SetServerName("testbox")

	completion, body := cn.ServeRequest(req(fnConnBindery, mux(sf17GetServerInfo, nil)))
	if completion != ncpproto.CompletionSuccess {
		t.Fatalf("completion = %#x, want success", completion)
	}
	if len(body) < 48 {
		t.Fatalf("server-info body too short: %d", len(body))
	}
	// The first 48 bytes are the NUL-padded server name, upper-cased.
	name := string(trimNUL(body[:48]))
	if name != "TESTBOX" {
		t.Errorf("server name = %q, want TESTBOX", name)
	}
}

func TestServeRequest_UnsupportedFunction(t *testing.T) {
	svc, cn := newTestService(t)
	completion, _ := cn.ServeRequest(req(0xEE, nil)) // not a recognised function
	if completion != ncpproto.CompletionFuncNotSupp {
		t.Fatalf("completion = %#x, want func-not-supported", completion)
	}
	if got := svc.counters.unsupportedFn.Load(); got != 1 {
		t.Errorf("unsupported_fn counter = %d, want 1", got)
	}
}

// TestEndToEnd_OpenWriteReadClose drives the full create→login→alloc-dir→create→
// write→read→close path against the memfs volume.
func TestEndToEnd_OpenWriteReadClose(t *testing.T) {
	svc, cn := newTestService(t)

	// Login (cleartext, no authenticator wired → guest grant).
	loginArgs := append([]byte{0, 1}, byteStr("SUPERVISOR")...) // object type + name
	loginArgs = append(loginArgs, byteStr("secret")...)         // password
	if completion, _ := cn.ServeRequest(req(fnConnBindery, mux(sf17LoginUnencrypted, loginArgs))); completion != ncpproto.CompletionSuccess {
		t.Fatalf("login completion = %#x", completion)
	}
	if !cn.c.loggedIn {
		t.Fatal("connection not marked logged-in")
	}

	// Allocate a directory handle at the volume root. mars_nwe alloc args (after the
	// subfunction byte): src-handle(1), drive letter(1), then the length-prefixed
	// "VOL:" path.
	allocArgs := append([]byte{0 /*src handle*/, 0 /*drive*/}, byteStr("SYS:")...)
	completion, body := cn.ServeRequest(req(fnDirServices, mux(sf16AllocPermDir, allocArgs)))
	if completion != ncpproto.CompletionSuccess || len(body) < 1 {
		t.Fatalf("alloc dir handle: completion=%#x body=%v", completion, body)
	}
	dirHandle := body[0]

	// Create a file FOO.TXT under the handle (create args: dirhandle, attr, len, name).
	createArgs := append([]byte{dirHandle, 0 /*attr*/}, byteStr("FOO.TXT")...)
	completion, body = cn.ServeRequest(req(fnCreateFile, createArgs))
	if completion != ncpproto.CompletionSuccess || len(body) < 6 {
		t.Fatalf("create file: completion=%#x body len=%d", completion, len(body))
	}
	// The open/create reply prefix is ext_fhandle[2]+fhandle[4]; the client echoes it
	// preceded by a filler byte on read/write/close.
	handlePrefix := body[:6]
	fileHandle := append([]byte{0 /*filler*/}, handlePrefix...)

	// Write "hello" at offset 0 (args: filler+handle, offset[4], size[2], data).
	data := []byte("hello")
	writeArgs := append([]byte{}, fileHandle...)
	writeArgs = appendBE32(writeArgs, 0)                               // offset
	writeArgs = append(writeArgs, byte(len(data)>>8), byte(len(data))) // length BE
	writeArgs = append(writeArgs, data...)
	if completion, _ = cn.ServeRequest(req(fnWriteFile, writeArgs)); completion != ncpproto.CompletionSuccess {
		t.Fatalf("write file completion = %#x", completion)
	}

	// Read it back (args: filler+handle, offset[4], max_size[2]).
	readArgs := append([]byte{}, fileHandle...)
	readArgs = appendBE32(readArgs, 0)
	readArgs = append(readArgs, 0, byte(len(data)))
	completion, body = cn.ServeRequest(req(fnReadFile, readArgs))
	if completion != ncpproto.CompletionSuccess {
		t.Fatalf("read file completion = %#x", completion)
	}
	if len(body) < 2 {
		t.Fatalf("read reply too short: %v", body)
	}
	n := int(body[0])<<8 | int(body[1])
	if got := string(body[2 : 2+n]); got != "hello" {
		t.Errorf("read = %q, want hello", got)
	}

	// Close the file.
	if completion, _ = cn.ServeRequest(req(fnCloseFile, fileHandle)); completion != ncpproto.CompletionSuccess {
		t.Fatalf("close completion = %#x", completion)
	}

	// Stats reflect the activity: one connection, one logged-in user, zero open files.
	st := svc.Stats()
	if st.Gauges["connected_machines"] != 1 {
		t.Errorf("connected_machines = %v, want 1", st.Gauges["connected_machines"])
	}
	if st.Gauges["logged_in_users"] != 1 {
		t.Errorf("logged_in_users = %v, want 1", st.Gauges["logged_in_users"])
	}
	if st.Gauges["open_files"] != 0 {
		t.Errorf("open_files = %v, want 0 after close", st.Gauges["open_files"])
	}
	if st.Counters["logins_ok"] != 1 {
		t.Errorf("logins_ok = %d, want 1", st.Counters["logins_ok"])
	}
}

// trimNUL drops trailing NUL bytes from a fixed field.
func trimNUL(b []byte) []byte {
	for len(b) > 0 && b[len(b)-1] == 0 {
		b = b[:len(b)-1]
	}
	return b
}

func appendBE32(dst []byte, v uint32) []byte {
	return append(dst, byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}
