package finder

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

func TestCatalogMkdirListRename(t *testing.T) {
	ffs, err := fs.BuildShare(fs.ShareSpec{Name: "Mem", FSType: "memfs"}, nil)
	if err != nil {
		t.Fatalf("BuildShare: %v", err)
	}
	svc := New(nil, nil)
	root := ffs.Meta().EnsureCNID("")
	svc.put(&Session{
		ID:      "t",
		Kind:    "local",
		Volume:  "Mem",
		FS:      ffs,
		local:   true,
		touched: time.Now(),
	})

	dir, err := svc.Mkdir("t", CNIDRef(root), "Folder")
	if err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	if !dir.IsDir || dir.Name != "Folder" {
		t.Fatalf("mkdir node = %+v", dir)
	}

	file, err := svc.CreateFile("t", CNIDRef(dir.ID), "hello.txt", []byte("hi"), nil, nil)
	if err != nil {
		t.Fatalf("CreateFile: %v", err)
	}
	if file.IsDir || file.DataBytes != 2 {
		t.Fatalf("file node = %+v", file)
	}

	kids, err := svc.Children("t", CNIDRef(dir.ID))
	if err != nil {
		t.Fatalf("Children: %v", err)
	}
	if len(kids) != 1 || kids[0].Name != "hello.txt" {
		t.Fatalf("children = %+v", kids)
	}

	got, err := svc.Lookup("t", CNIDRef(dir.ID), "hello.txt")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if got.ID != file.ID {
		t.Fatalf("lookup id %d want %d", got.ID, file.ID)
	}

	data, err := svc.ReadFork("t", CNIDRef(file.ID), false, 0, 0)
	if err != nil {
		t.Fatalf("ReadFork: %v", err)
	}
	if string(data) != "hi" {
		t.Fatalf("data %q", data)
	}

	if err := svc.Rename("t", CNIDRef(file.ID), "bye.txt"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if _, err := svc.Lookup("t", CNIDRef(dir.ID), "bye.txt"); err != nil {
		t.Fatalf("lookup after rename: %v", err)
	}

	n, err := svc.GetNode("t", CNIDRef(dir.ID))
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if n.Name != "Folder" {
		t.Fatalf("dir name %q", n.Name)
	}

	if err := svc.Remove("t", CNIDRef(file.ID)); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	kids, err = svc.Children("t", CNIDRef(dir.ID))
	if err != nil {
		t.Fatalf("Children after remove: %v", err)
	}
	if len(kids) != 0 {
		t.Fatalf("expected empty, got %+v", kids)
	}
}

type forkIOCounter struct {
	fs.ForkFS
	opens  int
	closes int
}

type countingForkFile struct {
	fs.File
	onClose func()
}

func (f countingForkFile) Close() error {
	f.onClose()
	return f.File.Close()
}

func (c *forkIOCounter) OpenFork(path string, fork fs.ForkType, flag int) (fs.File, error) {
	c.opens++
	f, err := c.ForkFS.OpenFork(path, fork, flag)
	if err != nil {
		return nil, err
	}
	return countingForkFile{File: f, onClose: func() { c.closes++ }}, nil
}

func TestReadForkOpensAndCloses(t *testing.T) {
	base, err := fs.BuildShare(fs.ShareSpec{Name: "Mem", FSType: "memfs"}, nil)
	if err != nil {
		t.Fatalf("BuildShare: %v", err)
	}
	wf, err := base.CreateFile("hello.txt")
	if err != nil {
		t.Fatalf("CreateFile: %v", err)
	}
	if _, err := wf.WriteAt([]byte("abcdef"), 0); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}
	_ = wf.Close()

	counted := &forkIOCounter{ForkFS: base}
	svc := New(nil, nil)
	root := counted.Meta().EnsureCNID("")
	svc.put(&Session{ID: "t", Kind: "local", Volume: "Mem", FS: counted, local: true, touched: time.Now()})
	file, err := svc.Lookup("t", CNIDRef(root), "hello.txt")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}

	got, err := svc.ReadFork("t", CNIDRef(file.ID), false, 1, 3)
	if err != nil {
		t.Fatalf("ReadFork range: %v", err)
	}
	if string(got) != "bcd" {
		t.Fatalf("range = %q, want bcd", got)
	}
	if counted.opens != 1 || counted.closes != 1 {
		t.Fatalf("range open/close = %d/%d, want 1/1", counted.opens, counted.closes)
	}

	all, err := svc.ReadFork("t", CNIDRef(file.ID), false, 0, 0)
	if err != nil {
		t.Fatalf("ReadFork all: %v", err)
	}
	if string(all) != "abcdef" {
		t.Fatalf("all = %q, want abcdef", all)
	}
	if counted.opens != 2 || counted.closes != 2 {
		t.Fatalf("all open/close = %d/%d, want 2/2", counted.opens, counted.closes)
	}
}

