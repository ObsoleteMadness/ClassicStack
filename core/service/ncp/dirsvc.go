package ncp

// dirsvc.go holds the fnDirServices (0x16) subfunction handlers beyond the
// allocate/deallocate/volume-info trio in fileio.go, plus the small top-level
// housekeeping functions (synchronization locks, TTS, commit, set-attributes).
// Every wire layout follows mars_nwe nwconn.c case 0x16 / connect.c /
// nwvolume.c; the storage side rides the same §9 seam as fileio.go.

import (
	"os"
	"strings"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

// maxVolumeSlots is the NetWare volume-number space: Get Volume Name (0x16/0x06)
// is defined for numbers 0..31. Per mars_nwe nw_get_volume_name, a number inside
// the space with no volume bound answers SUCCESS with an empty name (clients scan
// the whole range building their volume table); only a number outside the space
// is a 0x98 error.
const maxVolumeSlots = 32

// volInfoSecSize is the NetWare sector size volume-usage replies are denominated
// in; the fixed 8-sectors-per-block of the purge/dir-info replies (mars_nwe
// hard-codes 8) makes one block 4096 bytes.
const (
	volInfoSecSize     = 512
	volInfoSecPerBlock = 8
	volInfoBlockSize   = volInfoSecSize * volInfoSecPerBlock
)

// NetWare directory rights mask bits (the 0x16/0x03 effective-rights reply).
const (
	rightRead     uint8 = 0x01
	rightWrite    uint8 = 0x02
	rightOpen     uint8 = 0x04
	rightCreate   uint8 = 0x08
	rightDelete   uint8 = 0x10
	rightParental uint8 = 0x20
	rightSearch   uint8 = 0x40
	rightModify   uint8 = 0x80

	rightsAll      uint8 = 0xFF
	rightsReadOnly       = rightRead | rightOpen | rightSearch
)

// effRights is the effective-rights mask for a volume: everything, or the
// read/open/search subset on a read-only volume.
func effRights(vol *Volume) uint8 {
	if vol.sh.ReadOnly() {
		return rightsReadOnly
	}
	return rightsAll
}

// nwDate encodes a time as the NetWare (DOS) date word: (year-1980)<<9 |
// month<<5 | day (mars_nwe un_date_2_nw); stored big-endian on the NCP wire.
func nwDate(t time.Time) uint16 {
	y := t.Year() - 1980
	if y < 0 {
		y = 0
	}
	return uint16(y)<<9 | uint16(t.Month())<<5 | uint16(t.Day())
}

// nwTime encodes a time as the NetWare (DOS) time word: hour<<11 | minute<<5 |
// second/2 (mars_nwe un_time_2_nw); stored big-endian on the NCP wire.
func nwTime(t time.Time) uint16 {
	return uint16(t.Hour())<<11 | uint16(t.Minute())<<5 | uint16(t.Second()/2)
}

// appendPaddedName appends name upper-cased in a fixed NUL-padded field.
func appendPaddedName(dst []byte, name string, width int) []byte {
	field := make([]byte, width)
	copy(field, strings.ToUpper(name))
	return append(dst, field...)
}

// resolveWire resolves a wire path against a directory handle: a path carrying a
// colon ("VOL:dir") names its volume absolutely (mars_nwe build_path), a bare
// path is relative to the handle's base directory.
func (cn *Conn) resolveWire(handle uint8, wire string) (*Volume, string, error) {
	if strings.Contains(wire, ":") {
		return cn.resolveVolPath(wire)
	}
	dh, ok := cn.c.Dir(handle)
	if !ok {
		return nil, "", errBadHandle
	}
	rel, err := dh.volume.ResolvePath(wire)
	if err != nil {
		return nil, "", err
	}
	return dh.volume, joinStore(dh.path, rel), nil
}

// resolveWireAt reads the length-prefixed wire path at args[at] and resolves it
// against the handle (resolveWire).
func (cn *Conn) resolveWireAt(handle uint8, args []byte, at int) (*Volume, string, error) {
	wire, _, ok := readByteString(args, at)
	if !ok {
		return nil, "", errFuncNotSupported
	}
	return cn.resolveWire(handle, wire)
}

// loginDirName is the well-known directory the connection-init handle points at.
const loginDirName = "LOGIN"

// seedLoginDir binds directory handle 1 to the first volume's LOGIN directory,
// falling back to the volume root when none exists. mars_nwe nw_init_connect
// seeds dirs[0] (handle 1) to volume 0's LOGIN/ the same way — DOS shells use
// handle 1 (SYS:LOGIN, where LOGIN.EXE lives) without ever allocating it, e.g.
// the Get Directory Path(handle 1) a requester issues right after attach.
func (s *Service) seedLoginDir(c *connection) {
	vol, ok := s.volumeByIndex(0)
	if !ok {
		return
	}
	path := ""
	if store, err := vol.ResolvePath(loginDirName); err == nil {
		if st, serr := vol.FS().Stat(store); serr == nil && st.IsDir() {
			path = store
		}
	}
	c.SeedDir(1, vol, path)
}

// --- 0x16 subfunction handlers ---

// setDirHandle answers Set Directory Handle (0x16/0x00): args are target
// handle(1), source handle(1), then a length-prefixed path resolved against the
// source. The target handle is retargeted to the resolved directory (mars_nwe
// nw_set_dir_handle/alter_dir_handle). No reply body.
func (cn *Conn) setDirHandle(args []byte) ([]byte, error) {
	if len(args) < 3 {
		return nil, errBadHandle
	}
	vol, store, err := cn.resolveWireAt(args[1], args, 2)
	if err != nil {
		return nil, err
	}
	if !cn.mayUse(vol) {
		return nil, errAccessDenied
	}
	st, err := vol.FS().Stat(store)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		return nil, os.ErrNotExist
	}
	cn.c.SetDir(args[0], vol, store)
	return nil, nil
}

