package ncp

// fileio.go holds the volume, directory-handle, file, and directory-scan handlers.
// Every path operation resolves a NetWare wire path through the bound Volume's
// codec (Volume.ResolvePath) and acts via Volume.FS() — the §9 storage seam — so
// the engine holds no storage-layout knowledge. Erase/Rename ride the
// metadata-carrying FS().Remove/Rename + DeleteMetadata/MoveMetadata pairing the
// seam documents.
//
// Directory handles: a NetWare client allocates a handle bound to a volume + base
// directory, then issues file operations with a handle + relative path. Our open/
// search functions take (dirHandle, path); the effective store path is the
// handle's base joined with the relative path, both resolved through the codec.

import (
	"io"
	"os"
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

// --- volume / dir-handle services (0x16 subfunctions) ---

// getVolumeInfo answers Get Volume Info with Handle (0x16/0x15). Per mars_nwe the
// reply XDATA is, all big-endian u16 unless noted: sectors-per-block(2),
// total_blocks(2), avail_blocks(2), total_dirs(2), avail_dirs(2), name[16],
// removable(2). The arg is the subfunction's first byte = a dir handle whose
// volume we report. We scale blocks so totals fit the 16-bit fields (mars_nwe's
// sector_scale loop).
func (cn *Conn) getVolumeInfo(args []byte) ([]byte, error) {
	vol, _, err := cn.resolveDir(args)
	if err != nil {
		return nil, err
	}
	total, free, err := vol.FS().DiskUsage("")
	if err != nil {
		return nil, err
	}
	const blockSize = 4096
	totalBlocks := total / blockSize
	availBlocks := free / blockSize
	// Scale so block counts fit the 16-bit fields (mars_nwe increments by 2).
	scale := uint64(1)
	for totalBlocks/scale > 0xFFFF {
		scale += 2
	}
	out := make([]byte, 0, 28)
	out = appendU16(out, uint16(scale))             // sectors per block
	out = appendU16(out, uint16(totalBlocks/scale)) // total blocks
	out = appendU16(out, uint16(availBlocks/scale)) // available blocks
	out = appendU16(out, 0xFFFF)                    // total directory slots
	out = appendU16(out, 0xFFFF)                    // available directory slots
	var nameField [16]byte
	copy(nameField[:], vol.Name())
	out = append(out, nameField[:]...)
	out = appendU16(out, 0) // removable flag
	return out, nil
}

// allocDirHandle answers Allocate Directory Handle (0x16 subfunctions 0x12 perm /
// 0x13 temp / 0x16 special-temp). Per mars_nwe the subfunction args are: source
// dir-handle, then two bytes, then the path that names the volume (VOL:dir/...).
// Reply is the new dir-handle byte and an 8-bit effective-rights mask. The new
// handle is bound to the resolved volume + base directory.
func (cn *Conn) allocDirHandle(args []byte) ([]byte, error) {
	// args[0] = source handle, args[1]/args[2] = drive/flags, args[3:] = path.
	if len(args) < 4 {
		return nil, errFuncNotSupported
	}
	path := strings.TrimRight(string(args[3:]), "\x00")
	vol, base, err := cn.resolveVolPath(path)
	if err != nil {
		return nil, err
	}
	if !cn.mayUse(vol) {
		return nil, errAccessDenied
	}
	id := cn.c.AllocDir(vol, base)
	// Reply: new handle, then an 8-bit effective-rights mask (0xFF = all rights).
	return []byte{id, 0xFF}, nil
}

// deallocDirHandle answers Deallocate Directory Handle (0x16/0x14): the
// subfunction's first arg byte is the handle. No reply body.
func (cn *Conn) deallocDirHandle(args []byte) ([]byte, error) {
	if len(args) < 1 {
		return nil, errFuncNotSupported
	}
	cn.c.FreeDir(args[0])
	return nil, nil
}

// --- file services ---

// openFile handles open (0x4C/0x41) and create (0x43/0x4D). Per mars_nwe the args
// are: dir-handle, attribute byte, [access byte — open 0x4C only], name-length,
// name. It opens (or creates) the file via the seam, allocates an open-file handle,
// and replies with the NetWare open reply: ext_fhandle[2]=0, fhandle[4] (our slot
// id), reserved[2]=0, then a 14-byte name and the 4-byte size — the prefix the
// client echoes as its 6-byte file handle on read/write/close.
func (cn *Conn) openFile(args []byte, create bool) ([]byte, error) {
	// create (0x43/0x4D): dirhandle, attribute, len, name → 1 skip byte.
	// open   (0x4C):       dirhandle, attrib, access, len, name → 2 skip bytes.
	skip := 2
	if create {
		skip = 1
	}
	vol, store, err := cn.resolveHandlePath(args, skip)
	if err != nil {
		return nil, err
	}
	if !cn.mayUse(vol) {
		return nil, errAccessDenied
	}
	if create && vol.sh.ReadOnly() {
		return nil, errAccessDenied
	}
	var f fs.File
	if create {
		f, err = vol.FS().CreateFile(store)
	} else {
		f, err = vol.FS().OpenFile(store, os.O_RDWR)
	}
	if err != nil {
		return nil, err
	}
	id := cn.c.AllocFile(&openFile{volume: vol, path: store, handle: f})
	cn.svc.pushStats()

	var size int64
	if st, serr := f.Stat(); serr == nil {
		size = st.Size()
	}
	out := make([]byte, 0, 6+2+14+4)
	out = appendFileHandle(out, id) // ext_fhandle[2]=0 + fhandle[4]=id
	out = append(out, 0, 0)         // reserved[2]
	out = vol.appendFileName(out, store)
	out = appendU32(out, uint32(size))
	return out, nil
}

// closeFile handles fnCloseFile (0x42). Per mars_nwe the args are reserve(1),
// ext_fhandle[2], fhandle[4]; the slot id is in fhandle. It closes the seam handle
// and frees the slot.
func (cn *Conn) closeFile(args []byte) ([]byte, error) {
	id, ok := parseFileHandle(args)
	if !ok {
		return nil, errBadHandle
	}
	of, ok := cn.c.FreeFile(id)
	if !ok {
		return nil, errBadHandle
	}
	if f, ok := of.handle.(fs.File); ok {
		_ = f.Close()
	}
	cn.svc.pushStats()
	return nil, nil
}

// readFile handles fnReadFile (0x48). Per mars_nwe the args are filler(1),
// ext_fhandle[2], fhandle[4], offset[4 BE], max_size[2 BE]. Reply is size[2 BE]
// then the data, with a leading pad byte inserted when the read offset is odd
// (mars_nwe's `zusatz`), and the total reply length is size+zusatz+2.
func (cn *Conn) readFile(args []byte) ([]byte, error) {
	const hdr = 1 + 2 + 4 // filler + ext_fhandle + fhandle
	if len(args) < hdr+4+2 {
		return nil, errBadHandle
	}
	of, ok := cn.fileFor(args)
	if !ok {
		return nil, errBadHandle
	}
	off := int64(beU32(args[hdr:]))
	want := int(beU16(args[hdr+4:]))
	f, ok := of.handle.(fs.File)
	if !ok {
		return nil, errBadHandle
	}
	buf := make([]byte, want)
	n, err := f.ReadAt(buf, off)
	if err != nil && err != io.EOF {
		return nil, err
	}
	pad := 0
	if off&1 == 1 {
		pad = 1 // NetWare aligns the data to an even file offset
	}
	out := make([]byte, 0, 2+pad+n)
	out = appendU16(out, uint16(n))
	for i := 0; i < pad; i++ {
		out = append(out, 0)
	}
	out = append(out, buf[:n]...)
	return out, nil
}

// writeFile handles fnWriteFile (0x49). Per mars_nwe the args are filler(1),
// ext_handle[2], fhandle[4], offset[4 BE], size[2 BE], data. No reply body.
func (cn *Conn) writeFile(args []byte) ([]byte, error) {
	const hdr = 1 + 2 + 4
	if len(args) < hdr+4+2 {
		return nil, errBadHandle
	}
	of, ok := cn.fileFor(args)
	if !ok {
		return nil, errBadHandle
	}
	if of.volume.sh.ReadOnly() {
		return nil, errAccessDenied
	}
	off := int64(beU32(args[hdr:]))
	n := int(beU16(args[hdr+4:]))
	data := args[hdr+6:]
	if n > len(data) {
		n = len(data)
	}
	f, ok := of.handle.(fs.File)
	if !ok {
		return nil, errBadHandle
	}
	if _, err := f.WriteAt(data[:n], off); err != nil {
		return nil, err
	}
	return nil, nil
}

// getFileSize handles fnGetFileSize (0x47). Per mars_nwe the args are filler(1),
// ext_filehandle[2], fhandle[4]; reply is a 4-byte BE size.
func (cn *Conn) getFileSize(args []byte) ([]byte, error) {
	of, ok := cn.fileFor(args)
	if !ok {
		return nil, errBadHandle
	}
	f, ok := of.handle.(fs.File)
	if !ok {
		return nil, errBadHandle
	}
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	return appendU32(nil, uint32(st.Size())), nil
}

// eraseFile handles fnEraseFile (0x44): a dir-handle byte, an attribute byte, and a
// length-prefixed path. It removes the file via the seam (which carries the fork
// metadata).
func (cn *Conn) eraseFile(args []byte) ([]byte, error) {
	vol, store, err := cn.resolveHandlePath(args, 1)
	if err != nil {
		return nil, err
	}
	if !cn.mayUse(vol) {
		return nil, errAccessDenied
	}
	if vol.sh.ReadOnly() {
		return nil, errAccessDenied
	}
	if err := vol.FS().Remove(store); err != nil {
		return nil, err
	}
	_ = vol.FS().DeleteMetadata(store)
	return nil, nil
}

// renameFile handles fnRenameFile (0x45): a source dir-handle + length-prefixed
// path, then a destination dir-handle + length-prefixed path. Both resolve through
// the seam; the rename carries fork metadata. (Function 0x46 is set-attributes, a
// different call we do not implement.)
func (cn *Conn) renameFile(args []byte) ([]byte, error) {
	srcVol, srcPath, p, err := cn.resolveHandlePathAt(args, 0)
	if err != nil {
		return nil, err
	}
	dstVol, dstPath, _, err := cn.resolveHandlePathAt(args, p)
	if err != nil {
		return nil, err
	}
	if srcVol != dstVol {
		return nil, errAccessDenied // cross-volume rename unsupported
	}
	if !cn.mayUse(srcVol) || srcVol.sh.ReadOnly() {
		return nil, errAccessDenied
	}
	if err := srcVol.FS().Rename(srcPath, dstPath); err != nil {
		return nil, err
	}
	_ = srcVol.FS().MoveMetadata(srcPath, dstPath)
	return nil, nil
}

// --- directory scan (0x3E / 0x3F) ---

// searchInit handles fnFileSearchInit (0x3E). Per mars_nwe the args are a
// dir-handle byte then a length-prefixed path; the reply is volume(1), dir_id[2],
// searchsequence[2], dir_rights(1). We reuse our dir-handle slot id as the dir_id
// and start the search sequence at 0xFFFF ("before first"), so searchContinue is
// stateless across calls.
func (cn *Conn) searchInit(args []byte) ([]byte, error) {
	if len(args) < 1 {
		return nil, errFuncNotSupported
	}
	dirHandle := args[0]
	rel, _, ok := readByteString(args, 1)
	if !ok {
		return nil, errFuncNotSupported
	}
	dh, ok := cn.c.Dir(dirHandle)
	if !ok {
		return nil, errBadHandle
	}
	relStore, err := dh.volume.ResolvePath(rel)
	if err != nil {
		return nil, err
	}
	store := joinStore(dh.path, relStore)
	vol := dh.volume
	if !cn.mayUse(vol) {
		return nil, errAccessDenied
	}
	if _, err := vol.FS().Stat(store); err != nil {
		return nil, err
	}
	// Bind a fresh dir handle to the resolved directory; its id is the dir_id we
	// hand back and the client echoes on searchContinue.
	dirID := cn.c.AllocDir(vol, store)
	out := make([]byte, 0, 6)
	out = append(out, 0)          // volume number (single-volume reply)
	out = append(out, 0, dirID)   // dir_id[2] (BE; low byte = our slot)
	out = append(out, 0xFF, 0xFF) // searchsequence = before-first
	out = append(out, 0xFF)       // dir_rights (all)
	return out, nil
}

// searchContinue handles fnFileSearchContinue (0x3F). Per mars_nwe the args are
// volume(1), dir_id[2], searchsequence[2], search_attrib(1), len(1), pattern. The
// reply is searchsequence[2], dir_id[2], then the entry info. We return the next
// matching entry or errNoMoreFiles at the end of the directory.
func (cn *Conn) searchContinue(args []byte) ([]byte, error) {
	if len(args) < 5 {
		return nil, errNoMoreFiles
	}
	dirID := args[2] // low byte of dir_id[1..2]
	last := int(beU16(args[3:]))
	dh, ok := cn.c.Dir(dirID)
	if !ok {
		return nil, errBadHandle
	}
	entries, err := dh.volume.FS().ReadDir(dh.path)
	if err != nil {
		return nil, err
	}
	next := last + 1
	if last == 0xFFFF {
		next = 0
	}
	if next >= len(entries) {
		return nil, errNoMoreFiles
	}
	e := entries[next]
	info, err := e.Info()
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, 32)
	// Reply: next searchsequence[2], echoed dir_id[2], then name + attr + size.
	out = append(out, byte(next>>8), byte(next))
	out = append(out, args[1], args[2]) // echo dir_id
	fullStore := e.Name()
	if dh.path != "" {
		fullStore = dh.path + "/" + e.Name()
	}
	out = dh.volume.appendFileName(out, fullStore)
	var size uint32
	attr := byte(0x00)
	if e.IsDir() {
		attr = 0x10 // subdirectory attribute
	} else {
		size = uint32(info.Size())
	}
	out = append(out, attr)
	out = appendU32(out, size)
	return out, nil
}