type readDirCounter struct {
	fs.ForkFS
	n int
}

func (c *readDirCounter) ReadDir(path string) ([]os.DirEntry, error) {
	c.n++
	return c.ForkFS.ReadDir(path)
}

func TestLookupDoesNotEnumerateDirectory(t *testing.T) {
	base, err := fs.BuildShare(fs.ShareSpec{Name: "Mem", FSType: "memfs"}, nil)
	if err != nil {
		t.Fatalf("BuildShare: %v", err)
	}
	if err := base.CreateDir("Folder"); err != nil {
		t.Fatalf("CreateDir: %v", err)
	}
	for _, name := range []string{"a", "b", "c", "d", "e"} {
		f, err := base.CreateFile("Folder/" + name)
		if err != nil {
			t.Fatalf("CreateFile %s: %v", name, err)
		}
		_ = f.Close()
	}
	counted := &readDirCounter{ForkFS: base}
	svc := New(nil, nil)
	root := counted.Meta().EnsureCNID("")
	svc.put(&Session{ID: "t", Kind: "local", Volume: "Mem", FS: counted, local: true, touched: time.Now()})

	folder, err := svc.Lookup("t", CNIDRef(root), "Folder")
	if err != nil {
		t.Fatalf("Lookup Folder: %v", err)
	}
	if counted.n != 0 {
		t.Fatalf("Lookup(Folder) ReadDir count = %d, want 0", counted.n)
	}

	got, err := svc.Lookup("t", CNIDRef(folder.ID), "c")
	if err != nil {
		t.Fatalf("Lookup c: %v", err)
	}
	if got.Name != "c" || got.IsDir {
		t.Fatalf("lookup c = %+v", got)
	}
	if counted.n != 0 {
		t.Fatalf("Lookup(c) ReadDir count = %d, want 0 (must Stat, not enumerate)", counted.n)
	}

	if _, err := svc.Lookup("t", CNIDRef(folder.ID), "Icon\r"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Lookup Icon\\r err = %v, want ErrNotFound", err)
	}
	if counted.n != 0 {
		t.Fatalf("Lookup(missing) ReadDir count = %d, want 0", counted.n)
	}

	kids, err := svc.Children("t", CNIDRef(folder.ID))
	if err != nil {
		t.Fatalf("Children: %v", err)
	}
	if len(kids) != 5 {
		t.Fatalf("Children count = %d, want 5", len(kids))
	}
	if counted.n != 1 {
		t.Fatalf("Children ReadDir count = %d, want 1", counted.n)
	}
}

func TestLocalIDParse(t *testing.T) {
	id := localID("afp", "Mac HD")
	proto, name, ok := parseLocalID(id)
	if !ok || proto != "afp" || name != "Mac HD" {
		t.Fatalf("parse %q → %q %q %v", id, proto, name, ok)
	}
}

func TestChildrenHidesAppleDoubleSidecars(t *testing.T) {
	ffs, err := fs.BuildShare(fs.ShareSpec{Name: "Mem", FSType: "memfs"}, nil)
	if err != nil {
		t.Fatalf("BuildShare: %v", err)
	}
	if _, err := ffs.CreateFile("doc"); err != nil {
		t.Fatalf("CreateFile doc: %v", err)
	}
	if _, err := ffs.OpenFile("._doc", os.O_CREATE|os.O_RDWR); err != nil {
		t.Fatalf("OpenFile sidecar: %v", err)
	}
	if err := ffs.CreateDir(".AppleDouble"); err != nil {
		t.Fatalf("CreateDir: %v", err)
	}

	svc := New(nil, nil)
	root := ffs.Meta().EnsureCNID("")
	svc.put(&Session{ID: "t", Kind: "local", Volume: "Mem", FS: ffs, local: true, touched: time.Now()})
	kids, err := svc.Children("t", CNIDRef(root))
	if err != nil {
		t.Fatalf("Children: %v", err)
	}
	if len(kids) != 1 || kids[0].Name != "doc" {
		t.Fatalf("children = %+v, want [doc] (sidecars hidden)", kids)
	}
}