// getDirPath answers Get Directory Path (0x16/0x01): the arg is a dir handle;
// the reply is a length-prefixed upper-case "VOL:path" with no trailing slash
// (mars_nwe nw_get_directory_path).
func (cn *Conn) getDirPath(args []byte) ([]byte, error) {
	vol, base, err := cn.resolveDir(args)
	if err != nil {
		return nil, err
	}
	path := strings.ToUpper(vol.Name()) + ":" + strings.ToUpper(base)
	path = strings.TrimSuffix(path, "/")
	if len(path) > 255 {
		path = path[:255]
	}
	out := make([]byte, 0, 1+len(path))
	out = append(out, byte(len(path)))
	return append(out, path...), nil
}

// scanDirInfo answers Scan Directory Information (0x16/0x02): args are dir
// handle(1), subdirectory number(2 BE, 1-based, first call 1), then a
// length-prefixed path. The reply is the Nth subdirectory's name[16], create
// date+time(2+2 BE), owner id(4), inherited-rights mask(1), reserved(1), and the
// echoed subdirectory number (mars_nwe nwconn.c case 0x2). Past the last
// subdirectory the answer is 0x9C.
func (cn *Conn) scanDirInfo(args []byte) ([]byte, error) {
	if len(args) < 4 {
		return nil, errBadHandle
	}
	vol, store, err := cn.resolveWireAt(args[0], args, 3)
	if err != nil {
		return nil, err
	}
	if !cn.mayUse(vol) {
		return nil, errAccessDenied
	}
	n := int(beU16(args[1:3]))
	if n == 0 {
		n = 1
	}
	entries, err := vol.FS().ReadDir(store)
	if err != nil {
		return nil, err
	}
	idx := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		idx++
		if idx != n {
			continue
		}
		info, ierr := e.Info()
		if ierr != nil {
			return nil, ierr
		}
		full := joinStore(store, e.Name())
		out := make([]byte, 0, 28)
		out = appendPaddedName(out, vol.ShortName(full), 16)
		out = appendU16(out, nwDate(info.ModTime()))
		out = appendU16(out, nwTime(info.ModTime()))
		out = appendU32(out, 0) // owner id
		out = append(out, effRights(vol), 0)
		out = append(out, args[1], args[2]) // echoed subdirectory number
		return out, nil
	}
	return nil, errNoMoreFiles
}