// --- path / handle resolution helpers ---

// mayUse reports whether the connection's identity may use the volume per its
// allow-list. A guest connection (not logged in) passes only for a world-open
// volume.
func (cn *Conn) mayUse(vol *Volume) bool {
	cn.c.mu.Lock()
	user := cn.c.user
	cn.c.mu.Unlock()
	return vol.allows(user)
}

// resolveDir reads a leading dir-handle byte and returns its bound volume and base
// store path.
func (cn *Conn) resolveDir(args []byte) (*Volume, string, error) {
	if len(args) < 1 {
		return nil, "", errBadHandle
	}
	dh, ok := cn.c.Dir(args[0])
	if !ok {
		return nil, "", errBadHandle
	}
	return dh.volume, dh.path, nil
}

// resolveVolPath resolves a VOL:dir/... wire path to its volume and base store
// path (no dir handle involved — used by allocDirHandle).
func (cn *Conn) resolveVolPath(wire string) (*Volume, string, error) {
	volName := wire
	if before, _, found := strings.Cut(wire, ":"); found {
		volName = before
	}
	vol, ok := cn.svc.volumeByName(volName)
	if !ok {
		return nil, "", os.ErrNotExist
	}
	store, err := vol.ResolvePath(wire)
	if err != nil {
		return nil, "", err
	}
	return vol, store, nil
}

