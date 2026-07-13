package etherdfs

import (
	"errors"
	"io"
	iofs "io/fs"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	proto "github.com/ObsoleteMadness/ClassicStack/core/protocol/etherdfs"
)

// dispatch routes one decoded request frame to its opcode handler and returns
// the AX status word and reply payload. Retransmits — a frame whose sequence
// matches the client's last handled sequence — replay the cached reply rather than
// re-running the side effect (the reference server's dedup, important for
// non-idempotent ops like WRITE/RENAME/DELETE over a lossy segment).
//
// There is no dedicated wire opcode for "discovery": the reference client's
// auto-discovery (etherdfs "::") broadcasts an ordinary AL_DISKSPACE query for
// the drive it is about to map and learns the server's MAC from whichever reply
// arrives (see sendquery()'s updatermac in the reference client). AL_INSTALLCHK
// (0x00) is a DOS-side INT 2Fh installation-check subfunction the client's TSR
// handles locally by chaining to the previous handler — it is never sent over
// the wire. So a normal drive lookup below is what makes discovery work; there
// is deliberately no opcode-0x00 special case.
func (s *Service) dispatch(req proto.Frame) (status uint16, payload []byte, ok bool) {
	sess := s.sessions.get(req.SrcMAC)

	if cachedStatus, cachedPayload, hit := sess.cachedReply(req.Sequence); hit {
		return cachedStatus, cachedPayload, true
	}

	status, payload = s.handle(sess, req)
	sess.cacheReply(req.Sequence, status, payload)
	return status, payload, true
}

// handle dispatches a request against the addressed drive.
func (s *Service) handle(sess *session, req proto.Frame) (status uint16, payload []byte) {
	drv, ok := s.drive(req.Drive)
	if !ok {
		// No such drive: report path-not-found, the DOS error a redirector maps to
		// "invalid drive". This still answers (unlike the reference server, which
		// silently drops an out-of-range/unmapped drive), so a discovery probe
		// against any drive number gets a reply and learns our MAC.
		return proto.ErrPathNotFound, nil
	}

	switch req.Opcode {
	case proto.OpDiskspace:
		return s.handleDiskSpace(drv)
	case proto.OpGetattr:
		return s.handleGetAttr(drv, req.Payload)
	case proto.OpSetattr:
		return s.handleSetAttr(drv, req.Payload)
	case proto.OpFindFirst:
		return s.handleFindFirst(sess, drv, req.Payload)
	case proto.OpFindNext:
		return s.handleFindNext(sess, req.Payload)
	case proto.OpOpen:
		return s.handleOpen(sess, drv, req.Payload, false, false)
	case proto.OpCreate:
		return s.handleOpen(sess, drv, req.Payload, true, false)
	case proto.OpSpopnfil:
		return s.handleOpen(sess, drv, req.Payload, false, true)
	case proto.OpReadfil:
		return s.handleRead(sess, req.Payload)
	case proto.OpWritefil:
		return s.handleWrite(sess, drv, req.Payload)
	case proto.OpClsfil:
		return s.handleClose(sess, req.Payload)
	case proto.OpCmmtfil:
		return s.handleCommit(sess, req.Payload)
	case proto.OpSkfmend:
		return s.handleSeekFromEnd(sess, req.Payload)
	case proto.OpDelete:
		return s.handleDelete(drv, req.Payload)
	case proto.OpRename:
		return s.handleRename(drv, req.Payload)
	case proto.OpMkdir:
		return s.handleMkdir(drv, req.Payload)
	case proto.OpRmdir:
		return s.handleRmdir(drv, req.Payload)
	case proto.OpChdir:
		return s.handleChdir(drv, req.Payload)
	case proto.OpLockfil, proto.OpUnlockfil:
		// Lock/unlock are no-ops: this server does not enforce byte-range locks.
		return proto.ErrNone, nil
	case proto.OpInstallChk:
		// Not sent by the reference client (see dispatch's doc comment), but
		// answered harmlessly for any variant that does probe it: success plus
		// the advertised server name, matching the reference server's tolerant
		// "unknown query -> still respond if the drive/frame was valid" stance.
		return proto.ErrNone, []byte(s.serverName())
	default:
		// Unrecognised opcode: report access-denied rather than dropping, so the
		// client gets a definite (if unhelpful) answer rather than timing out.
		return proto.ErrAccessDenied, nil
	}
}