// getEffDirRights answers Get Effective Directory Rights (0x16/0x03): args are a
// dir handle(1) then a length-prefixed path; the reply is the 1-byte
// effective-rights mask.
func (cn *Conn) getEffDirRights(args []byte) ([]byte, error) {
	if len(args) < 2 {
		return nil, errBadHandle
	}
	vol, store, err := cn.resolveWireAt(args[0], args, 1)
	if err != nil {
		return nil, err
	}
	if !cn.mayUse(vol) {
		return nil, errAccessDenied
	}
	if _, err := vol.FS().Stat(store); err != nil {
		return nil, err
	}
	return []byte{effRights(vol)}, nil
}

// getVolumeNumber answers Get Volume Number (0x16/0x05): the arg is a
// length-prefixed volume name; the reply is the 1-byte volume number. An unknown
// name is 0x98 (mars_nwe nw_get_volume_number).
func (cn *Conn) getVolumeNumber(args []byte) ([]byte, error) {
	name, _, ok := readByteString(args, 0)
	if !ok {
		return nil, errNoSuchVolume
	}
	vol, found := cn.svc.volumeByName(name)
	if !found {
		return nil, errNoSuchVolume
	}
	return []byte{byte(cn.svc.volumeIndex(vol))}, nil
}

// getVolumeName answers Get Volume Name (0x16/0x06): the arg is a volume number
// 0..31; the reply is the length-prefixed upper-case name. A number in range with
// no volume bound answers success with an empty name (mars_nwe
// nw_get_volume_name — clients scan the whole range); out of range is 0x98.
func (cn *Conn) getVolumeName(args []byte) ([]byte, error) {
	if len(args) < 1 {
		return nil, errNoSuchVolume
	}
	n := int(args[0])
	if vol, ok := cn.svc.volumeByIndex(n); ok {
		name := strings.ToUpper(vol.Name())
		if len(name) > 16 {
			name = name[:16]
		}
		return append([]byte{byte(len(name))}, name...), nil
	}
	if n < maxVolumeSlots {
		return []byte{0}, nil // empty slot: success, empty name
	}
	return nil, errNoSuchVolume
}

// createDir answers Create Directory (0x16/0x0A): args are dir handle(1), then a
// length-prefixed path, then a trailing access-rights mask(1, not stored). The path
// PRECEDES the rights byte (Novell "Create Directory" wire order, mars_nwe
// nw_creat_dir); an earlier version read the path at a fixed offset 2 as though rights
// came first, which failed every real client's frame with 0xFB (errFuncNotSupported).
func (cn *Conn) createDir(args []byte) ([]byte, error) {
	if len(args) < 3 {
		return nil, errBadHandle
	}
	vol, store, err := cn.resolveWireAt(args[0], args, 1)
	if err != nil {
		return nil, err
	}
	if !cn.mayUse(vol) || vol.sh.ReadOnly() {
		return nil, errAccessDenied
	}
	if err := vol.FS().CreateDir(store); err != nil {
		return nil, err
	}
	return nil, nil
}

// deleteDir answers Delete Directory (0x16/0x0B): args are dir handle(1), a
// reserved byte, then a length-prefixed path naming an (empty) directory.
func (cn *Conn) deleteDir(args []byte) ([]byte, error) {
	if len(args) < 3 {
		return nil, errBadHandle
	}
	vol, store, err := cn.resolveWireAt(args[0], args, 2)
	if err != nil {
		return nil, err
	}
	if !cn.mayUse(vol) || vol.sh.ReadOnly() {
		return nil, errAccessDenied
	}
	st, err := vol.FS().Stat(store)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		return nil, os.ErrNotExist
	}
	if err := vol.FS().Remove(store); err != nil {
		return nil, err
	}
	_ = vol.FS().DeleteMetadata(store)
	return nil, nil
}

