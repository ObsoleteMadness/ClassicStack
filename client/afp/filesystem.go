package afp

import (
	stdfs "io/fs"
	"os"
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	proto "github.com/ObsoleteMadness/ClassicStack/core/protocol/afp"
)

// filesystem.go implements fs.FileSystem over AFP. Enumerate/GetFileDirParms carry a
// file+dir bitmap requesting the fields the fs layer needs: long name, data-fork length,
// mod date, and Finder info (for type/creator via the ForkEngine).

// statBitmap is the file/dir parameter set the adapter requests for a Stat/Enumerate.
const (
	fileStatBitmap = proto.FDBitmapAttributes | proto.FDBitmapLongName |
		proto.FDBitmapCreateDate | proto.FDBitmapModDate |
		proto.FDBitmapFinderInfo | proto.FileBitmapDataForkLen
	dirStatBitmap = proto.FDBitmapAttributes | proto.FDBitmapLongName |
		proto.FDBitmapCreateDate | proto.FDBitmapModDate |
		proto.FDBitmapFinderInfo
)

var _ fs.FileSystem = (*FS)(nil)

// ReadDir lists a directory via FPEnumerate, paging until the server reports no more
// entries (kFPObjectNotFound at the next start index).
func (f *FS) ReadDir(path string) ([]stdfs.DirEntry, error) {
	var out []stdfs.DirEntry
	start := uint16(1)
	for {
		req := proto.EnumerateRequest{
			VolID:        f.volID,
			DirID:        proto.CNIDRoot,
			FileBitmap:   fileStatBitmap,
			DirBitmap:    dirStatBitmap,
			ReqCount:     50,
			StartIndex:   start,
			MaxReplySize: 4000,
			PathType:     pathType,
			Path:         afpWirePath(path),
		}
		body, result, err := f.sess.Command(req.Marshal())
		if err != nil {
			return nil, err
		}
		if result == proto.ErrObjectNotFnd {
			break // no more entries
		}
		if result != proto.NoErr {
			return nil, afpError("FPEnumerate", result)
		}
		reply, ok := proto.ParseEnumerateReply(body)
		if !ok {
			return nil, errMalformed("FPEnumerate reply")
		}
		if len(reply.Entries) == 0 {
			break
		}
		for _, e := range reply.Entries {
			out = append(out, dirEntry{
				name:     string(e.LongName),
				dir:      e.IsDir,
				size:     int64(e.DataForkLen),
				mod:      e.ModDate,
				create:   e.CreateDate,
				afpAttrs: e.Attributes,
			})
		}
		start += uint16(len(reply.Entries))
	}
	return out, nil
}

// Stat resolves one path via FPGetFileDirParms.
func (f *FS) Stat(path string) (stdfs.FileInfo, error) {
	p, err := f.getFileDirParms(path)
	if err != nil {
		return nil, err
	}
	_, base := splitPath(path)
	name := base
	if len(p.Params.LongName) > 0 {
		name = string(p.Params.LongName)
	}
	return fileInfo{
		name:       name,
		size:       int64(p.Params.DataForkLen),
		dir:        p.IsDir,
		modTime:    p.Params.ModDate,
		createTime: p.Params.CreateDate,
		afpAttrs:   p.Params.Attributes,
	}, nil
}

// getFileDirParms runs FPGetFileDirParms for a path and returns the parsed reply,
// mapping object-not-found to fs.ErrNotExist.
func (f *FS) getFileDirParms(path string) (proto.GetFileDirParmsReply, error) {
	req := proto.GetFileDirParmsRequest{
		VolID:      f.volID,
		DirID:      proto.CNIDRoot,
		FileBitmap: fileStatBitmap,
		DirBitmap:  dirStatBitmap,
		PathType:   pathType,
		Path:       afpWirePath(path),
	}
	body, result, err := f.sess.Command(req.Marshal())
	if err != nil {
		return proto.GetFileDirParmsReply{}, err
	}
	if result == proto.ErrObjectNotFnd || result == proto.ErrDirNotFound {
		return proto.GetFileDirParmsReply{}, stdfs.ErrNotExist
	}
	if result != proto.NoErr {
		return proto.GetFileDirParmsReply{}, afpError("FPGetFileDirParms", result)
	}
	reply, ok := proto.ParseGetFileDirParmsReply(body)
	if !ok {
		return proto.GetFileDirParmsReply{}, errMalformed("FPGetFileDirParms reply")
	}
	return reply, nil
}

// DiskUsage reports the volume's total/free bytes via FPGetVolParms.
func (f *FS) DiskUsage(path string) (total, free uint64, err error) {
	req := proto.GetVolParmsRequest{
		VolID:  f.volID,
		Bitmap: proto.VolBitmapBytesFree | proto.VolBitmapBytesTotal,
	}
	body, e := f.command("FPGetVolParms", req.Marshal())
	if e != nil {
		return 0, 0, e
	}
	vp, ok := proto.ParseVolParams(body)
	if !ok {
		return 0, 0, errMalformed("FPGetVolParms reply")
	}
	return uint64(vp.BytesTotal), uint64(vp.BytesFree), nil
}

