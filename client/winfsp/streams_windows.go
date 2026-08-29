//go:build windows

package winfsp

import (
	"errors"
	"os"
	"strings"

	winfsp "github.com/winfsp/go-winfsp"

	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

// This file surfaces a file's resource fork and Apple metadata as NTFS named
// streams, following the stream names NT Services for Macintosh (SFM) defines
// (macfile.h). When native forks are enabled (csmount -fork native → Options.
// NativeForks), the mount presents, alongside the unnamed data stream:
//
//	:AFP_Resource   the resource fork          (ForkEngine.OpenFork(ResourceFork))
//	:AFP_AfpInfo    the 60-byte AfpInfo record (ForkEngine Read/WriteFinderInfo)
//	:Comments       the Finder comment         (ForkEngine Read/WriteComment)
//
// so Windows tools (and the SMB redirector) can read/write Mac forks through the
// same stream names a real SFM/AFP server exposes. When NativeForks is off the
// mount has no streams and any ':stream' path is rejected as before.
//
// SFM does not expose the AFP_DeskTop / AFP_IdIndex volume streams (they are
// server-internal), so we do not map them.

// SFM NTFS stream names (NT macfile.h AFP_*_STREAM, without the leading ':').
// NTFS stream names are case-insensitive, so lookupStream folds case.
const (
	streamNameResource = "AFP_Resource"
	streamNameAfpInfo  = "AFP_AfpInfo"
	streamNameComments = "Comments"
)

// streamKind identifies which fork/record a handle targets.
type streamKind uint8

const (
	streamData     streamKind = iota // "" — the unnamed data stream (the file itself)
	streamResource                   // :AFP_Resource
	streamAfpInfo                    // :AFP_AfpInfo
	streamComments                   // :Comments
)

// errNoSuchStream is mapped to STATUS_OBJECT_NAME_NOT_FOUND: a stream name that is
// not one of the SFM streams we surface.
var errNoSuchStream = errors.New("winfsp: no such stream")

// lookupStream maps an NTFS stream name (without ':') to a streamKind. An empty name
// is the data stream. An unknown name returns ok=false.
func lookupStream(name string) (streamKind, bool) {
	switch {
	case name == "":
		return streamData, true
	case strings.EqualFold(name, streamNameResource):
		return streamResource, true
	case strings.EqualFold(name, streamNameAfpInfo):
		return streamAfpInfo, true
	case strings.EqualFold(name, streamNameComments):
		return streamComments, true
	default:
		return streamData, false
	}
}

// streamName returns the canonical SFM name for a non-data stream kind.
func (k streamKind) streamName() string {
	switch k {
	case streamResource:
		return streamNameResource
	case streamAfpInfo:
		return streamNameAfpInfo
	case streamComments:
		return streamNameComments
	default:
		return ""
	}
}

// readRecordStream materialises the current bytes of a record stream (AfpInfo or
// Comments) from the ForkEngine, for a handle opened on that stream. The resource
// fork is NOT a record stream (it is a live fs.File) and must not be passed here.
func (a *Adapter) readRecordStream(storePath string, k streamKind) ([]byte, error) {
	switch k {
	case streamAfpInfo:
		finder, ok, err := a.fsys.ReadFinderInfo(storePath)
		if err != nil {
			return nil, err
		}
		if !ok {
			// No FinderInfo yet: an all-zero AfpInfo record (valid signature) so the
			// stream reads as an empty-but-present 60 bytes, matching SFM.
			return fs.AfpInfo{}.Marshal(), nil
		}
		return fs.AfpInfo{FinderInfo: finder}.Marshal(), nil
	case streamComments:
		c, ok := a.fsys.ReadComment(storePath)
		if !ok {
			return nil, nil
		}
		return append([]byte(nil), c...), nil
	default:
		return nil, errNoSuchStream
	}
}

// flushRecordStream writes a record stream's buffer back through the ForkEngine. For
// AfpInfo, only the FinderInfo slice of the record is persisted (the ForkEngine seam
// exposes FinderInfo, not the whole SFM record); BackupTime/ProDOSInfo written by a
// Windows tool are decoded but not stored, matching what the AFP wire path keeps.
func (a *Adapter) flushRecordStream(storePath string, k streamKind, buf []byte) error {
	switch k {
	case streamAfpInfo:
		rec, err := fs.UnmarshalAfpInfo(buf)
		if err != nil {
			// A short/garbage record is tolerated as "no FinderInfo" (SFM behaviour);
			// nothing to persist.
			return nil
		}
		return a.fsys.WriteFinderInfo(storePath, rec.FinderInfo)
	case streamComments:
		return a.fsys.WriteComment(storePath, buf)
	default:
		return errNoSuchStream
	}
}

// streamAdapter is the Adapter presented to WinFsp when native forks are enabled. It
// adds only GetStreamInfo — go-winfsp's Mount sets FspFSAttributeNamedStreams (and wires
// the GetStreamInfo op) exactly when the mounted filesystem implements BehaviourGetStreamInfo,
// so a mount whose forks are OFF must present the bare *Adapter, which has no such method
// and therefore advertises no streams. Every other delegate is inherited from *Adapter.
type streamAdapter struct{ *Adapter }

// GetStreamInfo lists the NTFS streams present on an open file: the unnamed data stream
// plus the SFM resource-fork / AfpInfo / Comments streams that currently carry content.
func (s streamAdapter) GetStreamInfo(
	_ *winfsp.FileSystemRef, file uintptr,
	fill func(name string, streamSize, streamAllocationSize uint64) (bool, error),
) error {
	a := s.Adapter
	h, ok := a.handles.get(file)
	if !ok {
		return os.ErrInvalid
	}
	// Directories carry no forks; report no streams.
	if h.isDir {
		return nil
	}
	fi, err := a.fsys.Stat(h.path)
	if err != nil {
		return err
	}
	trace("GetStreamInfo path=%q", h.path)
	return a.listStreams(h.path, uint64(fi.Size()), fill)
}

// mountable returns the winfsp.BehaviourBase to hand to winfsp.Mount: the stream-aware
// wrapper when native forks are on, else the bare *Adapter (no stream enumeration).
func (a *Adapter) mountable() winfsp.BehaviourBase {
	if a.nativeForks {
		return streamAdapter{a}
	}
	return a
}

// peelStream splits a WinFsp path into its base file path and stream name, but only when
// native forks are enabled. With streams off it returns the whole path and an empty
// stream, so toStorePath still rejects any stray ':' as it always has.
func (a *Adapter) peelStream(winPath string) (base, stream string) {
	if !a.nativeForks {
		return winPath, ""
	}
	return splitStream(winPath)
}

// openStream opens a named stream on storePath and returns a handle. The base file must
// already exist (WinFsp opens the base then the stream). streamData is delegated to the
// normal data-fork path by the caller, so this only handles the fork/record streams.
func (a *Adapter) openStream(
	storePath string, k streamKind, flag int, info *winfsp.FSP_FSCTL_FILE_INFO,
) (uintptr, error) {
	// The base file's stat drives the shared FILE_INFO fields (attributes, times, id);
	// the size is then overridden with the stream's own length below.
	fi, err := a.fsys.Stat(storePath)
	if err != nil {
		return 0, err
	}
	if fi.IsDir() {
		// SFM forks live on files, not directories.
		return 0, errNoSuchStream
	}
	h := &openFile{path: storePath, flag: flag, stream: k}

	switch k {
	case streamResource:
		// A writable open of the resource fork must create it if absent — Windows opens a
		// stream to write it whether or not the fork exists yet (the ForkEngine returns
		// ErrNotExist for a missing fork opened without O_CREATE).
		if flag != os.O_RDONLY {
			flag |= os.O_CREATE
		}
		rf, err := a.fsys.OpenFork(storePath, fs.ResourceFork, flag)
		if err != nil {
			return 0, err
		}
		h.f = rf
	case streamAfpInfo, streamComments:
		buf, err := a.readRecordStream(storePath, k)
		if err != nil {
			return 0, err
		}
		h.streamBuf = buf
	default:
		return 0, errNoSuchStream
	}

	a.fillFileInfo(info, storePath, fi)
	if sz, err := a.streamSize(h); err == nil {
		info.FileSize = uint64(sz)
		info.AllocationSize = (info.FileSize + 4095) / 4096 * 4096
	}
	return a.handles.add(h), nil
}

// streamSize returns the current length of a stream handle's fork/record. For the resource
// fork it reads the live fs.File's Stat (which reflects unflushed writes), not ForkLen —
// the fork buffers writes and only persists on Sync/Close, so ForkLen would report the
// stale on-disk length between a Write and its flush.
func (a *Adapter) streamSize(h *openFile) (int64, error) {
	switch h.stream {
	case streamResource:
		if h.f == nil {
			return a.fsys.ForkLen(h.path, fs.ResourceFork)
		}
		fi, err := h.f.Stat()
		if err != nil {
			return 0, err
		}
		return fi.Size(), nil
	case streamAfpInfo, streamComments:
		return int64(len(h.streamBuf)), nil
	default:
		return 0, errNoSuchStream
	}
}

// readStream serves a Read on a stream handle. The resource fork reads through its live
// fs.File; a record stream reads out of its in-memory buffer.
func (a *Adapter) readStream(h *openFile, buf []byte, offset uint64) (int, error) {
	if h.stream == streamResource {
		if h.f == nil {
			return 0, os.ErrInvalid
		}
		return h.f.ReadAt(buf, int64(offset))
	}
	if offset >= uint64(len(h.streamBuf)) {
		return 0, nil
	}
	n := copy(buf, h.streamBuf[offset:])
	return n, nil
}

// writeStream serves a Write on a stream handle. The resource fork writes through its
// live fs.File; a record stream mutates its in-memory buffer (flushed on Flush/Cleanup).
func (a *Adapter) writeStream(h *openFile, buf []byte, offset uint64, writeToEnd bool) (int, error) {
	if a.readOnly {
		return 0, os.ErrPermission
	}
	if h.stream == streamResource {
		if h.f == nil {
			return 0, os.ErrInvalid
		}
		off := int64(offset)
		if writeToEnd {
			if fi, err := h.f.Stat(); err == nil {
				off = fi.Size()
			}
		}
		return h.f.WriteAt(buf, off)
	}
	off := int(offset)
	if writeToEnd {
		off = len(h.streamBuf)
	}
	if end := off + len(buf); end > len(h.streamBuf) {
		grown := make([]byte, end)
		copy(grown, h.streamBuf)
		h.streamBuf = grown
	}
	copy(h.streamBuf[off:], buf)
	h.streamDirty = true
	return len(buf), nil
}

// truncateStream serves SetFileSize on a stream handle.
func (a *Adapter) truncateStream(h *openFile, size int64) error {
	if a.readOnly {
		return os.ErrPermission
	}
	if h.stream == streamResource {
		if h.f == nil {
			return os.ErrInvalid
		}
		return h.f.Truncate(size)
	}
	if int(size) < len(h.streamBuf) {
		h.streamBuf = h.streamBuf[:size]
	} else if int(size) > len(h.streamBuf) {
		grown := make([]byte, size)
		copy(grown, h.streamBuf)
		h.streamBuf = grown
	}
	h.streamDirty = true
	return nil
}

// flushStream persists a dirty record stream through the ForkEngine. The resource fork
// is flushed through its fs.File; a clean record stream is a no-op.
func (a *Adapter) flushStream(h *openFile) error {
	if h.stream == streamResource {
		if h.f != nil {
			return h.f.Sync()
		}
		return nil
	}
	if !h.streamDirty {
		return nil
	}
	if err := a.flushRecordStream(h.path, h.stream, h.streamBuf); err != nil {
		return err
	}
	h.streamDirty = false
	return nil
}

// streamFileInfo fills a FILE_INFO for a stream handle: the base file's shared fields
// with the stream's own size.
func (a *Adapter) streamFileInfo(info *winfsp.FSP_FSCTL_FILE_INFO, h *openFile) error {
	fi, err := a.fsys.Stat(h.path)
	if err != nil {
		return err
	}
	a.fillFileInfo(info, h.path, fi)
	if sz, err := a.streamSize(h); err == nil {
		info.FileSize = uint64(sz)
		info.AllocationSize = (info.FileSize + 4095) / 4096 * 4096
	}
	return nil
}

// listStreams reports the streams present on a data file to a GetStreamInfo fill
// callback: always the unnamed data stream, plus any SFM stream that currently has
// content. A resource fork with zero length and absent Finder info / comment are
// omitted so the file does not advertise empty forks.
func (a *Adapter) listStreams(
	storePath string, dataSize uint64,
	fill func(name string, size, alloc uint64) (bool, error),
) error {
	alloc := func(n uint64) uint64 { return (n + 4095) / 4096 * 4096 }

	// The unnamed data stream is always present.
	if ok, err := fill("", dataSize, alloc(dataSize)); err != nil || !ok {
		return err
	}

	// Resource fork, when non-empty.
	if n, err := a.fsys.ForkLen(storePath, fs.ResourceFork); err == nil && n > 0 {
		if ok, err := fill(streamNameResource, uint64(n), alloc(uint64(n))); err != nil || !ok {
			return err
		}
	}

	// AfpInfo, when the file carries Finder info.
	if _, ok, err := a.fsys.ReadFinderInfo(storePath); err == nil && ok {
		if ok, err := fill(streamNameAfpInfo, fs.AfpInfoSize, alloc(fs.AfpInfoSize)); err != nil || !ok {
			return err
		}
	}

	// Comments, when present and non-empty.
	if c, ok := a.fsys.ReadComment(storePath); ok && len(c) > 0 {
		if ok, err := fill(streamNameComments, uint64(len(c)), alloc(uint64(len(c)))); err != nil || !ok {
			return err
		}
	}
	return nil
}