// handleDiskSpace answers AL_DISKSPACE: report the drive root's free/total space
// as DOS cluster geometry in fixed 32KB clusters (see proto.DiskSpaceStatus/
// DiskSpaceReply — the reference server's "MS-DOS tolerates only 1 [sector per
// cluster] here" constraint). The AX status word this call returns is the fixed
// DiskSpaceStatus DATA value, not a generic error code.
func (s *Service) handleDiskSpace(drv *Drive) (uint16, []byte) {
	total, free, err := drv.FS().DiskUsage("")
	if err != nil {
		// Report a small but non-zero geometry so the drive is usable even when the
		// backend cannot report usage (e.g. a synthetic fs).
		total, free = 0, 0
	}
	const bytesPerCluster = 32768 // one 32768-byte sector per cluster, per DiskSpaceStatus
	totalClusters := clampClusters(total / bytesPerCluster)
	freeClusters := clampClusters(free / bytesPerCluster)
	return proto.DiskSpaceStatus, proto.DiskSpaceReply{
		TotalClusters: totalClusters,
		FreeClusters:  freeClusters,
	}.Encode(nil)
}

// clampClusters caps a cluster count to the 16-bit DOS field (0xFFFF).
func clampClusters(n uint64) uint16 {
	if n > 0xFFFF {
		return 0xFFFF
	}
	return uint16(n)
}

// handleGetAttr answers AL_GETATTR: stat the path and report DOS time, size, and
// FAT attribute.
func (s *Service) handleGetAttr(drv *Drive, body []byte) (uint16, []byte) {
	p := drv.resolvePath(proto.DecodePathRequest(body).Path)
	info, err := drv.FS().Stat(p)
	if err != nil {
		return dosError(err), nil
	}
	return proto.ErrNone, proto.GetAttrReply{
		Time: dosDateTime(info.ModTime()),
		Size: clampSize(info.Size()),
		Attr: drv.fatAttr(p, info),
	}.Encode(nil)
}

// handleSetAttr answers AL_SETATTR: persist the requested FAT attribute bits
// (RO/HID/SYS/ARCH) through the drive's DOS-attribute store, so they survive on a
// host filesystem that cannot represent them (the §16 storage seam — metastore,
// Samba xattr, sidecar, or Windows-native passthrough per the share's backend).
// The directory/volume structural bits are ignored. A missing target is rejected.
func (s *Service) handleSetAttr(drv *Drive, body []byte) (uint16, []byte) {
	if drv.ReadOnly() {
		return proto.ErrAccessDenied, nil
	}
	r, err := proto.DecodeSetAttrRequest(body)
	if err != nil {
		return proto.ErrFileNotFound, nil
	}
	p := drv.resolvePath(r.Path)
	if _, err := drv.FS().Stat(p); err != nil {
		return dosError(err), nil
	}
	m := drv.meta()
	cur, _ := m.Attrs(p)
	cur.Attrs = uint16(r.Attr) & fs.DOSStorableMask
	if err := m.SetAttrs(p, cur); err != nil {
		return proto.ErrAccessDenied, nil
	}
	return proto.ErrNone, nil
}

// handleFindFirst answers AL_FINDFIRST: list the search directory, filter by the
// wildcard mask and attribute, pre-resolve each match's 8.3 short name, cache the
// cursor, and return the first entry (or no-more-files).
func (s *Service) handleFindFirst(sess *session, drv *Drive, body []byte) (uint16, []byte) {
	r, err := proto.DecodeFindFirstRequest(body)
	if err != nil {
		return proto.ErrNoMoreFiles, nil
	}
	storePath := drv.resolvePath(r.Path)
	dir, pattern := splitSearch(storePath)
	mask := wildcardToFCBMask(pattern)

	entries, err := drv.FS().ReadDir(dir)
	if err != nil {
		return proto.ErrPathNotFound, nil
	}

	cur := &findCursor{attr: r.Attr}
	for _, e := range entries {
		fe, ok := resolveFindEntry(drv, dir, e)
		if !ok {
			continue
		}
		if !matchFCB(mask, proto.FilenameToFCB(fe.shortName)) {
			continue
		}
		if !attrAllowed(fe.attr, r.Attr) {
			continue
		}
		cur.entries = append(cur.entries, fe)
	}
	sort.Slice(cur.entries, func(i, j int) bool {
		return cur.entries[i].shortName < cur.entries[j].shortName
	})
	if len(cur.entries) == 0 {
		return proto.ErrNoMoreFiles, nil
	}
	dirID := sess.addCursor(cur)
	return proto.ErrNone, findReplyAt(cur, 0, dirID)
}

