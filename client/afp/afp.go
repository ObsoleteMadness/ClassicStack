// Package afp is the AFP client's fs.FileSystem + native fs.ForkEngine adapter: it maps
// the core/fs operations onto AFP commands over an ASP session (client/asp) — Enumerate
// → ReadDir, GetFileDirParms → Stat, OpenFork/Read/Write → the File I/O, and
// Get/SetFileDirParms → Finder info (type/creator). Because it implements fs.ForkEngine
// natively, client.Connect selects the "passthrough" fork backend so a remote AFP
// volume's resource forks come straight off the wire (OpenFork(Resource)), not from
// synthesised AppleDouble sidecars.
//
// Paths are '/'-separated and volume-root-relative; they are translated to the AFP wire
// form (a leading NUL then NUL-joined elements, one path-type charset) and resolved by
// the server against the volume root CNID.
//
// Ring: CLIENT.
package afp

import (
	stdfs "io/fs"
	"strings"
	"sync"
	"time"

	aspclient "github.com/ObsoleteMadness/ClassicStack/client/asp"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	proto "github.com/ObsoleteMadness/ClassicStack/core/protocol/afp"
)

// pathType is the AFP path-type this client uses. Long names (MacRoman, 31 bytes) is the
// classic-server baseline; UTF-8 is an AFP-3 refinement not needed for a 2.x server.
const pathType = proto.PathTypeLongNames

// FS is an AFP client bound to one open volume. It satisfies fs.FileSystem and
// fs.ForkEngine (the "passthrough" fork backend forwards to it).
type FS struct {
	sess  *aspclient.Session
	volID uint16
	name  string

	// onClose, if set, runs after the session is closed (the factory sets it to close
	// the owning DDP endpoint, so FS.Close tears the whole transport down).
	onClose func()

	mu       sync.Mutex
	readOnly bool
}

// Open logs into the server over sess and opens the named volume, returning the FS. The
// caller has already run FPLogin via Login; Open runs FPOpenVol.
func Open(sess *aspclient.Session, volume string) (*FS, error) {
	req := proto.OpenVolRequest{
		Bitmap:  proto.VolBitmapID | proto.VolBitmapSignature | proto.VolBitmapAttributes,
		VolName: volume,
	}
	body, result, err := sess.Command(req.Marshal())
	if err != nil {
		return nil, err
	}
	if result != proto.NoErr {
		return nil, afpError("FPOpenVol", result)
	}
	vp, ok := proto.ParseVolParams(body)
	if !ok {
		return nil, errMalformed("FPOpenVol reply")
	}
	return &FS{sess: sess, volID: vp.VolID, name: volume}, nil
}

// afpWirePath translates a '/'-separated, volume-root-relative store path to the AFP
// wire pathname: a leading NUL, then the elements joined by NUL, each in the path-type
// charset (MacRoman here; ASCII passes through unchanged). An empty path names the
// volume root and is sent as a single NUL (the "this directory" form the server accepts).
func afpWirePath(p string) []byte {
	p = strings.Trim(p, "/")
	if p == "" {
		return []byte{0x00}
	}
	elems := strings.Split(p, "/")
	out := []byte{0x00}
	for i, e := range elems {
		if i > 0 {
			out = append(out, 0x00)
		}
		out = append(out, []byte(e)...)
	}
	return out
}

// splitPath splits a '/'-separated path into its parent and final element.
func splitPath(p string) (dir, base string) {
	p = strings.Trim(p, "/")
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[:i], p[i+1:]
	}
	return "", p
}

// command runs an AFP command block and returns the reply body, mapping a non-zero
// result to an error.
func (f *FS) command(name string, block []byte) ([]byte, error) {
	body, result, err := f.sess.Command(block)
	if err != nil {
		return nil, err
	}
	if result != proto.NoErr {
		return nil, afpError(name, result)
	}
	return body, nil
}

// fileInfo is the fs.FileInfo the adapter returns from parsed AFP params.
type fileInfo struct {
	name       string
	size       int64
	dir        bool
	modTime    time.Time
	createTime time.Time
	afpAttrs   uint16 // FPGetFileDirParms Attributes word (AFP AttrInvisible/System/…)
}

func (fi fileInfo) Name() string { return fi.name }
func (fi fileInfo) Size() int64  { return fi.size }
func (fi fileInfo) Mode() stdfs.FileMode {
	if fi.dir {
		return stdfs.ModeDir | 0o755
	}
	return 0o644
}
func (fi fileInfo) ModTime() time.Time { return fi.modTime }
func (fi fileInfo) IsDir() bool        { return fi.dir }

// Sys exposes the file's DOS-equivalent attributes and creation time to a DOS/Windows
// consumer (the WinFsp mount, via the share's fs-native MetaEngine), mapping the AFP
// attribute bits — Invisible→Hidden, System→System, WriteInhibit→ReadOnly. Returns nil
// when nothing maps, so a plain file is not reported as having attributes.
func (fi fileInfo) Sys() any {
	dos := afpAttrsToDOS(fi.afpAttrs)
	if dos == 0 && fi.createTime.IsZero() {
		return nil
	}
	return afpMeta{dos: dos, create: fi.createTime}
}

// afpMeta adapts AFP-derived metadata to the fs interfaces the fs-native MetaEngine reads
// (fs.DOSAttrInfo for the attribute bits, fs.DOSCreateTimeInfo for the creation date).
type afpMeta struct {
	dos    uint16
	create time.Time
}

func (m afpMeta) DOSAttrs() uint16         { return m.dos }
func (m afpMeta) DOSCreateTime() time.Time { return m.create }

// afpAttrsToDOS maps the AFP file/dir Attributes word to the DOS attribute bits.
func afpAttrsToDOS(a uint16) uint16 {
	var d uint16
	if a&proto.AttrInvisible != 0 {
		d |= fs.DOSHidden
	}
	if a&proto.AttrSystem != 0 {
		d |= fs.DOSSystem
	}
	if a&proto.AttrWriteInhibit != 0 {
		d |= fs.DOSReadOnly
	}
	return d
}

// dirEntry is the fs.DirEntry the adapter returns from Enumerate.
type dirEntry struct {
	name     string
	dir      bool
	size     int64
	mod      time.Time
	create   time.Time
	afpAttrs uint16
}

func (d dirEntry) Name() string { return d.name }
func (d dirEntry) IsDir() bool  { return d.dir }
func (d dirEntry) Type() stdfs.FileMode {
	if d.dir {
		return stdfs.ModeDir
	}
	return 0
}
func (d dirEntry) Info() (stdfs.FileInfo, error) {
	return fileInfo{name: d.name, size: d.size, dir: d.dir, modTime: d.mod, createTime: d.create, afpAttrs: d.afpAttrs}, nil
}
