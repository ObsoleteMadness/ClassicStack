package ncp

import (
	"testing"

	ncpproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/ncp"
)

// nsReq builds a function-0x57 request body: the subfunction byte at [0] then the
// supplied argument bytes (the name-space family puts the subfunction first, not
// behind a length prefix).
func nsReq(sub uint8, args []byte) *ncpproto.RequestHeader {
	body := append([]byte{sub}, args...)
	return req(fnNameSpace, body)
}

// hpath builds an NW_HPATH for a 4-byte base anchor with one path component.
func hpathBase(base uint32, volume uint8, components ...string) []byte {
	out := []byte{volume}
	out = append(out, byte(base), byte(base>>8), byte(base>>16), byte(base>>24))
	out = append(out, ncpproto.HPathFlagBase, byte(len(components)))
	for _, c := range components {
		out = append(out, byte(len(c)))
		out = append(out, c...)
	}
	return out
}

// hpathHandle builds an NW_HPATH anchored on a 1-byte DOS dir handle.
func hpathHandle(handle uint8, components ...string) []byte {
	out := []byte{0 /*volume*/, handle, 0, 0, 0, ncpproto.HPathFlagHandle, byte(len(components))}
	for _, c := range components {
		out = append(out, byte(len(c)))
		out = append(out, c...)
	}
	return out
}

func TestNamespace_GetLoaded(t *testing.T) {
	_, cn := newTestService(t)
	// args: namespace(1) + dst(1) + volume(1) — body[2] is the volume.
	completion, body := cn.ServeRequest(nsReq(ncpproto.NSGetLoadedList, []byte{0, 0, 0}))
	if completion != ncpproto.CompletionSuccess {
		t.Fatalf("get-loaded completion=%#x", completion)
	}
	count := int(body[0])
	if count < 1 {
		t.Fatalf("namespace count = %d, want >=1", count)
	}
	got := map[uint8]bool{}
	for _, id := range body[2 : 2+count] {
		got[id] = true
	}
	if !got[ncpproto.NameDOS] || !got[ncpproto.NameOS2] || !got[ncpproto.NameMAC] {
		t.Errorf("loaded namespaces = %v, want DOS+OS2+MAC", body[2:2+count])
	}
}

// seedLongNameFile creates a long-named file in the volume root via the seam (as if
// placed there out of band), so the namespace search/info calls can find it.
func seedLongNameFile(t *testing.T, cn *Conn, name string) {
	t.Helper()
	vol, _ := cn.svc.volumeByIndex(0)
	f, err := vol.FS().CreateFile(name)
	if err != nil {
		t.Fatalf("seed CreateFile %q: %v", name, err)
	}
	_, _ = f.WriteAt([]byte("data"), 0)
	_ = f.Close()
}

func TestNamespace_GenDirBaseInitSearchAndSearch(t *testing.T) {
	_, cn := newTestService(t)
	seedLongNameFile(t, cn, "Quarterly Report.txt")

	// Generate a dir base at the volume root (OS/2 namespace), empty path.
	root := hpathHandle(0) // handle 0 = the root the DOS path uses... but we need a base.
	// Use a DOS dir handle bound to the root first, then gen-base from it.
	dh := cn.c.AllocDir(mustVol(t, cn), "")
	root = hpathHandle(dh)
	genArgs := append([]byte{ncpproto.NameOS2, ncpproto.NameOS2, 0}, root...)
	completion, body := cn.ServeRequest(nsReq(ncpproto.NSGenDirBase, genArgs))
	if completion != ncpproto.CompletionSuccess || len(body) < 9 {
		t.Fatalf("gen-dir-base completion=%#x len=%d", completion, len(body))
	}
	base := leU32(body[0:])

	// Initialize a search at that base.
	initArgs := append([]byte{ncpproto.NameOS2}, hpathBase(base, 0)...)
	completion, body = cn.ServeRequest(nsReq(ncpproto.NSInitSearch, initArgs))
	if completion != ncpproto.CompletionSuccess || len(body) < 9 {
		t.Fatalf("init-search completion=%#x len=%d", completion, len(body))
	}
	searchBase := leU32(body[1:])

	// Search: searchattrib[2]@2, infomask[4]@4, volume@8, base[4]@9, seq[4]@13, len@17, pattern@18.
	infomask := ncpproto.InfoMskEntryName | ncpproto.InfoMskAttributeInfo | ncpproto.InfoMskDataStreamSize
	args := make([]byte, 18)
	args[0] = ncpproto.NameOS2 // namespace at body[1] → args[0] after subfn
	// args index here is relative to body[1:]; rebuild explicitly:
	sargs := make([]byte, 17) // bytes after subfn: [0]=ns,[1..2]=attr,[3..6]=infomask,[7]=vol,[8..11]=base,[12..15]=seq,[16]=len
	sargs[0] = ncpproto.NameOS2
	// searchattrib (offset 2 in body = offset 1 here) — leave 0 (match files)
	putLE32(sargs[3:], infomask)
	putLE32(sargs[8:], searchBase)
	putLE32(sargs[12:], 0xFFFFFFFF) // before-first
	sargs[16] = 1                   // pattern len
	sargs = append(sargs, '*')
	completion, body = cn.ServeRequest(nsReq(ncpproto.NSSearch, sargs))
	if completion != ncpproto.CompletionSuccess {
		t.Fatalf("search completion=%#x", completion)
	}
	// Reply: next-sequence[4], then dir info. InfoMskEntryName is appended LAST, as a
	// 1-byte length then the name — so the trailing bytes are len|name.
	if len(body) < 4 {
		t.Fatalf("search reply too short: %v", body)
	}
	want := "Quarterly Report.txt"
	gotName, ok := trailingName(body)
	if !ok || gotName != want {
		t.Errorf("search returned name %q (ok=%v), want %q", gotName, ok, want)
	}
}