// handleFindNext answers AL_FINDNEXT: advance the cursor identified by the request's
// directory ID/position and return the next entry (or no-more-files).
func (s *Service) handleFindNext(sess *session, body []byte) (uint16, []byte) {
	r, err := proto.DecodeFindNextRequest(body)
	if err != nil {
		return proto.ErrNoMoreFiles, nil
	}
	cur, ok := sess.cursor(r.DirID)
	if !ok {
		return proto.ErrNoMoreFiles, nil
	}
	pos := int(r.Position)
	if pos < 0 || pos >= len(cur.entries) {
		return proto.ErrNoMoreFiles, nil
	}
	return proto.ErrNone, findReplyAt(cur, pos, r.DirID)
}

// findReplyAt builds a FindReply for the cursor entry at pos, with the position
// field advanced to pos+1 so the next AL_FINDNEXT resumes after it.
func findReplyAt(cur *findCursor, pos int, dirID uint16) []byte {
	e := cur.entries[pos]
	return proto.FindReply{
		Attr:     e.attr,
		FCB:      proto.FilenameToFCB(e.shortName),
		Time:     e.dosTime,
		Size:     e.size,
		DirID:    dirID,
		Position: uint16(pos + 1),
	}.Encode(nil)
}

// handleOpen answers AL_OPEN / AL_CREATE / AL_SPOPNFIL. create truncates/creates;
// spopnfil carries an action code and an action-result in the reply. On success an
// open handle is registered and its file ID returned.
func (s *Service) handleOpen(sess *session, drv *Drive, body []byte, create, special bool) (uint16, []byte) {
	r, err := proto.DecodeOpenRequest(body)
	if err != nil {
		return proto.ErrFileNotFound, nil
	}
	p := drv.resolvePath(r.Path)
	if p == "" {
		return proto.ErrFileNotFound, nil
	}

	var f fs.File
	// Action (the CX result word) is only meaningful for AL_SPOPNFIL
	// (1=opened, 2=created, 3=truncated) but the reference server always
	// transmits it (0 for plain OPEN/CREATE, which ignore it) — see
	// spec/errata.md "Reply AX status..." / the OPEN reply's fixed 25-byte shape.
	var action uint16

	switch {
	case create:
		if drv.ReadOnly() {
			return proto.ErrAccessDenied, nil
		}
		nf, err := drv.FS().CreateFile(p)
		if err != nil {
			return dosError(err), nil
		}
		f = nf
		if special {
			action = 2 // created
		}
	default:
		flag := os.O_RDWR
		if drv.ReadOnly() {
			flag = os.O_RDONLY
		}
		nf, err := drv.FS().OpenFile(p, flag)
		if err != nil {
			// SPOPNFIL with a create-if-missing action falls back to create.
			if special && createOnMissing(r.Action) && !drv.ReadOnly() {
				cf, cerr := drv.FS().CreateFile(p)
				if cerr != nil {
					return dosError(cerr), nil
				}
				f = cf
				action = 2
			} else {
				return dosError(err), nil
			}
		} else {
			f = nf
			if special {
				action = 1 // opened existing
			}
		}
	}

	info, _ := f.Stat()
	of := &openFile{file: f, path: p, readOnly: drv.ReadOnly()}
	fid, ok := sess.addFile(of)
	if !ok {
		_ = f.Close()
		return proto.ErrAccessDenied, nil
	}

	// Mode is the access-mode byte the client stores in the SFT's open_mode low
	// byte (ETHERDFS.C: "sftptr->open_mode |= answer[24]") and uses to decide
	// whether writes through this handle are allowed at all — sending a fixed 0
	// (DOS access code "read-only") silences every subsequent AL_WRITEFIL
	// regardless of what the caller asked for. The reference server's three
	// opcodes each derive it differently: AL_CREATE hardcodes "read/write" (2);
	// AL_SPOPNFIL echoes the request's open-mode word (MM) masked to 7 bits (the
	// FCB-open bit, bit 7, is handled separately by the client); plain AL_OPEN
	// echoes the request's SS word, which carries the desired access mode there
	// (not a create attribute, unlike AL_CREATE's SS).
	var mode uint8
	switch {
	case create:
		mode = 2 // read/write
	case special:
		mode = uint8(r.OpenMode & 0x7f)
	default:
		mode = uint8(r.Attr & 0xff)
	}

	rep := proto.OpenReply{
		Attr:   drv.fatAttr(p, info),
		FCB:    proto.FilenameToFCB(shortBase(drv, p)),
		Time:   dosDateTime(modTimeOf(info)),
		Size:   sizeOf(info),
		FileID: fid,
		Action: action,
		Mode:   mode,
	}
	return proto.ErrNone, rep.Encode(nil)
}

