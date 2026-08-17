package fuse

import (
	"bytes"
	"os"
	"testing"
)

func TestNamedForkPathRoundTrip(t *testing.T) {
	a := newTestAdapter(t, true, XattrLayoutApple)
	fh, err := a.Create("/doc", os.O_RDWR, 0o644)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	_ = a.Release("/doc", fh)

	rsrcPath := "/doc/" + namedForkDirName + "/" + namedForkRsrcName
	fh, err = a.Open(rsrcPath, os.O_RDWR)
	if err != nil {
		t.Fatalf("Open namedfork: %v", err)
	}
	payload := []byte("named-fork-bytes")
	n, err := a.Write(rsrcPath, payload, 0, fh)
	if err != nil || n != len(payload) {
		t.Fatalf("Write namedfork n=%d err=%v", n, err)
	}
	if err := a.Release(rsrcPath, fh); err != nil {
		t.Fatalf("Release: %v", err)
	}

	st, err := a.Getattr(rsrcPath, 0)
	if err != nil {
		t.Fatalf("Getattr namedfork: %v", err)
	}
	if st.IsDir || st.Size != int64(len(payload)) {
		t.Errorf("namedfork stat dir=%v size=%d, want file size %d", st.IsDir, st.Size, len(payload))
	}

	fh, err = a.Open(rsrcPath, os.O_RDONLY)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	buf := make([]byte, len(payload))
	rn, err := a.Read(rsrcPath, buf, 0, fh)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !bytes.Equal(buf[:rn], payload) {
		t.Errorf("Read got %q, want %q", buf[:rn], payload)
	}
	_ = a.Release(rsrcPath, fh)

	dirPath := "/doc/" + namedForkDirName
	ents, err := a.Readdir(dirPath, 0)
	if err != nil {
		t.Fatalf("Readdir namedfork dir: %v", err)
	}
	if len(ents) != 1 || ents[0].Name != namedForkRsrcName {
		t.Errorf("namedfork dir entries = %v, want [rsrc]", ents)
	}

	// Parent listing must not include ..namedfork.
	root, err := a.Readdir("/", 0)
	if err != nil {
		t.Fatalf("Readdir /: %v", err)
	}
	for _, e := range root {
		if e.Name == namedForkDirName {
			t.Error("..namedfork leaked into parent listing")
		}
	}
}

func TestNamedForkHiddenWhenNativeOff(t *testing.T) {
	a := newTestAdapter(t, false, XattrLayoutApple)
	fh, err := a.Create("/doc", os.O_RDWR, 0o644)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	_ = a.Release("/doc", fh)
	if _, err := a.Getattr("/doc/"+namedForkDirName+"/"+namedForkRsrcName, 0); err == nil {
		t.Error("namedfork getattr succeeded with NativeForks off")
	}
}

func TestSplitNamedFork(t *testing.T) {
	cases := []struct {
		in   string
		base string
		kind namedKind
	}{
		{"doc", "doc", namedNone},
		{"doc/" + namedForkDirName, "doc", namedForkDir},
		{"doc/" + namedForkDirName + "/" + namedForkRsrcName, "doc", namedForkRsrc},
		{namedForkDirName, "", namedForkDir},
	}
	for _, c := range cases {
		base, kind := splitNamedFork(c.in)
		if base != c.base || kind != c.kind {
			t.Errorf("splitNamedFork(%q) = (%q,%d), want (%q,%d)", c.in, base, kind, c.base, c.kind)
		}
	}
}
