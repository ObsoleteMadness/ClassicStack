package fs

import (
	"errors"
	"io"
	stdfs "io/fs"
	"os"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"
)

// adsForkEngine stores resource forks and Finder metadata in NTFS alternate data
// streams, the layout Services for Macintosh (SFM) and modern SMB use, so a fork
// written by ClassicStack is readable by Windows SFM/SMB and vice-versa
// (spec/16-storage-seam.md §1b):
//
//   - the resource fork is the "<name>:AFP_Resource" stream;
//   - the 32-byte FinderInfo lives inside a 60-byte AfpInfo record in the
//     "<name>:AFP_AfpInfo" stream.
//
// Streams are addressed through the base FileSystem using the host's "path:stream"
// syntax: on an NTFS-backed FileSystem those resolve to real alternate data
// streams; on any other FileSystem they degrade to ordinary sidecar paths, so the
// engine's record handling stays testable without NTFS. The FinderInfo bytes are
// identical to the AppleDouble FinderInfo entry — only the container differs.
type adsForkEngine struct {
	fs FileSystem
}

func newADSForkEngine(base FileSystem) *adsForkEngine {
	return &adsForkEngine{fs: base}
}

// init registers the "ads" fork adapter (NTFS alternate-data-stream layout, §1b) into
// the fork-adapter registry, so it is available exactly when this file is linked.
func init() {
	RegisterForkAdapter("ads", func(spec ShareSpec, base FileSystem) (ForkEngine, error) {
		_ = spec
		return newADSForkEngine(base), nil
	})
}

// NTFS stream names SFM/SMB use for the AFP forks.
const (
	adsResourceStream = "AFP_Resource"
	adsAfpInfoStream  = "AFP_AfpInfo"
)

// resourceStreamPath returns the "<path>:AFP_Resource" stream path.
func resourceStreamPath(path string) string { return path + ":" + adsResourceStream }

// afpInfoStreamPath returns the "<path>:AFP_AfpInfo" stream path.
func afpInfoStreamPath(path string) string { return path + ":" + adsAfpInfoStream }

// --- AfpInfo record (spec/16 §1b): the 60-byte SFM metadata stream. ---

const (
	afpInfoSize = 60

	afpInfoSignature uint32 = 0x41465000 // 'A''F''P''\0'
	afpInfoVersion   uint32 = 0x00010000

	afpInfoFinderOff = 16 // finderInfo[32] starts here
	afpInfoFinderLen = 32
)

// errBadAfpInfo marks a malformed or wrong-signature AfpInfo stream; the engine
// treats it as "no FinderInfo present" rather than surfacing a decode error to a
// client, matching how SFM tolerates a missing/garbage stream.
var errBadAfpInfo = errors.New("fs: malformed AFP_AfpInfo record")

// afpInfo is the decoded AfpInfo record. Only the FinderInfo is exposed through
// the ForkEngine today; backupTime / prodosInfo are preserved on round-trip so a
// record written by Windows SFM is not clobbered.
type afpInfo struct {
	backupTime uint32
	finderInfo [32]byte
	prodosInfo [6]byte
}

// encodeAfpInfo builds a canonical 60-byte AfpInfo record.
func encodeAfpInfo(a afpInfo) []byte {
	b := make([]byte, afpInfoSize)
	bp.PutBE32(b[0:4], afpInfoSignature)
	bp.PutBE32(b[4:8], afpInfoVersion)
	// b[8:12] reserved1, b[12:16] backupTime.
	bp.PutBE32(b[12:16], a.backupTime)
	copy(b[afpInfoFinderOff:afpInfoFinderOff+afpInfoFinderLen], a.finderInfo[:])
	copy(b[48:54], a.prodosInfo[:])
	// b[54:60] reserved2.
	return b
}

// parseAfpInfo decodes a 60-byte AfpInfo record, validating the signature.
func parseAfpInfo(b []byte) (afpInfo, error) {
	var a afpInfo
	if len(b) < afpInfoSize {
		return a, errBadAfpInfo
	}
	if bp.BE32(b[0:4]) != afpInfoSignature {
		return a, errBadAfpInfo
	}
	a.backupTime = bp.BE32(b[12:16])
	copy(a.finderInfo[:], b[afpInfoFinderOff:afpInfoFinderOff+afpInfoFinderLen])
	copy(a.prodosInfo[:], b[48:54])
	return a, nil
}

// --- small whole-stream read/write helpers over the base FileSystem. ---