// handleRead answers AL_READFIL: read Length bytes at Offset from the open file.
func (s *Service) handleRead(sess *session, body []byte) (uint16, []byte) {
	r, err := proto.DecodeReadRequest(body)
	if err != nil {
		return proto.ErrInvalidHandle, nil
	}
	of, ok := sess.file(r.FileID)
	if !ok {
		return proto.ErrInvalidHandle, nil
	}
	buf := make([]byte, r.Length)
	n, err := of.file.ReadAt(buf, int64(r.Offset))
	if err != nil && !errors.Is(err, io.EOF) {
		return proto.ErrReadFault, nil
	}
	return proto.ErrNone, proto.ReadReply(buf[:n])
}

// handleWrite answers AL_WRITEFIL: write Data at Offset to the open file, or
// truncate-at-offset for a zero-length write. Returns the bytes written.
func (s *Service) handleWrite(sess *session, drv *Drive, body []byte) (uint16, []byte) {
	r, err := proto.DecodeWriteRequest(body)
	if err != nil {
		return proto.ErrInvalidHandle, nil
	}
	of, ok := sess.file(r.FileID)
	if !ok {
		return proto.ErrInvalidHandle, nil
	}
	if of.readOnly || drv.ReadOnly() {
		return proto.ErrAccessDenied, nil
	}
	if len(r.Data) == 0 {
		// A zero-byte write at offset truncates the file there (DOS convention).
		if err := of.file.Truncate(int64(r.Offset)); err != nil {
			return proto.ErrWriteFault, nil
		}
		return proto.ErrNone, proto.WriteReply(0)
	}
	n, err := of.file.WriteAt(r.Data, int64(r.Offset))
	if err != nil {
		return proto.ErrWriteFault, nil
	}
	return proto.ErrNone, proto.WriteReply(uint16(n))
}

// handleClose answers AL_CLSFIL: close the open handle. Always succeeds.
func (s *Service) handleClose(sess *session, body []byte) (uint16, []byte) {
	if id, ok := fileIDFromBody(body); ok {
		sess.closeFile(id)
	}
	return proto.ErrNone, nil
}

// handleCommit answers AL_CMMTFIL: flush the open handle to the backend.
func (s *Service) handleCommit(sess *session, body []byte) (uint16, []byte) {
	if id, ok := fileIDFromBody(body); ok {
		if of, ok := sess.file(id); ok {
			_ = of.file.Sync()
		}
	}
	return proto.ErrNone, nil
}