// renameDir answers Rename Directory (0x16/0x0F): args are dir handle(1), a
// length-prefixed old path, then the length-prefixed new name — the directory is
// renamed in place under its parent (mars_nwe nw_mv_dir). No reply body.
func (cn *Conn) renameDir(args []byte) ([]byte, error) {
	vol, oldStore, p, err := cn.resolveHandlePathAt(args, 0)
	if err != nil {
		return nil, err
	}
	newName, _, ok := readByteString(args, p)
	if !ok {
		return nil, errFuncNotSupported
	}
	if !cn.mayUse(vol) || vol.sh.ReadOnly() {
		return nil, errAccessDenied
	}
	newLeaf, err := vol.ResolvePath(newName)
	if err != nil {
		return nil, err
	}
	parent := ""
	if i := strings.LastIndexByte(oldStore, '/'); i >= 0 {
		parent = oldStore[:i]
	}
	newStore := joinStore(parent, newLeaf)
	if err := vol.FS().Rename(oldStore, newStore); err != nil {
		return nil, err
	}
	_ = vol.FS().MoveMetadata(oldStore, newStore)
	return nil, nil
}

// setDirInfo answers Set Directory Information (0x16/0x19): args are dir
// handle(1), creation date(2)+time(2), owner id(4), new rights mask(1), then a
// length-prefixed path. The target is validated; the DOS directory metadata
// itself is accepted and discarded — the §9 seam stores none — so FILER-style
// flows complete instead of aborting. No reply body.
func (cn *Conn) setDirInfo(args []byte) ([]byte, error) {
	vol, store, _, err := cn.resolveHandlePathAt(args, 0, 9)
	if err != nil {
		return nil, err
	}
	if !cn.mayUse(vol) || vol.sh.ReadOnly() {
		return nil, errAccessDenied
	}
	if _, err := vol.FS().Stat(store); err != nil {
		return nil, err
	}
	return nil, nil
}

// scanVolRestrictions answers Scan Volume User Disk Restrictions (0x16/0x20):
// args are volume number(1) and a 4-byte sequence. There are no per-user disk
// restrictions, so the reply is a zero entry count (mars_nwe answers the same).
func (cn *Conn) scanVolRestrictions(args []byte) ([]byte, error) {
	if len(args) < 1 || int(args[0]) >= maxVolumeSlots {
		return nil, errNoSuchVolume
	}
	return []byte{0x00}, nil
}

// getVolPurgeInfo answers Get Volume and Purge Information (0x16/0x2C, NetWare
// 3.11+; ncpfs depends on it). The arg is a volume number; the reply is — all
// 32-bit fields LITTLE-endian (mars_nwe U32_TO_32) — total blocks, available
// blocks, purgeable blocks(0), not-yet-purgeable blocks(0), total dir entries,
// available dir entries, reserved(4), sectors-per-block(1, always 8), then the
// length-prefixed volume name.
func (cn *Conn) getVolPurgeInfo(args []byte) ([]byte, error) {
	if len(args) < 1 {
		return nil, errNoSuchVolume
	}
	vol, ok := cn.svc.volumeByIndex(int(args[0]))
	if !ok {
		return nil, errNoSuchVolume
	}
	return volUsageReply(vol, true)
}

// getDirInfo answers Get Directory Information (0x16/0x2D): the arg is a dir
// handle; the reply is the handle's volume usage — the 0x2C shape without the
// two purgeable-blocks fields.
func (cn *Conn) getDirInfo(args []byte) ([]byte, error) {
	vol, _, err := cn.resolveDir(args)
	if err != nil {
		return nil, err
	}
	return volUsageReply(vol, false)
}

