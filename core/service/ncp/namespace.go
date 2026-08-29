package ncp

// namespace.go implements the NetWare name-space family — NCP function 0x57 — that
// carries long filenames beyond DOS 8.3: the OS/2 and Macintosh name spaces. It is
// the long-name counterpart of the DOS file calls in fileio.go, reusing the same
// storage seam (Volume.FS()) and the AFP/SMB filename codec + name engine
// (Volume.wireNameFor / decodeWireName), plus the shared case-insensitive
// fold-resolve (fs.ResolveFold) so a mis-cased long name still finds its file on a
// case-sensitive host.
//
// Dispatch quirk (mars_nwe nwconn.c): for function 0x57 the subfunction byte is the
// FIRST request-data byte (body[0]), not after a 2-byte length prefix as for the
// 0x16/0x17 multiplexed functions. The name space the request's path and reply
// name are encoded in is body[1] for most subfunctions.
//
// Reference: mars_nwe src/namspace.c (handle_func_0x57) — CLAUDE.md #7.

import (
	"os"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	ncpproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/ncp"
)

// Name-space ids the service serves (DOS always; OS/2 + Mac added). Local aliases
// of the protocol constants so the handlers read cleanly.
const (
	nsDOS = ncpproto.NameDOS
	nsMAC = ncpproto.NameMAC
	nsNFS = ncpproto.NameNFS
	nsOS2 = ncpproto.NameOS2
)

// loadedNamespaces is the set this server advertises via Get-Name-Spaces-Loaded.
// NFS/FTAM are not served.
var loadedNamespaces = []uint8{nsDOS, nsOS2, nsMAC}

// nameSpace demuxes the function-0x57 name-space family. body[0] is the
// subfunction; the per-subfunction arg layout follows mars_nwe.
func (cn *Conn) nameSpace(body []byte) ([]byte, error) {
	if len(body) < 1 {
		return nil, errFuncNotSupported
	}
	switch body[0] {
	case ncpproto.NSGetLoadedList:
		return cn.nsGetLoaded(body)
	case ncpproto.NSGenDirBase:
		return cn.nsGenDirBase(body)
	case ncpproto.NSInitSearch:
		return cn.nsInitSearch(body)
	case ncpproto.NSSearch:
		return cn.nsSearch(body)
	case ncpproto.NSObtainInfo:
		return cn.nsObtainInfo(body)
	case ncpproto.NSOpenCreate:
		return cn.nsOpenCreate(body)
	default:
		return nil, errFuncNotSupported
	}
}

// nsGetLoaded answers Get Name Spaces Loaded (0x57/0x18): arg is a volume number at
// body[2]; reply is a 2-byte LE count then the loaded name-space id bytes.
func (cn *Conn) nsGetLoaded(body []byte) ([]byte, error) {
	if len(body) < 3 {
		return nil, errFuncNotSupported
	}
	if _, ok := cn.svc.volumeByIndex(int(body[2])); !ok {
		return nil, os.ErrNotExist
	}
	out := []byte{byte(len(loadedNamespaces)), 0x00}
	return append(out, loadedNamespaces...), nil
}

// nsGenDirBase answers Generate Dir Base and Volume Number (0x57/0x16): it resolves
// the request's NW_HPATH to a directory and hands back a 4-byte name-space dir base
// (and a DOS dir base — we use the same value) plus the volume number. The client
// then anchors searches/opens on that base.
func (cn *Conn) nsGenDirBase(body []byte) ([]byte, error) {
	// body: [0]=subfn, [1]=src-ns, [2]=dst-ns, [3]=reserved, [4:]=NW_HPATH.
	if len(body) < 5 {
		return nil, errFuncNotSupported
	}
	ns := body[2]
	vol, store, err := cn.resolveHPath(body[4:], ns)
	if err != nil {
		return nil, err
	}
	if !cn.mayUse(vol) {
		return nil, errAccessDenied
	}
	base := cn.c.AllocBase(vol, store)
	volNum := cn.svc.volumeIndex(vol)
	out := make([]byte, 0, 9)
	out = appendLE32(out, base)     // ns dir base
	out = appendLE32(out, base)     // dos dir base (same)
	out = append(out, byte(volNum)) // volume number
	return out, nil
}

// nsInitSearch answers Initialize Search (0x57/0x02): it resolves the NW_HPATH to a
// directory, allocates a search base bound to it, and returns the 9-byte search
// descriptor (volume, base[4], start-sequence[4]=0xFFFFFFFF "before first").
func (cn *Conn) nsInitSearch(body []byte) ([]byte, error) {
	// body: [0]=subfn, [1]=namespace, [2:]=NW_HPATH.
	if len(body) < 3 {
		return nil, errFuncNotSupported
	}
	ns := body[1]
	vol, store, err := cn.resolveHPath(body[2:], ns)
	if err != nil {
		return nil, err
	}
	if !cn.mayUse(vol) {
		return nil, errAccessDenied
	}
	base := cn.c.AllocBase(vol, store)
	out := make([]byte, 0, 9)
	out = append(out, byte(cn.svc.volumeIndex(vol))) // volume
	out = appendLE32(out, base)                      // search base
	out = appendLE32(out, 0xFFFFFFFF)                // sequence: before-first
	return out, nil
}