// CreateDir creates a directory via FPCreateDir.
func (f *FS) CreateDir(path string) error {
	req := proto.CreateDirRequest{
		VolID:    f.volID,
		DirID:    proto.CNIDRoot,
		PathType: pathType,
		Path:     afpWirePath(path),
	}
	_, err := f.command("FPCreateDir", req.Marshal())
	return err
}

// CreateFile creates a file via FPCreateFile and returns an open handle to its data
// fork.
func (f *FS) CreateFile(path string) (fs.File, error) {
	req := proto.CreateFileRequest{
		VolID:    f.volID,
		DirID:    proto.CNIDRoot,
		PathType: pathType,
		Path:     afpWirePath(path),
	}
	if _, err := f.command("FPCreateFile", req.Marshal()); err != nil {
		return nil, err
	}
	return f.OpenFork(path, fs.DataFork, os.O_RDWR)
}

// OpenFile opens a file's data fork. O_CREATE creates it first.
func (f *FS) OpenFile(path string, flag int) (fs.File, error) {
	if flag&os.O_CREATE != 0 {
		// Create-if-missing: try create, ignore an "exists" error.
		req := proto.CreateFileRequest{VolID: f.volID, DirID: proto.CNIDRoot, PathType: pathType, Path: afpWirePath(path)}
		if _, err := f.command("FPCreateFile", req.Marshal()); err != nil && !strings.Contains(err.Error(), "kFPObjectExists") {
			return nil, err
		}
	}
	return f.OpenFork(path, fs.DataFork, flag)
}

// Remove deletes a file or (empty) directory via FPDelete.
func (f *FS) Remove(path string) error {
	req := proto.DeleteRequest{
		VolID:    f.volID,
		DirID:    proto.CNIDRoot,
		PathType: pathType,
		Path:     afpWirePath(path),
	}
	_, err := f.command("FPDelete", req.Marshal())
	return err
}

// Rename renames or moves a path. A rename within the same directory uses FPRename; a
// move to a different directory uses FPMoveAndRename.
func (f *FS) Rename(old, new string) error {
	oldDir, oldBase := splitPath(old)
	newDir, newBase := splitPath(new)
	if oldDir == newDir {
		req := proto.RenameRequest{
			VolID:    f.volID,
			DirID:    proto.CNIDRoot,
			PathType: pathType,
			OldName:  afpNamePath(oldDir, oldBase),
			NewName:  []byte(newBase),
		}
		_, err := f.command("FPRename", req.Marshal())
		return err
	}
	req := proto.MoveAndRenameRequest{
		VolID:    f.volID,
		SrcDirID: proto.CNIDRoot,
		DstDirID: proto.CNIDRoot,
		PathType: pathType,
		SrcPath:  afpWirePath(old),
		DstPath:  afpWirePath(newDir),
		NewName:  []byte(newBase),
	}
	_, err := f.command("FPMoveAndRename", req.Marshal())
	return err
}

// ShortName / MediumName return the path's final element; the AFP server derives the
// real short/medium names, but the client fs layer only needs a stable value for the
// local metadata bookkeeping (the shareFS MetaEngine overrides these anyway).
func (f *FS) ShortName(path string) (string, error) {
	_, base := splitPath(path)
	return base, nil
}

func (f *FS) MediumName(path string) (string, error) {
	_, base := splitPath(path)
	return base, nil
}

// Capabilities reports the AFP volume's capabilities to the fs layer.
func (f *FS) Capabilities() fs.Capabilities {
	f.mu.Lock()
	ro := f.readOnly
	f.mu.Unlock()
	// DirAttributes: our FPGetFileDirParms/FPEnumerate FileInfo carries the AFP attribute
	// word + create date natively (fs.DOSAttrInfo/DOSCreateTimeInfo on Sys()), so the
	// share's MetaEngine reads them from the wire — surfacing Invisible→hidden,
	// System→system, WriteInhibit→read-only and the creation date to the WinFsp mount.
	return fs.Capabilities{ReadOnly: ro, ChildCount: true, DirAttributes: true}
}

// Close ends the underlying ASP session (fs.FSCloser), so client.Connect's ForkFS.Close
// tears the whole AFP session down; onClose (set by the factory) then closes the DDP
// endpoint/transport.
func (f *FS) Close() error {
	err := f.sess.Close()
	if f.onClose != nil {
		f.onClose()
	}
	return err
}

// afpNamePath builds an AFP wire pathname for a single leaf name in dir (used by
// FPRename, which names the old object by path).
func afpNamePath(dir, base string) []byte {
	if dir == "" {
		return afpWirePath(base)
	}
	return afpWirePath(dir + "/" + base)
}