// resolveHandlePath reads a dir-handle byte at args[0], skips `skip` extra leading
// bytes (attribute/search bytes vary per function), reads the length-prefixed
// relative path, and joins it onto the handle's base — returning the volume and the
// effective store path.
func (cn *Conn) resolveHandlePath(args []byte, skip int) (*Volume, string, error) {
	vol, store, _, err := cn.resolveHandlePathAt(args, 0, skip)
	return vol, store, err
}

// resolveHandlePathAt is resolveHandlePath starting at offset `at`, returning the
// offset past the consumed path so a two-path function (rename) can chain. The
// optional variadic `skip` is the count of bytes between the handle byte and the
// length-prefixed path (default 0).
func (cn *Conn) resolveHandlePathAt(args []byte, at int, skip ...int) (*Volume, string, int, error) {
	sk := 0
	if len(skip) > 0 {
		sk = skip[0]
	}
	if at >= len(args) {
		return nil, "", at, errBadHandle
	}
	dh, ok := cn.c.Dir(args[at])
	if !ok {
		return nil, "", at, errBadHandle
	}
	rel, p, ok := readByteString(args, at+1+sk)
	if !ok {
		return nil, "", at, errFuncNotSupported
	}
	relStore, err := dh.volume.ResolvePath(rel)
	if err != nil {
		return nil, "", p, err
	}
	store := joinStore(dh.path, relStore)
	return dh.volume, store, p, nil
}