// handleSeekFromEnd answers AL_SKFMEND: compute the absolute offset from the file
// size plus the (signed) request offset and return it. The redirector uses this to
// implement SEEK_END without the server holding a cursor.
func (s *Service) handleSeekFromEnd(sess *session, body []byte) (uint16, []byte) {
	r, err := proto.DecodeSeekFromEndRequest(body)
	if err != nil {
		return proto.ErrInvalidHandle, nil
	}
	of, ok := sess.file(r.FileID)
	if !ok {
		return proto.ErrInvalidHandle, nil
	}
	info, err := of.file.Stat()
	if err != nil {
		return proto.ErrReadFault, nil
	}
	end := info.Size()
	abs := max(end+int64(r.Offset), 0)
	return proto.ErrNone, proto.SeekReply(uint32(abs))
}

// handleDelete answers AL_DELETE.
func (s *Service) handleDelete(drv *Drive, body []byte) (uint16, []byte) {
	if drv.ReadOnly() {
		return proto.ErrAccessDenied, nil
	}
	p := drv.resolvePath(proto.DecodePathRequest(body).Path)
	if err := drv.FS().Remove(p); err != nil {
		return dosError(err), nil
	}
	_ = drv.meta().DeleteAttrs(p)
	return proto.ErrNone, nil
}

// handleRename answers AL_RENAME.
func (s *Service) handleRename(drv *Drive, body []byte) (uint16, []byte) {
	if drv.ReadOnly() {
		return proto.ErrAccessDenied, nil
	}
	r, err := proto.DecodeRenameRequest(body)
	if err != nil {
		return proto.ErrFileNotFound, nil
	}
	src := drv.resolvePath(r.Src)
	dst := drv.resolvePath(r.Dst)
	if _, err := drv.FS().Stat(dst); err == nil {
		return proto.ErrAccessDenied, nil // destination exists
	}
	if err := drv.FS().Rename(src, dst); err != nil {
		return dosError(err), nil
	}
	_ = drv.meta().RenameAttrs(src, dst)
	return proto.ErrNone, nil
}

// handleMkdir answers AL_MKDIR.
func (s *Service) handleMkdir(drv *Drive, body []byte) (uint16, []byte) {
	if drv.ReadOnly() {
		return proto.ErrAccessDenied, nil
	}
	p := drv.resolvePath(proto.DecodePathRequest(body).Path)
	if err := drv.FS().CreateDir(p); err != nil {
		return dosError(err), nil
	}
	return proto.ErrNone, nil
}

// handleRmdir answers AL_RMDIR.
func (s *Service) handleRmdir(drv *Drive, body []byte) (uint16, []byte) {
	if drv.ReadOnly() {
		return proto.ErrAccessDenied, nil
	}
	p := drv.resolvePath(proto.DecodePathRequest(body).Path)
	if err := drv.FS().Remove(p); err != nil {
		return dosError(err), nil
	}
	return proto.ErrNone, nil
}

// handleChdir answers AL_CHDIR: validate the target directory exists. The server
// holds no per-client current directory (the client tracks it); this only confirms
// the path is a directory so the redirector accepts the CD.
func (s *Service) handleChdir(drv *Drive, body []byte) (uint16, []byte) {
	p := drv.resolvePath(proto.DecodePathRequest(body).Path)
	if p == "" {
		return proto.ErrNone, nil // root always exists
	}
	info, err := drv.FS().Stat(p)
	if err != nil {
		return proto.ErrPathNotFound, nil
	}
	if !info.IsDir() {
		return proto.ErrPathNotFound, nil
	}
	return proto.ErrNone, nil
}

// --- helpers -------------------------------------------------------------

// splitSearch splits a resolved search path into its directory and final-element
// pattern (which may contain wildcards). A path with no separator searches the
// root with the whole path as the pattern.
func splitSearch(p string) (dir, pattern string) {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[:i], p[i+1:]
	}
	return "", p
}

// resolveFindEntry builds a findEntry for a directory entry: its 8.3 short name
// (via the drive's NameEngine), size, modtime, and FAT attribute.
func resolveFindEntry(drv *Drive, dir string, e iofs.DirEntry) (findEntry, bool) {
	full := e.Name()
	if dir != "" {
		full = dir + "/" + e.Name()
	}
	short, err := drv.FS().ShortName(full)
	if err != nil || short == "" {
		short = strings.ToUpper(e.Name())
	}
	info, err := e.Info()
	if err != nil {
		return findEntry{}, false
	}
	return findEntry{
		shortName: short,
		size:      clampSize(info.Size()),
		dosTime:   dosDateTime(info.ModTime()),
		attr:      drv.fatAttr(full, info),
	}, true
}