// nsSearch answers Search for File or Dir (0x57/0x03): it walks the base's
// directory from the supplied sequence, applies the search attribute (files/dirs),
// and returns the next entry rendered in the request's name space, with only the
// info-mask-selected fields. errNoMoreFiles ends the scan.
func (cn *Conn) nsSearch(body []byte) ([]byte, error) {
	// body offsets (mars_nwe, relative to requestdata = body): searchattrib[2]@2,
	// infomask[4]@4, volume@8, basehandle[4]@9, sequence[4]@13, len@17, pattern@18.
	if len(body) < 18 {
		return nil, errNoMoreFiles
	}
	ns := body[1]
	searchAttrib := leU16(body[2:])
	infomask := leU32(body[4:])
	base := leU32(body[9:])
	sequence := leU32(body[13:])

	dh, ok := cn.c.Base(base)
	if !ok {
		return nil, errBadHandle
	}
	entries, err := dh.volume.FS().ReadDir(dh.path)
	if err != nil {
		return nil, err
	}
	next := int(sequence) + 1
	if sequence == 0xFFFFFFFF {
		next = 0
	}
	// Skip entries that do not match the requested attribute (dirs vs files).
	wantDirs := searchAttrib&0x10 != 0 // ATTR_DIR bit (per the DOS scan attribute)
	for next < len(entries) {
		e := entries[next]
		if e.IsDir() != wantDirs && searchAttrib&0x10 != 0 {
			next++
			continue
		}
		store := joinStore(dh.path, e.Name())
		entry, err := cn.dirEntryInfo(dh.volume, store, e.Name(), e.IsDir(), ns)
		if err != nil {
			next++
			continue
		}
		out := make([]byte, 0, 64)
		out = appendLE32(out, uint32(next)) // next search sequence
		out = entry.MarshalDirInfo(infomask, out)
		return out, nil
	}
	return nil, errNoMoreFiles
}

// nsObtainInfo answers Obtain File or Subdir Info (0x57/0x06): it resolves the
// NW_HPATH and returns the entry's info-mask-selected fields in the request's name
// space.
func (cn *Conn) nsObtainInfo(body []byte) ([]byte, error) {
	// body: [0]=subfn,[1]=src-ns,[2]=dst-ns? mars_nwe: destnamspace@1, searchattrib[2]@2,
	// infomask[4]@4, NW_HPATH@8.
	if len(body) < 8 {
		return nil, errFuncNotSupported
	}
	ns := body[1]
	infomask := leU32(body[4:])
	vol, store, err := cn.resolveHPath(body[8:], ns)
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
	entry, err := cn.dirEntryInfo(vol, store, baseName(store), st.IsDir(), ns)
	if err != nil {
		return nil, err
	}
	return entry.MarshalDirInfo(infomask, nil), nil
}

// nsOpenCreate answers Open/Create File or Subdir (0x57/0x01): it resolves the
// NW_HPATH, opens or creates the file per the mode bits, allocates an open-file
// handle, and returns the 4-byte handle, the action taken, a pad byte, then the
// entry info.
func (cn *Conn) nsOpenCreate(body []byte) ([]byte, error) {
	// body: [0]=subfn,[1]=namespace,[2]=mode? mars_nwe: opencreatmode@1, attrib[2]@2,
	// infomask[4]@4, creatattrib[4]@8, access_rights[2]@12, NW_HPATH@14.
	if len(body) < 14 {
		return nil, errFuncNotSupported
	}
	ns := body[1]
	mode := body[2]
	infomask := leU32(body[4:])
	vol, store, resolved := cn.resolveHPathSoft(body[14:], ns)
	if vol == nil {
		return nil, os.ErrNotExist
	}
	if !cn.mayUse(vol) {
		return nil, errAccessDenied
	}

	wantCreate := mode&ncpproto.OpcModeCreat != 0
	if wantCreate && vol.sh.ReadOnly() {
		return nil, errAccessDenied
	}

	var f fs.File
	var err error
	var action uint8
	switch {
	case !resolved && wantCreate:
		f, err = vol.FS().CreateFile(store)
		action = ncpproto.OpcActionCreat
	case resolved && mode&ncpproto.OpcModeReplace != 0 && wantCreate:
		f, err = vol.FS().CreateFile(store) // truncate-create
		action = ncpproto.OpcActionReplace
	default:
		f, err = vol.FS().OpenFile(store, os.O_RDWR)
		action = ncpproto.OpcActionOpen
	}
	if err != nil {
		return nil, err
	}
	id := cn.c.AllocFile(&openFile{volume: vol, path: store, handle: f})
	cn.svc.pushStats()

	st, _ := f.Stat()
	isDir := st != nil && st.IsDir()
	entry, _ := cn.dirEntryInfo(vol, store, baseName(store), isDir, ns)
	out := make([]byte, 0, 6+len(entry.Name))
	out = appendFileHandle(out, id) // ext_fhandle[2]=0 + fhandle[4]=id (6 bytes)
	out = append(out, action, 0)    // action + reserved pad
	out = entry.MarshalDirInfo(infomask, out)
	return out, nil
}