// joinStore joins a base store path and a relative store path with the seam's '/'
// separator, dropping empty components.
func joinStore(base, rel string) string {
	switch {
	case base == "":
		return rel
	case rel == "":
		return base
	default:
		return base + "/" + rel
	}
}

// fileFor returns the open file named by the 6-byte handle at the head of args.
func (cn *Conn) fileFor(args []byte) (*openFile, bool) {
	id, ok := parseFileHandle(args)
	if !ok {
		return nil, false
	}
	return cn.c.File(id)
}

// --- wire helpers ---

func beU16(b []byte) uint16 { return uint16(b[0])<<8 | uint16(b[1]) }
func beU32(b []byte) uint32 {
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}

// appendFileHandle writes the NetWare open-reply file-handle prefix: ext_fhandle[2]
// (always zero) followed by fhandle[4] carrying our 16-bit slot id in its low two
// bytes. The client treats the 6-byte prefix as opaque and echoes it (preceded by a
// filler byte) on read/write/close.
func appendFileHandle(dst []byte, id uint16) []byte {
	return append(dst, 0, 0 /*ext_fhandle*/, 0, 0, byte(id>>8), byte(id) /*fhandle*/)
}

// parseFileHandle reads the slot id from a read/write/close/getsize request. Per
// mars_nwe those carry filler(1), ext_fhandle[2], fhandle[4]; our slot id lives in
// the low two bytes of the 4-byte fhandle (offset 5..6 from the start).
func parseFileHandle(b []byte) (uint16, bool) {
	if len(b) < 7 {
		return 0, false
	}
	return uint16(b[5])<<8 | uint16(b[6]), true
}

// appendFileName writes a fixed 14-byte NetWare name field (8.3, NUL-padded) for
// the store path, deriving a unique uppercase 8.3 short name through the volume's
// NameEngine — so a long host name maps to a stable "NAME~1"-style 8.3 that
// reverses back to the same host file, rather than a raw truncation that would
// collide. A volume with no name engine (or a derivation error) falls back to the
// uppercased leaf.
func (v *Volume) appendFileName(dst []byte, store string) []byte {
	name := shortLeaf(store)
	if sn, err := v.FS().ShortName(store); err == nil && sn != "" {
		name = shortLeaf(sn)
	}
	var field [14]byte
	copy(field[:], strings.ToUpper(name))
	return append(dst, field[:]...)
}

// shortLeaf returns the final '/'-separated element of a store path.
func shortLeaf(store string) string {
	if i := strings.LastIndexByte(store, '/'); i >= 0 {
		return store[i+1:]
	}
	return store
}