// attrAllowed reports whether an entry with attribute entryAttr is admitted by a
// find request's attribute filter searchAttr. The DOS rule: regular files always
// match; hidden/system/directory entries match only when the corresponding bit is
// requested.
func attrAllowed(entryAttr, searchAttr uint8) bool {
	const special = proto.AttrHidden | proto.AttrSystem | proto.AttrDirectory
	excluded := entryAttr & special &^ searchAttr
	return excluded == 0
}

// fatAttr derives the FAT attribute byte for store path p: the structural bit
// (Directory) from the entry, the persisted RO/HID/SYS/ARCH bits from the drive's
// DOS-attribute store when present, else the host-derived defaults (Archive for a
// file, plus ReadOnly from a read-only drive or a write-denied mode). A persisted
// value is authoritative for the storable bits — it is how an attribute the host
// filesystem cannot represent (Hidden/System on POSIX) survives.
func (d *Drive) fatAttr(p string, info iofs.FileInfo) uint8 {
	var a uint8
	if info != nil && info.IsDir() {
		a |= proto.AttrDirectory
	}

	if stored, ok := d.meta().Attrs(p); ok {
		a |= uint8(stored.Attrs & fs.DOSStorableMask)
		if d.ReadOnly() {
			a |= proto.AttrReadOnly
		}
		if a&proto.AttrDirectory == 0 && a&^proto.AttrReadOnly == 0 {
			a |= proto.AttrArchive // a plain file always carries Archive
		}
		return a
	}

	// No stored attributes: derive from the host entry.
	if info == nil || !info.IsDir() {
		a |= proto.AttrArchive
	}
	if d.ReadOnly() || (info != nil && info.Mode().Perm()&0o200 == 0) {
		a |= proto.AttrReadOnly
	}
	return a
}

// clampSize caps a file size to the 32-bit DOS size field.
func clampSize(n int64) uint32 {
	if n < 0 {
		return 0
	}
	if n > 0xFFFFFFFF {
		return 0xFFFFFFFF
	}
	return uint32(n)
}

// shortBase returns the 8.3 short name of a store path's final element.
func shortBase(drv *Drive, p string) string {
	short, err := drv.FS().ShortName(p)
	if err != nil || short == "" {
		base := p
		if i := strings.LastIndexByte(p, '/'); i >= 0 {
			base = p[i+1:]
		}
		return strings.ToUpper(base)
	}
	return short
}

// fileIDFromBody reads the trailing 2-byte file ID some bare-handle ops carry.
func fileIDFromBody(body []byte) (uint16, bool) {
	if len(body) < 2 {
		return 0, false
	}
	return uint16(body[0]) | uint16(body[1])<<8, true
}

// createOnMissing reports whether an AL_SPOPNFIL action code requests creating the
// file when it does not exist (the DOS "create new" / "create or open" actions set
// the low nibble's create bit).
func createOnMissing(action uint16) bool { return action&0x0010 != 0 }

// modTimeOf / sizeOf read a possibly-nil FileInfo defensively (CreateFile/OpenFile
// may return a handle whose Stat the dispatch did not insist on).
func modTimeOf(info iofs.FileInfo) time.Time {
	if info != nil {
		return info.ModTime()
	}
	return time.Time{}
}

func sizeOf(info iofs.FileInfo) uint32 {
	if info == nil {
		return 0
	}
	return clampSize(info.Size())
}

// dosError maps a filesystem error to the DOS error code the redirector expects.
func dosError(err error) uint16 {
	switch {
	case err == nil:
		return proto.ErrNone
	case errors.Is(err, iofs.ErrNotExist):
		return proto.ErrFileNotFound
	case errors.Is(err, iofs.ErrPermission):
		return proto.ErrAccessDenied
	case errors.Is(err, iofs.ErrExist):
		return proto.ErrFileExists
	default:
		return proto.ErrAccessDenied
	}
}
