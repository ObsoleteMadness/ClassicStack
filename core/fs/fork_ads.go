package fs

import (
	"errors"
	"io"
	stdfs "io/fs"
	"os"
)

// adsForkEngine stores resource forks and Finder metadata in NTFS alternate data
// streams, the layout Services for Macintosh (SFM) and modern SMB use, so a fork
// written by ClassicStack is readable by Windows SFM/SMB and vice-versa
// (spec/16-storage-seam.md §1b):
//
//   - the resource fork is the "<name>:AFP_Resource" stream;
//   - the 32-byte FinderInfo lives inside a 60-byte AfpInfo record in the
//     "<name>:AFP_AfpInfo" stream;
//   - the Finder comment is the "<name>:Comments" stream.
//
// The engine has two modes, chosen by whether the base FileSystem already speaks forks:
//
//   - Plain host directory (local_fs, no native ForkEngine): forks are STORED in real
//     NTFS alternate data streams, addressed via the host "path:stream" syntax. This is
//     the server case, and requires an NTFS volume (see requireNTFS / ErrNotNTFS).
//   - Client mount of a native-fork protocol (AFP: the base implements ForkEngine): the
//     remote volume already HAS the forks, so this engine reads/writes them THROUGH the
//     base's native ForkEngine (FPOpenFork / Finder-info on the wire) and merely PRESENTS
//     them under the SFM stream names to the WinFsp mount. It never appends
//     ":AFP_Resource" to a wire path (which would ask the server for a bogus filename),
//     and the NTFS requirement does not apply.
//
// In the storage mode the FinderInfo bytes are
// identical to the AppleDouble FinderInfo entry — only the container differs.
type adsForkEngine struct {
	fs FileSystem
	// native is the base's own fork engine when the base already speaks forks (a client
	// mount of a native-fork protocol like AFP). When set, the resource fork / Finder
	// info / comment are read and written THROUGH it (the wire), and this engine only
	// PRESENTS them under the SFM stream names to the mount — it never appends
	// ":AFP_Resource" to a wire path. When nil (a plain host directory such as local_fs),
	// forks are stored in real NTFS alternate data streams via "path:stream" keys.
	native ForkEngine
}

func newADSForkEngine(base FileSystem) *adsForkEngine {
	e := &adsForkEngine{fs: base}
	if fe, ok := base.(ForkEngine); ok {
		e.native = fe
	}
	return e
}

// ErrNotNTFS is returned when the "ads" fork backend is selected over a base that is
// not an NTFS volume. The SFM alternate-data-stream layout only exists on NTFS — on any
// other filesystem the "path:stream" syntax is not a real stream, so we fail the share
// build loudly rather than silently writing a broken/degraded container.
var ErrNotNTFS = errors.New("fs: ads fork backend requires an NTFS volume")

// volumeIsNTFS reports whether the volume backing hostPath is NTFS. It is an injected
// seam (installed by fork_ads_ntfs_windows.go via GetVolumeInformationW) so core/fs
// stays syscall-free on non-Windows / TinyGo builds — the same pattern as
// hostNativeDOSAttr. ok is false when the volume type cannot be determined; on a build
// with no probe installed (any non-Windows OS) it is nil, and an NTFS volume cannot
// exist there anyway, so the ads factory rejects.
var volumeIsNTFS func(hostPath string) (isNTFS bool, ok bool)

// init registers the "ads" fork adapter (NTFS alternate-data-stream layout, §1b) into
// the fork-adapter registry, so it is available exactly when this file is linked. The
// factory rejects a base that is not on an NTFS volume (ErrNotNTFS), because the SFM
// stream layout is meaningless off NTFS.
func init() {
	RegisterForkAdapter("ads", func(spec ShareSpec, base FileSystem) (ForkEngine, error) {
		_ = spec
		// When the base already speaks forks (a client mount of AFP), the ads engine
		// PRESENTS those wire forks under the SFM stream names — it does not store local
		// NTFS streams, so the on-disk NTFS requirement does not apply. The NTFS check is
		// only for a real host directory where the streams must live in actual ADS.
		if _, native := base.(ForkEngine); !native {
			if err := requireNTFS(base); err != nil {
				return nil, err
			}
		}
		return newADSForkEngine(base), nil
	})
}

// requireNTFS fails when base is a REAL host volume that is not NTFS — the case that
// would silently write a broken container, which is what an operator must be warned
// about. A base that is not host-backed (memfs, zipfs, a synthetic store) is NOT a host
// volume at all: there its "path:stream" keys are ordinary keys the store round-trips
// faithfully (this is how the ads engine's own unit tests run), so it is allowed. The
// check therefore targets exactly the misconfiguration in scope: a host directory that
// happens to sit on FAT/exFAT/a network filesystem instead of NTFS.
func requireNTFS(base FileSystem) error {
	hp, ok := base.(HostPather)
	if !ok {
		// Not a real host volume (memfs/zipfs/synthetic) — streams are simulated as
		// ordinary keys; nothing to validate.
		return nil
	}
	root, ok := hp.HostPath("")
	if !ok {
		// Host-backed but the root does not resolve to a host path; treat as
		// non-host for this check rather than failing a synthetic root.
		return nil
	}
	if volumeIsNTFS == nil {
		// A host-backed base on a build with no NTFS probe (any non-Windows OS): NTFS
		// cannot exist there, so ads over a real host directory is a misconfiguration.
		return ErrNotNTFS
	}
	isNTFS, ok := volumeIsNTFS(root)
	if !ok {
		// Could not determine the volume type for a real host path; fail closed rather
		// than write a possibly-broken container.
		return ErrNotNTFS
	}
	if !isNTFS {
		return ErrNotNTFS
	}
	return nil
}