func TestChildrenShowsSidecarsOnNoFork(t *testing.T) {
	ffs, err := fs.BuildShare(fs.ShareSpec{Name: "Mem", FSType: "memfs", ForkBackend: "nofork"}, nil)
	if err != nil {
		t.Fatalf("BuildShare: %v", err)
	}
	if _, err := ffs.CreateFile("doc"); err != nil {
		t.Fatalf("CreateFile doc: %v", err)
	}
	if _, err := ffs.OpenFile("._doc", os.O_CREATE|os.O_RDWR); err != nil {
		t.Fatalf("OpenFile sidecar: %v", err)
	}

	svc := New(nil, nil)
	root := ffs.Meta().EnsureCNID("")
	svc.put(&Session{ID: "t", Kind: "local", Volume: "Mem", FS: ffs, local: true, touched: time.Now()})
	kids, err := svc.Children("t", CNIDRef(root))
	if err != nil {
		t.Fatalf("Children: %v", err)
	}
	got := map[string]bool{}
	for _, k := range kids {
		got[k.Name] = true
	}
	if !got["doc"] || !got["._doc"] {
		t.Fatalf("nofork children = %+v, want doc and ._doc", kids)
	}
}

func TestOpenLocalReusesSession(t *testing.T) {
	ffs, err := fs.BuildShare(fs.ShareSpec{Name: "Mem", FSType: "memfs"}, nil)
	if err != nil {
		t.Fatalf("BuildShare: %v", err)
	}
	svc := New(nil, nil)
	svc.put(&Session{
		ID:        "s1",
		Kind:      "local",
		Volume:    "Mem",
		FS:        ffs,
		local:     true,
		remoteURI: "local:afp:Mem",
		touched:   time.Now(),
	})
	info, err := svc.OpenLocal("local:afp:Mem")
	if err != nil {
		t.Fatalf("OpenLocal: %v", err)
	}
	if info.SessionID != "s1" {
		t.Fatalf("session %q, want reused s1", info.SessionID)
	}
}

func TestPathVolumeNoCNIDIdentity(t *testing.T) {
	ffs, err := fs.BuildShare(fs.ShareSpec{Name: "Mem", FSType: "memfs", ForkBackend: "nofork"}, nil)
	if err != nil {
		t.Fatalf("BuildShare: %v", err)
	}
	svc := New(nil, nil)
	svc.put(&Session{
		ID:       "smb",
		Kind:     KindSMB,
		Protocol: KindSMB,
		Volume:   "Mem",
		FS:       ffs,
		touched:  time.Now(),
	})
	s, err := svc.get("smb")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	caps := s.capabilities()
	if caps.AddressBy != AddressPath {
		t.Fatalf("addressBy %q, want path", caps.AddressBy)
	}
	if caps.ResourceFork {
		t.Fatalf("nofork should not advertise resourceFork")
	}

	dir, err := svc.Mkdir("smb", PathRef(""), "FOO")
	if err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	if dir.Addr != AddressPath || dir.Path != "FOO" || dir.ParentPath != "" || dir.ID != 0 {
		t.Fatalf("path dir = %+v", dir)
	}
	file, err := svc.CreateFile("smb", PathRef("FOO"), "BAR.TXT", nil, nil, nil)
	if err != nil {
		t.Fatalf("CreateFile: %v", err)
	}
	if file.Path != "FOO/BAR.TXT" || file.ParentPath != "FOO" || file.ID != 0 {
		t.Fatalf("path file = %+v", file)
	}
	kids, err := svc.Children("smb", PathRef("FOO"))
	if err != nil {
		t.Fatalf("Children: %v", err)
	}
	if len(kids) != 1 || kids[0].Path != "FOO/BAR.TXT" {
		t.Fatalf("children = %+v", kids)
	}
	got, err := svc.ResolvePath("smb", "FOO/BAR.TXT")
	if err != nil {
		t.Fatalf("ResolvePath: %v", err)
	}
	if got.Path != file.Path {
		t.Fatalf("resolve %q want %q", got.Path, file.Path)
	}
	p, err := svc.PathOf("smb", PathRef("FOO/BAR.TXT"))
	if err != nil || p != "FOO/BAR.TXT" {
		t.Fatalf("PathOf = %q %v", p, err)
	}
	if _, err := svc.GetNode("smb", CNIDRef(2)); !errors.Is(err, ErrBadRef) {
		t.Fatalf("CNID on path volume err = %v, want ErrBadRef", err)
	}
}

func TestCNIDVolumeResolvePath(t *testing.T) {
	ffs, err := fs.BuildShare(fs.ShareSpec{Name: "Mem", FSType: "memfs"}, nil)
	if err != nil {
		t.Fatalf("BuildShare: %v", err)
	}
	svc := New(nil, nil)
	svc.put(&Session{ID: "t", Kind: KindLocal, Protocol: KindAFP, Volume: "Mem", FS: ffs, local: true, touched: time.Now()})
	root := ffs.Meta().EnsureCNID("")
	if _, err := svc.Mkdir("t", CNIDRef(root), "FOO"); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	file, err := svc.CreateFile("t", CNIDRef(ffs.Meta().EnsureCNID("FOO")), "BAR", []byte("x"), nil, nil)
	if err != nil {
		t.Fatalf("CreateFile: %v", err)
	}
	got, err := svc.ResolvePath("t", "FOO/BAR")
	if err != nil {
		t.Fatalf("ResolvePath: %v", err)
	}
	if got.Addr != AddressCNID || got.ID != file.ID || got.Path != "" {
		t.Fatalf("resolved %+v, want CNID %d without path", got, file.ID)
	}
	p, err := svc.PathOf("t", CNIDRef(file.ID))
	if err != nil || p != "FOO/BAR" {
		t.Fatalf("PathOf = %q %v", p, err)
	}
}