// --- helpers ---

// dirEntryInfo builds the protocol-neutral entry view for a store path, rendering
// the name in the request's name space and filling the DOS date/time + attribute
// fields from the seam Stat.
func (cn *Conn) dirEntryInfo(vol *Volume, store, leaf string, isDir bool, ns uint8) (ncpproto.DirEntryInfo, error) {
	st, err := vol.FS().Stat(store)
	if err != nil {
		return ncpproto.DirEntryInfo{}, err
	}
	d, t := dosDateTime(st.ModTime())
	attr := uint32(0x20) // archive bit
	if isDir {
		attr = 0x10 // subdirectory
	}
	if vol.sh.ReadOnly() {
		attr |= 0x01 // read-only
	}
	return ncpproto.DirEntryInfo{
		Name:        string(vol.wireNameFor(store, ns)),
		IsDir:       isDir,
		Size:        uint32(st.Size()),
		Attributes:  attr,
		CreateDate:  d,
		CreateTime:  t,
		ModifyDate:  d,
		ModifyTime:  t,
		ArchiveDate: d,
		ArchiveTime: t,
	}, nil
}

// resolveHPath parses an NW_HPATH, anchors it to its base/handle directory, joins
// the path components (decoded from the request name space), folds case
// (fs.ResolveFold) for the case-insensitive name spaces, and returns the volume and
// store path. A component that does not resolve is an error (use resolveHPathSoft
// for the open/create target).
func (cn *Conn) resolveHPath(b []byte, ns uint8) (*Volume, string, error) {
	vol, store, ok := cn.resolveHPathSoft(b, ns)
	if vol == nil {
		return nil, "", errBadHandle
	}
	if !ok {
		return nil, "", os.ErrNotExist
	}
	return vol, store, nil
}

// resolveHPathSoft is resolveHPath but returns ok=false (rather than an error) when
// the leaf does not exist, so the open/create path can create at the requested
// name. vol is nil only when the base/handle anchor is unknown.
func (cn *Conn) resolveHPathSoft(b []byte, ns uint8) (vol *Volume, store string, ok bool) {
	h, _, err := ncpproto.ParseHPath(b)
	if err != nil {
		return nil, "", false
	}
	// Anchor: a 4-byte base, or a 1-byte DOS dir handle (low byte of base).
	var dh *dirHandle
	switch h.Flag {
	case ncpproto.HPathFlagBase:
		dh, ok = cn.c.Base(h.BaseHandle())
	default: // HPathFlagHandle (or none): low byte is a DOS dir handle
		dh, ok = cn.c.Dir(h.Base[0])
	}
	if !ok || dh == nil {
		return nil, "", false
	}
	vol = dh.volume
	// Decode each component from the request name space and join onto the base.
	elems := dh.path
	for _, comp := range h.Components {
		dec, derr := vol.decodeWireName([]byte(comp), ns)
		if derr != nil {
			return vol, "", false
		}
		elems = joinStore(elems, dec)
	}
	// Case-fold for the case-insensitive name spaces (NFS is case-sensitive).
	if ns != nsNFS {
		if resolved, fok := fs.ResolveFold(vol.FS(), elems); fok {
			return vol, resolved, true
		}
		return vol, elems, false
	}
	if _, serr := vol.FS().Stat(elems); serr != nil {
		return vol, elems, false
	}
	return vol, elems, true
}

// dosDateTime converts a Go time to the NetWare/DOS packed date and time words.
func dosDateTime(t time.Time) (date, dtime uint16) {
	y := t.Year()
	if y < 1980 {
		y = 1980
	}
	date = uint16((y-1980)<<9) | uint16(int(t.Month())<<5) | uint16(t.Day())
	dtime = uint16(t.Hour()<<11) | uint16(t.Minute()<<5) | uint16(t.Second()/2)
	return date, dtime
}

func leU16(b []byte) uint16 { return uint16(b[0]) | uint16(b[1])<<8 }
func leU32(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}

// appendLE32 mirrors the protocol package's little-endian appender for the
// name-space reply fields.
func appendLE32(dst []byte, v uint32) []byte {
	return append(dst, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
}