// NTFS stream names SFM/SMB use for the AFP forks and metadata. These MUST match
// the names NT Services for Macintosh defines (macfile.h AFP_*_STREAM), so a fork
// written by ClassicStack is byte-for-byte interoperable with Windows SFM/SMB:
//
//	:AFP_Resource   the resource fork
//	:AFP_AfpInfo    the 60-byte AfpInfo record (holds the 32-byte FinderInfo)
//	:Comments       the Finder comment
//
// The volume-level SFM streams (:AFP_IdIndex, the CNID database; :AFP_DeskTop, the
// desktop DB) are NOT the per-file fork engine's concern — ClassicStack tracks
// CNIDs in the range-scannable metastore (meta_ads.go) instead, which SFM's single
// opaque :AFP_IdIndex stream cannot do — so they are deliberately not reproduced here.
const (
	adsResourceStream = "AFP_Resource"
	adsAfpInfoStream  = "AFP_AfpInfo"
	adsCommentStream  = "Comments"
)

// resourceStreamPath returns the "<path>:AFP_Resource" stream path.
func resourceStreamPath(path string) string { return path + ":" + adsResourceStream }

// afpInfoStreamPath returns the "<path>:AFP_AfpInfo" stream path.
func afpInfoStreamPath(path string) string { return path + ":" + adsAfpInfoStream }

// commentStreamPath returns the "<path>:Comments" stream path.
func commentStreamPath(path string) string { return path + ":" + adsCommentStream }

// --- AfpInfo record (spec/16 §1b): the 60-byte SFM metadata stream. ---
//
// The record type and its codec are the exported fs.AfpInfo DTO (afpinfo.go),
// the single source of truth shared with the WinFsp mount client's AFP_AfpInfo
// stream. The unexported helpers below keep this engine's original names and are
// thin wrappers over that DTO.

const afpInfoSize = AfpInfoSize

// ErrBadAfpInfo marks a malformed or wrong-signature AfpInfo stream; callers
// treat it as "no FinderInfo present" rather than surfacing a decode error to a
// client, matching how SFM tolerates a missing/garbage stream.
var ErrBadAfpInfo = errors.New("fs: malformed AFP_AfpInfo record")

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
	return AfpInfo{BackupTime: a.backupTime, FinderInfo: a.finderInfo, ProDOSInfo: a.prodosInfo}.Marshal()
}

// parseAfpInfo decodes a 60-byte AfpInfo record, validating the signature.
func parseAfpInfo(b []byte) (afpInfo, error) {
	a, err := UnmarshalAfpInfo(b)
	if err != nil {
		return afpInfo{}, err
	}
	return afpInfo{backupTime: a.BackupTime, finderInfo: a.FinderInfo, prodosInfo: a.ProDOSInfo}, nil
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
	if e.native != nil {
		// Base owns the forks (AFP): open the real resource fork on the wire.
		return e.native.OpenFork(path, fork, flag)
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
	if e.native != nil {
		return e.native.ForkLen(path, fork)
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
	if e.native != nil {
		return e.native.ReadFinderInfo(path)
	}
	a, present, err := e.readAfpInfo(path)
	if err != nil || !present {
		return [32]byte{}, false, err
	}
	return a.finderInfo, true, nil
}

func (e *adsForkEngine) WriteFinderInfo(path string, info [32]byte) error {
	if e.native != nil {
		return e.native.WriteFinderInfo(path, info)
	}
	// Preserve any backupTime / prodosInfo a prior writer (e.g. Windows SFM) set.
	a, _, err := e.readAfpInfo(path)
	if err != nil {
		return err
	}
	a.finderInfo = info
	return e.writeAll(afpInfoStreamPath(path), encodeAfpInfo(a))
}

// ReadComment reads the Finder comment from the "<path>:Comments" stream — the SFM
// AFP_COMM_STREAM. ok is false when the stream is absent or empty.
func (e *adsForkEngine) ReadComment(path string) (c []byte, ok bool) {
	if e.native != nil {
		return e.native.ReadComment(path)
	}
	b, err := e.readAll(commentStreamPath(path))
	if err != nil || len(b) == 0 {
		return nil, false
	}
	return b, true
}

// WriteComment writes the Finder comment to the "<path>:Comments" stream. An empty
// comment removes the stream (SFM RemoveComment semantics) rather than leaving a
// zero-length one.
func (e *adsForkEngine) WriteComment(path string, c []byte) error {
	if e.native != nil {
		return e.native.WriteComment(path, c)
	}
	if len(c) == 0 {
		if err := e.fs.Remove(commentStreamPath(path)); err != nil && !errors.Is(err, stdfs.ErrNotExist) {
			return err
		}
		return nil
	}
	return e.writeAll(commentStreamPath(path), c)
}

// adsMetadataStreams are the per-file SFM streams that ride with the data file and
// must be moved/deleted alongside it.
func adsMetadataStreams() []func(string) string {
	return []func(string) string{resourceStreamPath, afpInfoStreamPath, commentStreamPath}
}

func (e *adsForkEngine) MoveMetadata(old, new string) error {
	if e.native != nil {
		// The remote volume carries forks with the file; its own Rename moves them.
		return e.native.MoveMetadata(old, new)
	}
	for _, stream := range adsMetadataStreams() {
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
	if e.native != nil {
		return e.native.DeleteMetadata(path)
	}
	for _, stream := range adsMetadataStreams() {
		if err := e.fs.Remove(stream(path)); err != nil && !errors.Is(err, stdfs.ErrNotExist) {
			return err
		}
	}
	return nil
}

var _ ForkEngine = (*adsForkEngine)(nil)