func TestWriteAttrsDOS(t *testing.T) {
	ffs, err := fs.BuildShare(fs.ShareSpec{Name: "Mem", FSType: "memfs", ForkBackend: "nofork"}, nil)
	if err != nil {
		t.Fatalf("BuildShare: %v", err)
	}
	svc := New(nil, nil)
	svc.put(&Session{ID: "smb", Kind: KindSMB, Protocol: KindSMB, Volume: "Mem", FS: ffs, touched: time.Now()})
	file, err := svc.CreateFile("smb", PathRef(""), "HIDDEN.TXT", nil, nil, nil)
	if err != nil {
		t.Fatalf("CreateFile: %v", err)
	}
	if err := svc.WriteAttrs("smb", PathRef(file.Path), map[string]bool{"hidden": true, "readonly": true}); err != nil {
		t.Fatalf("WriteAttrs: %v", err)
	}
	got, err := svc.GetNode("smb", PathRef(file.Path))
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if !got.Attrs["hidden"] || !got.Attrs["readonly"] {
		t.Fatalf("attrs = %+v", got.Attrs)
	}
}

func TestAppleDoubleDoesNotAdvertiseMacMetaOnSMB(t *testing.T) {
	ffs, err := fs.BuildShare(fs.ShareSpec{Name: "Mem", FSType: "memfs", ForkBackend: "appledouble"}, nil)
	if err != nil {
		t.Fatalf("BuildShare: %v", err)
	}
	sess := &Session{ID: "smb", Kind: KindSMB, Protocol: KindSMB, Volume: "Mem", FS: ffs}
	caps := sess.capabilities()
	if caps.ResourceFork || caps.FinderInfo || caps.ResourceIcons {
		t.Fatalf("SMB appledouble advertised Mac metadata: %+v", caps)
	}
}

func TestLocalSMBAppleDoubleDoesNotAdvertiseMacMeta(t *testing.T) {
	ffs, err := fs.BuildShare(fs.ShareSpec{Name: "Mem", FSType: "memfs", ForkBackend: "appledouble"}, nil)
	if err != nil {
		t.Fatalf("BuildShare: %v", err)
	}
	sess := &Session{
		ID:       "local-smb",
		Kind:     KindLocal,
		Protocol: KindSMB,
		Volume:   "Mem",
		FS:       ffs,
		local:    true,
	}
	caps := sess.capabilities()
	if caps.ResourceFork || caps.FinderInfo || caps.ResourceIcons {
		t.Fatalf("local SMB advertised Mac metadata: %+v", caps)
	}
	if caps.AddressBy != AddressPath {
		t.Fatalf("addressBy %q, want path", caps.AddressBy)
	}
}

func TestAFPAppleDoubleAdvertisesMacMeta(t *testing.T) {
	ffs, err := fs.BuildShare(fs.ShareSpec{Name: "Mem", FSType: "memfs", ForkBackend: "appledouble"}, nil)
	if err != nil {
		t.Fatalf("BuildShare: %v", err)
	}
	sess := &Session{
		ID:       "local-afp",
		Kind:     KindLocal,
		Protocol: KindAFP,
		Volume:   "Mem",
		FS:       ffs,
		local:    true,
	}
	caps := sess.capabilities()
	if !caps.ResourceFork || !caps.FinderInfo {
		t.Fatalf("AFP appledouble missing Mac metadata: %+v", caps)
	}
}

func TestAFPNoForkDoesNotAdvertiseMacMeta(t *testing.T) {
	ffs, err := fs.BuildShare(fs.ShareSpec{Name: "Mem", FSType: "memfs", ForkBackend: "nofork"}, nil)
	if err != nil {
		t.Fatalf("BuildShare: %v", err)
	}
	sess := &Session{
		ID:       "local-afp",
		Kind:     KindLocal,
		Protocol: KindAFP,
		Volume:   "Mem",
		FS:       ffs,
		local:    true,
	}
	caps := sess.capabilities()
	if caps.ResourceFork || caps.FinderInfo || caps.ResourceIcons {
		t.Fatalf("AFP nofork advertised Mac metadata: %+v", caps)
	}
}