// volUsageReply builds the shared 0x2C/0x2D volume-usage body. withPurge selects
// the 0x2C shape (purgeable + not-yet-purgeable block fields present).
func volUsageReply(vol *Volume, withPurge bool) ([]byte, error) {
	total, free, err := vol.FS().DiskUsage("")
	if err != nil {
		return nil, err
	}
	// Directory-entry slots are not tracked; report an ample fixed pool.
	const dirSlots = 0xFFFF
	name := strings.ToUpper(vol.Name())
	if len(name) > 16 {
		name = name[:16]
	}
	out := make([]byte, 0, 30+len(name))
	out = appendLE32(out, uint32(total/volInfoBlockSize))
	out = appendLE32(out, uint32(free/volInfoBlockSize))
	if withPurge {
		out = appendLE32(out, 0) // purgeable blocks
		out = appendLE32(out, 0) // not-yet-purgeable blocks
	}
	out = appendLE32(out, dirSlots)
	out = appendLE32(out, dirSlots)
	out = appendLE32(out, 0) // reserved by Novell
	out = append(out, volInfoSecPerBlock)
	out = append(out, byte(len(name)))
	return append(out, name...), nil
}

// --- top-level housekeeping functions ---

// getVolumeInfoWithNumber answers Get Volume Info with Number (0x12): the arg is
// a volume number; the body is the same shape Get Volume Info with Handle
// (0x16/0x15) answers.
func (cn *Conn) getVolumeInfoWithNumber(body []byte) ([]byte, error) {
	if len(body) < 1 {
		return nil, errNoSuchVolume
	}
	vol, ok := cn.svc.volumeByIndex(int(body[0]))
	if !ok {
		return nil, errNoSuchVolume
	}
	return volumeInfoReply(vol)
}

// grantLock acknowledges the synchronization family (log/lock/release/clear for
// files, logical records, and physical byte ranges — functions 0x03..0x0E,
// 0x1A/0x1E/0x1F). ClassicStack keeps no cross-connection lock manager, so every
// log/lock is granted and every release/clear succeeds — the compatibility
// posture the charter picks over strict semantics (a lone vintage client never
// contends with itself). No reply body.
func (cn *Conn) grantLock() ([]byte, error) {
	return nil, nil
}

// ttsCall answers the Transaction Tracking System family (0x22): subfunction 0
// ("TTS is available?") succeeds — meaning no transaction tracking — and every
// other TTS verb is unsupported, exactly mars_nwe's behaviour.
func (cn *Conn) ttsCall(body []byte) ([]byte, error) {
	if len(body) > 0 && body[0] == 0x00 {
		return nil, nil
	}
	return nil, errFuncNotSupported
}

// commitFile answers Commit File (0x3B, and the older 0x3D form): args are a
// reserved byte then the 6-byte file handle; the open file is flushed to disk.
func (cn *Conn) commitFile(body []byte) ([]byte, error) {
	of, ok := cn.fileFor(body)
	if !ok {
		return nil, errBadHandle
	}
	f, ok := of.handle.(fs.File)
	if !ok {
		return nil, errBadHandle
	}
	if err := f.Sync(); err != nil {
		return nil, err
	}
	return nil, nil
}

// setFileAttributes answers Set File Attributes (0x46): args are the new
// attribute byte, a dir handle, a search-attribute byte, then the
// length-prefixed name. The target is validated; the DOS attribute bits are
// accepted and discarded (the §9 seam stores no DOS attributes) so COPY/FLAG
// flows complete. No reply body.
func (cn *Conn) setFileAttributes(body []byte) ([]byte, error) {
	vol, store, _, err := cn.resolveHandlePathAt(body, 1, 1)
	if err != nil {
		return nil, err
	}
	if !cn.mayUse(vol) || vol.sh.ReadOnly() {
		return nil, errAccessDenied
	}
	if _, err := vol.FS().Stat(store); err != nil {
		return nil, err
	}
	return nil, nil
}
