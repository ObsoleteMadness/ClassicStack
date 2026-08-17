package fuse

import (
	"bytes"
	"os"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/appledouble"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

func TestAppleXattrRoundTrip(t *testing.T) {
	a := newTestAdapter(t, true, XattrLayoutApple)
	fh, err := a.Create("/doc", os.O_RDWR, 0o644)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	_ = a.Release("/doc", fh)

	var info [32]byte
	copy(info[:], []byte("TEXTttxt-finder-info-bytes-here!"))
	if err := a.Setxattr("/doc", xattrAppleFinderInfo, info[:], 0); err != nil {
		t.Fatalf("Setxattr FinderInfo: %v", err)
	}
	got, err := a.Getxattr("/doc", xattrAppleFinderInfo)
	if err != nil {
		t.Fatalf("Getxattr FinderInfo: %v", err)
	}
	if !bytes.Equal(got, info[:]) {
		t.Errorf("FinderInfo = %q, want %q", got, info[:])
	}

	rsrc := []byte("\x00\x01\x02resource-fork-bytes")
	if err := a.Setxattr("/doc", xattrAppleResourceFork, rsrc, 0); err != nil {
		t.Fatalf("Setxattr ResourceFork: %v", err)
	}
	got, err = a.Getxattr("/doc", xattrAppleResourceFork)
	if err != nil {
		t.Fatalf("Getxattr ResourceFork: %v", err)
	}
	if !bytes.Equal(got, rsrc) {
		t.Errorf("ResourceFork = %q, want %q", got, rsrc)
	}

	names, err := a.Listxattr("/doc")
	if err != nil {
		t.Fatalf("Listxattr: %v", err)
	}
	want := map[string]bool{xattrAppleFinderInfo: false, xattrAppleResourceFork: false}
	for _, n := range names {
		if _, ok := want[n]; ok {
			want[n] = true
		}
	}
	for n, seen := range want {
		if !seen {
			t.Errorf("Listxattr missing %s (got %v)", n, names)
		}
	}

	n, err := a.fsys.ForkLen("doc", fs.ResourceFork)
	if err != nil || n != int64(len(rsrc)) {
		t.Errorf("ForkEngine resource len=%d err=%v, want %d", n, err, len(rsrc))
	}
}

func TestAppleResourceForkPositionedWrite(t *testing.T) {
	a := newTestAdapter(t, true, XattrLayoutApple)
	fh, err := a.Create("/doc", os.O_RDWR, 0o644)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	_ = a.Release("/doc", fh)

	chunk1 := []byte("AAAA")
	chunk2 := []byte("BBBB")
	if err := a.SetxattrP("/doc", xattrAppleResourceFork, chunk1, 0, 0); err != nil {
		t.Fatalf("SetxattrP pos=0: %v", err)
	}
	if err := a.SetxattrP("/doc", xattrAppleResourceFork, chunk2, 0, 4); err != nil {
		t.Fatalf("SetxattrP pos=4: %v", err)
	}
	got, err := a.GetxattrP("/doc", xattrAppleResourceFork, 0)
	if err != nil {
		t.Fatalf("GetxattrP: %v", err)
	}
	want := append(chunk1, chunk2...)
	if !bytes.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestNetatalkXattrRoundTrip(t *testing.T) {
	a := newTestAdapter(t, true, XattrLayoutNetatalk)
	fh, err := a.Create("/doc", os.O_RDWR, 0o644)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	_ = a.Release("/doc", fh)

	var info [32]byte
	copy(info[:], []byte("APPLmdrp________________________"))
	p := appledouble.Parsed{FinderInfo: info, HasFinder: true, Comment: []byte("hi"), HasComment: true}
	meta := fs.EncodeNetatalkMetadataEA(p, 0)
	if len(meta) != fs.NetatalkMetadataSize {
		t.Fatalf("metadata blob len=%d, want %d", len(meta), fs.NetatalkMetadataSize)
	}
	if err := a.Setxattr("/doc", xattrUserPrefix+xattrNetatalkMetadata, meta, 0); err != nil {
		t.Fatalf("Setxattr Metadata: %v", err)
	}
	// Unprefixed name is also accepted.
	got, err := a.Getxattr("/doc", xattrNetatalkMetadata)
	if err != nil {
		t.Fatalf("Getxattr Metadata: %v", err)
	}
	out, _, err := fs.ParseNetatalkMetadataEA(got)
	if err != nil {
		t.Fatalf("ParseNetatalkMetadataEA: %v", err)
	}
	if out.FinderInfo != info {
		t.Errorf("FinderInfo round-trip mismatch")
	}
	if !out.HasComment || !bytes.Equal(out.Comment, []byte("hi")) {
		t.Errorf("comment = %q, want hi", out.Comment)
	}

	rsrc := []byte("netatalk-resource")
	if err := a.Setxattr("/doc", xattrUserPrefix+xattrNetatalkResourceFork, rsrc, 0); err != nil {
		t.Fatalf("Setxattr ResourceFork: %v", err)
	}
	got, err = a.Getxattr("/doc", xattrNetatalkResourceFork)
	if err != nil {
		t.Fatalf("Getxattr ResourceFork: %v", err)
	}
	if !bytes.Equal(got, rsrc) {
		t.Errorf("ResourceFork = %q, want %q", got, rsrc)
	}

	names, err := a.Listxattr("/doc")
	if err != nil {
		t.Fatalf("Listxattr: %v", err)
	}
	wantNames := map[string]bool{
		xattrUserPrefix + xattrNetatalkMetadata:     false,
		xattrUserPrefix + xattrNetatalkResourceFork: false,
	}
	for _, n := range names {
		if _, ok := wantNames[n]; ok {
			wantNames[n] = true
		}
	}
	for n, seen := range wantNames {
		if !seen {
			t.Errorf("Listxattr missing %s (got %v)", n, names)
		}
	}

	// ResourceFork write must refresh Metadata's recorded length.
	got, err = a.Getxattr("/doc", xattrNetatalkMetadata)
	if err != nil {
		t.Fatalf("Getxattr Metadata after rsrc: %v", err)
	}
	_, rsrcLen, err := fs.ParseNetatalkMetadataEA(got)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if rsrcLen != uint32(len(rsrc)) {
		t.Errorf("Metadata recorded rsrcLen=%d, want %d", rsrcLen, len(rsrc))
	}
}