func (e *adsForkEngine) readAll(path string) ([]byte, error) {
	f, err := e.fs.OpenFile(path, os.O_RDONLY)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	buf := make([]byte, info.Size())
	if len(buf) == 0 {
		return buf, nil
	}
	n, err := f.ReadAt(buf, 0)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return buf[:n], nil
}

func (e *adsForkEngine) writeAll(path string, b []byte) error {
	f, err := e.fs.OpenFile(path, os.O_RDWR|os.O_CREATE)
	if err != nil {
		f, err = e.fs.CreateFile(path)
		if err != nil {
			return err
		}
	}
	defer func() { _ = f.Close() }()
	if err := f.Truncate(0); err != nil {
		return err
	}
	if len(b) == 0 {
		return f.Sync()
	}
	if _, err := f.WriteAt(b, 0); err != nil {
		return err
	}
	return f.Sync()
}

// readAfpInfo reads and decodes the AfpInfo stream, if present.
func (e *adsForkEngine) readAfpInfo(path string) (afpInfo, bool, error) {
	b, err := e.readAll(afpInfoStreamPath(path))
	if err != nil {
		if errors.Is(err, stdfs.ErrNotExist) {
			return afpInfo{}, false, nil
		}
		return afpInfo{}, false, err
	}
	a, err := parseAfpInfo(b)
	if err != nil {
		// A garbage stream is treated as absent, not fatal (SFM tolerance).
		return afpInfo{}, false, nil
	}
	return a, true, nil
}

// --- ForkEngine ---

func (e *adsForkEngine) OpenFork(path string, fork ForkType, flag int) (File, error) {
	if fork == DataFork {
		// The data fork is the unnamed stream — the file itself.
		return e.fs.OpenFile(path, flag)
	}
	// The resource fork is a real stream backed directly by the base FileSystem,
	// so reads/writes stream straight through without buffering the whole fork.
	streamPath := resourceStreamPath(path)
	f, err := e.fs.OpenFile(streamPath, flag)
	if err != nil {
		if errors.Is(err, stdfs.ErrNotExist) && flag&os.O_CREATE != 0 {
			return e.fs.CreateFile(streamPath)
		}
		return nil, err
	}
	return f, nil
}

func (e *adsForkEngine) ForkLen(path string, fork ForkType) (int64, error) {
	if fork == DataFork {
		info, err := e.fs.Stat(path)
		if err != nil {
			return 0, err
		}
		return info.Size(), nil
	}
	info, err := e.fs.Stat(resourceStreamPath(path))
	if err != nil {
		if errors.Is(err, stdfs.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	return info.Size(), nil
}

func (e *adsForkEngine) ReadFinderInfo(path string) (info [32]byte, ok bool, err error) {
	a, present, err := e.readAfpInfo(path)
	if err != nil || !present {
		return [32]byte{}, false, err
	}
	return a.finderInfo, true, nil
}

func (e *adsForkEngine) WriteFinderInfo(path string, info [32]byte) error {
	// Preserve any backupTime / prodosInfo a prior writer (e.g. Windows SFM) set.
	a, _, err := e.readAfpInfo(path)
	if err != nil {
		return err
	}
	a.finderInfo = info
	return e.writeAll(afpInfoStreamPath(path), encodeAfpInfo(a))
}

// ReadComment / WriteComment: the SFM ADS layout has no comment stream. Comments
// are an AFP-desktop concern; on ads shares they are not persisted to a stream.
func (e *adsForkEngine) ReadComment(path string) (c []byte, ok bool) {
	_ = path
	return nil, false
}

func (e *adsForkEngine) WriteComment(path string, c []byte) error {
	_ = path
	_ = c
	return nil
}

func (e *adsForkEngine) MoveMetadata(old, new string) error {
	for _, stream := range []func(string) string{resourceStreamPath, afpInfoStreamPath} {
		src := stream(old)
		if _, err := e.fs.Stat(src); err != nil {
			if errors.Is(err, stdfs.ErrNotExist) {
				continue
			}
			return err
		}
		if err := e.fs.Rename(src, stream(new)); err != nil {
			return err
		}
	}
	return nil
}

func (e *adsForkEngine) DeleteMetadata(path string) error {
	for _, stream := range []func(string) string{resourceStreamPath, afpInfoStreamPath} {
		if err := e.fs.Remove(stream(path)); err != nil && !errors.Is(err, stdfs.ErrNotExist) {
			return err
		}
	}
	return nil
}

var _ ForkEngine = (*adsForkEngine)(nil)
