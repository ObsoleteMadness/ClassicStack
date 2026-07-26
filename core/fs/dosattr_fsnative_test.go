package fs

import (
	stdfs "io/fs"
	"testing"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/metastore"
)

// attrFileInfo is a minimal fs.FileInfo whose Sys() carries DOS attributes via the
// fs.DOSAttrInfo interface — modelling a remote client FS (SMB/AFP) that reads the
// server's FileAttributes off the wire.
type attrFileInfo struct {
	name  string
	dir   bool
	attrs uint16
}

func (fi attrFileInfo) Name() string { return fi.name }
func (fi attrFileInfo) Size() int64  { return 0 }
func (fi attrFileInfo) Mode() stdfs.FileMode {
	if fi.dir {
		return stdfs.ModeDir | 0o755
	}
	return 0o644
}
func (fi attrFileInfo) ModTime() time.Time { return time.Time{} }
func (fi attrFileInfo) IsDir() bool        { return fi.dir }
func (fi attrFileInfo) Sys() any {
	if fi.attrs == 0 {
		return nil
	}
	return dosAttrsValue(fi.attrs)
}

type dosAttrsValue uint16

func (v dosAttrsValue) DOSAttrs() uint16 { return uint16(v) }

// attrFS is a FileSystem that returns per-path DOS attributes from Stat and advertises
// DirAttributes, so buildDOSAttrStore selects the fs-native backend.
type attrFS struct {
	FileSystem // embed for the methods this test does not exercise (nil is fine unused)
	attrs      map[string]uint16
}

func (f *attrFS) Stat(path string) (stdfs.FileInfo, error) {
	a, ok := f.attrs[path]
	if !ok {
		return nil, stdfs.ErrNotExist
	}
	return attrFileInfo{name: path, attrs: a}, nil
}

func (f *attrFS) Capabilities() Capabilities { return Capabilities{DirAttributes: true} }

// TestFSNativeDOSAttrStore checks buildDOSAttrStore selects the fs-native backend for a
// FileSystem advertising DirAttributes, and that Get reads the wire attributes while a
// session-local Set is cached and wins.
func TestFSNativeDOSAttrStore(t *testing.T) {
	base := &attrFS{attrs: map[string]uint16{
		"MSDOS.SYS": DOSReadOnly | DOSHidden | DOSSystem | DOSArchive, // 0x27
		"plain.txt": DOSArchive,                                       // 0x20 storable → archive
		"nofile":    0,
	}}
	store, _ := metastore.NewMem("")
	s := buildDOSAttrStore(dosBackendAuto, base, store, nil)

	if _, ok := s.(*fsNativeDOSAttrStore); !ok {
		t.Fatalf("DirAttributes FS should select fs-native store, got %T", s)
	}

	// Hidden/system/read-only surface from the wire.
	got, ok := s.Get("MSDOS.SYS")
	if !ok {
		t.Fatal("MSDOS.SYS: expected attributes from the wire")
	}
	if got.Attrs != (DOSReadOnly | DOSHidden | DOSSystem | DOSArchive) {
		t.Errorf("MSDOS.SYS attrs = %#x, want 0x27", got.Attrs)
	}

	// Archive-only still reports (it is in DOSStorableMask).
	if got, ok := s.Get("plain.txt"); !ok || got.Attrs != DOSArchive {
		t.Errorf("plain.txt: got (%#x, %v), want (0x20, true)", got.Attrs, ok)
	}

	// A missing file reports "nothing stored".
	if _, ok := s.Get("nofile"); ok {
		t.Error("nofile: expected no stored attributes")
	}

	// A session-local Set is cached and wins over the wire read.
	if err := s.Set("MSDOS.SYS", DOSAttr{Attrs: DOSHidden}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if got, _ := s.Get("MSDOS.SYS"); got.Attrs != DOSHidden {
		t.Errorf("after Set, attrs = %#x, want 0x02 (cached value wins)", got.Attrs)
	}
}