// trailingName extracts the InfoMskEntryName field (length-prefixed) from the tail
// of a dir-info reply.
func trailingName(body []byte) (string, bool) {
	for n := 1; n < len(body) && n <= 255; n++ {
		// The name field is the last (len, bytes) pair: try the candidate length at
		// position len(body)-1-n and see if it spans exactly to the end.
		pos := len(body) - 1 - n
		if pos < 4 {
			break
		}
		if int(body[pos]) == n {
			return string(body[pos+1:]), true
		}
	}
	return "", false
}

func TestNamespace_OS2CreateLongName(t *testing.T) {
	_, cn := newTestService(t)
	dh := cn.c.AllocDir(mustVol(t, cn), "")

	// Open/Create (0x57/0x01): namespace@1, mode@2, attrib[2]@3, infomask[4]@5,
	// creatattrib[4]@9, access[2]@13, NW_HPATH@14 — build the body after the subfn.
	name := "My Long Document.txt"
	hp := hpathHandle(dh, name)
	args := make([]byte, 13) // body[1..13] before the hpath
	args[0] = ncpproto.NameOS2
	args[1] = ncpproto.OpcModeCreat | ncpproto.OpcModeOpen
	putLE32(args[3:], ncpproto.InfoMskEntryName)
	args = append(args, hp...)
	completion, body := cn.ServeRequest(nsReq(ncpproto.NSOpenCreate, args))
	if completion != ncpproto.CompletionSuccess {
		t.Fatalf("os2-create completion=%#x", completion)
	}
	if len(body) < 8 {
		t.Fatalf("os2-create reply too short: %v", body)
	}
	action := body[6]
	if action != ncpproto.OpcActionCreat {
		t.Errorf("action = %#x, want create", action)
	}
	// The file now exists under its long name on the seam.
	vol, _ := cn.svc.volumeByIndex(0)
	if _, err := vol.FS().Stat(name); err != nil {
		t.Errorf("created long-named file not found on seam: %v", err)
	}
}

// TestNamespace_ObtainInfoCaseInsensitive proves a mis-cased long name still
// resolves to the stored file (the NetWare case-insensitive contract), via the
// shared fs.ResolveFold fold — even on a case-sensitive backend.
func TestNamespace_ObtainInfoCaseInsensitive(t *testing.T) {
	_, cn := newTestService(t)
	seedLongNameFile(t, cn, "Budget Plan.DOC")
	dh := cn.c.AllocDir(mustVol(t, cn), "")

	// Obtain Info (0x57/0x06) for a DIFFERENTLY-cased name: dst-ns@1, attr[2]@2,
	// infomask[4]@4, NW_HPATH@8.
	hp := hpathHandle(dh, "budget plan.doc")
	args := make([]byte, 7) // body[1..7] before hpath
	args[0] = ncpproto.NameOS2
	putLE32(args[3:], ncpproto.InfoMskEntryName|ncpproto.InfoMskDataStreamSize)
	args = append(args, hp...)
	completion, body := cn.ServeRequest(nsReq(ncpproto.NSObtainInfo, args))
	if completion != ncpproto.CompletionSuccess {
		t.Fatalf("obtain-info (mis-cased) completion=%#x — case fold failed", completion)
	}
	if name, ok := trailingName(body); !ok || name != "Budget Plan.DOC" {
		t.Errorf("obtain-info returned %q (ok=%v), want stored-case \"Budget Plan.DOC\"", name, ok)
	}
}

func mustVol(t *testing.T, cn *Conn) *Volume {
	t.Helper()
	v, ok := cn.svc.volumeByIndex(0)
	if !ok {
		t.Fatal("no volume 0")
	}
	return v
}

func putLE32(b []byte, v uint32) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
	b[3] = byte(v >> 24)
}
