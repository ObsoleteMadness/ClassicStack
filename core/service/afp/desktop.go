package afp

import (
	"sync"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"
)

// Desktop Database (Inside Macintosh: Networking, AFP 2.x §C "The Desktop
// database"): the Finder-facing store of file/folder comments, application icons,
// and APPL (creator → application) mappings a volume keeps so the Finder can draw
// icons and resolve "open with" without re-scanning the disk.
//
// The spine keeps the seam honest by splitting the database in two:
//
//   - Comments ride the §9 fork seam — v.FS().ReadComment/WriteComment — so a
//     comment lives in the same metadata container (AppleDouble sidecar, NTFS
//     stream, or Netatalk EA) as the file it annotates and survives a rename
//     through the FS, exactly like Finder info. The spine holds no layout
//     knowledge here.
//   - Icons and APPL mappings have no per-file home in the seam; they are
//     volume-scoped catalog state. They live in a per-volume in-memory desktopDB
//     (below). This mirrors how the mem metastore stands in until the sqlite/
//     adapter wiring lands — the persistence backend is an adapter concern, not a
//     spine concern, and the in-memory form keeps core free of database/path
//     knowledge.
//
// FPOpenDT hands the client a Desktop reference number (DTRefNum) that maps back
// to a volume; every later Desktop command carries it. FPAddIcon is the lone
// command that arrives over the two-phase ASPWrite path (it carries the bitmap as
// bulk write data, like FPWrite); see write.go / forkio.go.

// AFP Desktop command codes (Inside Macintosh: Networking, AFP 2.x §C). FPAddIcon
// is 192 because the Mac delivers it via ASPUserWrite (the two-phase write path),
// not ASPCommand — the icon bitmap is bulk write data.
const (
	cmdOpenDT        uint8 = 48  // FPOpenDT
	cmdCloseDT       uint8 = 49  // FPCloseDT
	cmdGetIcon       uint8 = 51  // FPGetIcon
	cmdGetIconInfo   uint8 = 52  // FPGetIconInfo
	cmdAddAPPL       uint8 = 53  // FPAddAPPL
	cmdRemoveAPPL    uint8 = 54  // FPRemoveAPPL
	cmdGetAPPL       uint8 = 55  // FPGetAPPL
	cmdAddComment    uint8 = 56  // FPAddComment
	cmdRemoveComment uint8 = 57  // FPRemoveComment
	cmdGetComment    uint8 = 58  // FPGetComment
	cmdAddIcon       uint8 = 192 // FPAddIcon (arrives via ASPUserWrite)
)

// Desktop result codes (kFP*; Inside Macintosh: Networking, "AFP result codes")
// used only by the Desktop commands.
const (
	afpErrItemNotFound  int32 = -5012 // kFPItemNotFound (no such icon)
	afpErrIconTypeError int32 = -5030 // kFPIconTypeError (replacement icon size differs)
)

// maxCommentLen is the AFP Finder-comment cap: comments are stored and returned
// truncated to 199 bytes (Inside Macintosh: Networking, "AddComment").
const maxCommentLen = 199

// iconEntry is one stored icon bitmap plus its 4-byte Finder tag.
type iconEntry struct {
	tag    uint32
	bitmap []byte
}

// iconKey identifies an icon by its (creator, file type, icon type) triple — the
// key FPGetIcon looks up and FPAddIcon writes.
type iconKey struct {
	creator  [4]byte
	fileType [4]byte
	iconType uint8
}

// applEntry is one APPL mapping: the tag the Finder stored plus the dirID +
// pathname locating the application file, so FPGetAPPL can resolve it.
type applEntry struct {
	tag      uint32
	dirID    uint32
	pathname string
}

// desktopDB is a volume's in-memory Desktop database for icons and APPL
// mappings. Comments are NOT held here — they ride the fork seam (see file doc).
// Insertion order is preserved per creator so FPGetIconInfo / FPGetAPPL can index
// by position the way the Finder expects.
type desktopDB struct {
	mu        sync.Mutex
	icons     map[iconKey]iconEntry
	iconOrder map[[4]byte][]iconKey   // creator → icon keys in insertion order
	appls     map[[4]byte][]applEntry // creator → APPL entries in insertion order
}

func newDesktopDB() *desktopDB {
	return &desktopDB{
		icons:     make(map[iconKey]iconEntry),
		iconOrder: make(map[[4]byte][]iconKey),
		appls:     make(map[[4]byte][]applEntry),
	}
}

// setIcon stores (or replaces) an icon. Replacing an existing icon whose bitmap
// is a different size is rejected with kFPIconTypeError, matching the AFP rule
// that an icon slot's size is fixed once created.
func (db *desktopDB) setIcon(creator, fileType [4]byte, iconType uint8, tag uint32, bitmap []byte) int32 {
	db.mu.Lock()
	defer db.mu.Unlock()
	k := iconKey{creator: creator, fileType: fileType, iconType: iconType}
	if existing, ok := db.icons[k]; ok {
		if len(existing.bitmap) != len(bitmap) {
			return afpErrIconTypeError
		}
	} else {
		db.iconOrder[creator] = append(db.iconOrder[creator], k)
	}
	db.icons[k] = iconEntry{tag: tag, bitmap: append([]byte(nil), bitmap...)}
	return afpNoErr
}

// getIcon returns the icon bitmap for a triple.
func (db *desktopDB) getIcon(creator, fileType [4]byte, iconType uint8) (iconEntry, bool) {
	db.mu.Lock()
	defer db.mu.Unlock()
	e, ok := db.icons[iconKey{creator: creator, fileType: fileType, iconType: iconType}]
	return e, ok
}

// iconInfoByIndex returns the index-th (1-based) icon registered for a creator,
// with the file/icon type from its key, for FPGetIconInfo.
func (db *desktopDB) iconInfoByIndex(creator [4]byte, index uint16) (iconEntry, [4]byte, uint8, bool) {
	db.mu.Lock()
	defer db.mu.Unlock()
	order := db.iconOrder[creator]
	if index == 0 || int(index) > len(order) {
		return iconEntry{}, [4]byte{}, 0, false
	}
	k := order[index-1]
	return db.icons[k], k.fileType, k.iconType, true
}

// addAPPL registers (or updates the tag of) an APPL mapping for a creator.
func (db *desktopDB) addAPPL(creator [4]byte, tag, dirID uint32, pathname string) {
	db.mu.Lock()
	defer db.mu.Unlock()
	entries := db.appls[creator]
	for i, e := range entries {
		if e.dirID == dirID && e.pathname == pathname {
			entries[i].tag = tag
			return
		}
	}
	db.appls[creator] = append(entries, applEntry{tag: tag, dirID: dirID, pathname: pathname})
}

// removeAPPL drops an APPL mapping. A mapping that is not present is a no-op.
func (db *desktopDB) removeAPPL(creator [4]byte, dirID uint32, pathname string) {
	db.mu.Lock()
	defer db.mu.Unlock()
	entries := db.appls[creator]
	for i, e := range entries {
		if e.dirID == dirID && e.pathname == pathname {
			db.appls[creator] = append(entries[:i], entries[i+1:]...)
			return
		}
	}
}

// applByIndex returns the index-th (0-based, the AFP convention for FPGetAPPL)
// APPL entry for a creator.
func (db *desktopDB) applByIndex(creator [4]byte, index uint16) (applEntry, bool) {
	db.mu.Lock()
	defer db.mu.Unlock()
	entries := db.appls[creator]
	if int(index) >= len(entries) {
		return applEntry{}, false
	}
	return entries[index], true
}

// --- per-session DTRefNum table -------------------------------------------

// dtRef is the resolved target of a Desktop reference number: the volume whose
// Desktop database it names.
type dtRef struct {
	vol *Volume
}

// dtTable maps the DTRefNums handed out by FPOpenDT to their volumes, per session.
// Reference numbers start at 1 (0 is reserved as "no desktop"); allocation reuses
// the lowest free id.
type dtTable struct {
	byRef  map[uint16]dtRef
	nextID uint16
}

func newDTTable() *dtTable {
	return &dtTable{byRef: make(map[uint16]dtRef), nextID: 1}
}

// open allocates a DTRefNum for a volume and returns it. A second FPOpenDT for a
// volume already open in this session yields a fresh ref (the Finder may open the
// Desktop more than once); both refer to the same per-volume database.
func (t *dtTable) open(vol *Volume) uint16 {
	id := t.nextID
	for {
		if id == 0 {
			id = 1
		}
		if _, taken := t.byRef[id]; !taken {
			break
		}
		id++
	}
	t.byRef[id] = dtRef{vol: vol}
	t.nextID = id + 1
	return id
}

// lookup resolves a DTRefNum to its volume.
func (t *dtTable) lookup(ref uint16) (*Volume, bool) {
	r, ok := t.byRef[ref]
	if !ok {
		return nil, false
	}
	return r.vol, true
}

// close invalidates a DTRefNum. Returns false if it was not open.
func (t *dtTable) close(ref uint16) bool {
	if _, ok := t.byRef[ref]; !ok {
		return false
	}
	delete(t.byRef, ref)
	return true
}

// --- handlers --------------------------------------------------------------

// afpOpenDT opens the Desktop database for a volume and returns a reference
// number. The per-volume database is created on first open and shared by every
// session (it is volume state, not session state).
//
// Request: cmd(1) pad(1) volID(2). Reply: DTRefNum(2).
func (s *Service) afpOpenDT(a *afpSession, block []byte) ([]byte, int32) {
	if len(block) < 4 {
		return nil, afpErrParamErr
	}
	vol, ok := s.VolumeByID(bp.BE16(block[2:4]))
	if !ok {
		return nil, afpErrParamErr
	}
	vol.ensureDesktop()
	ref := a.dt.open(vol)
	return bp.AppendBE16(nil, ref), afpNoErr
}

// afpCloseDT invalidates a Desktop reference number.
//
// Request: cmd(1) pad(1) DTRefNum(2). Reply: empty.
func (s *Service) afpCloseDT(a *afpSession, block []byte) ([]byte, int32) {
	if len(block) < 4 {
		return nil, afpErrParamErr
	}
	if !a.dt.close(bp.BE16(block[2:4])) {
		return nil, afpErrParamErr
	}
	return nil, afpNoErr
}

// afpAddComment stores a Finder comment on a file or directory through the fork
// seam (v.FS().WriteComment), so the comment travels with the file's metadata.
//
// Request: cmd(1) pad(1) DTRefNum(2) dirID(4) pathType(1) pathname...
//
//	pad-to-even commentLen(1) comment...
//
// Reply: empty.
func (s *Service) afpAddComment(a *afpSession, block []byte) ([]byte, int32) {
	vol, store, off, code := s.resolveDTPath(a, block)
	if code != afpNoErr {
		return nil, code
	}
	// The comment Pascal string starts after the pathname, aligned to an even
	// offset from the block start (Inside Macintosh: Networking, "AddComment").
	if off%2 != 0 {
		off++
	}
	comment, _, ok := pString(block, off)
	if !ok {
		return nil, afpErrParamErr
	}
	if len(comment) > maxCommentLen {
		comment = comment[:maxCommentLen]
	}
	if err := vol.FS().WriteComment(store, comment); err != nil {
		return nil, afpErrMiscErr
	}
	return nil, afpNoErr
}

// afpRemoveComment clears a file or directory's Finder comment (a zero-length
// WriteComment through the fork seam).
//
// Request: cmd(1) pad(1) DTRefNum(2) dirID(4) pathType(1) pathname...
// Reply: empty.
func (s *Service) afpRemoveComment(a *afpSession, block []byte) ([]byte, int32) {
	vol, store, _, code := s.resolveDTPath(a, block)
	if code != afpNoErr {
		return nil, code
	}
	if err := vol.FS().WriteComment(store, nil); err != nil {
		return nil, afpErrMiscErr
	}
	return nil, afpNoErr
}

// afpGetComment retrieves a file or directory's Finder comment from the fork
// seam.
//
// Request: cmd(1) pad(1) DTRefNum(2) dirID(4) pathType(1) pathname...
// Reply: commentLen(1) comment... (kFPItemNotFound if there is no comment).
func (s *Service) afpGetComment(a *afpSession, block []byte) ([]byte, int32) {
	vol, store, _, code := s.resolveDTPath(a, block)
	if code != afpNoErr {
		return nil, code
	}
	comment, ok := vol.FS().ReadComment(store)
	if !ok || len(comment) == 0 {
		return nil, afpErrItemNotFound
	}
	if len(comment) > maxCommentLen {
		comment = comment[:maxCommentLen]
	}
	return putPString(nil, comment), afpNoErr
}

// afpAddIcon stores an icon bitmap in a volume's Desktop database. It arrives over
// the two-phase ASPWrite path (the bitmap is bulk write data), so by the time it
// reaches here the data has already been collected onto the command block.
//
// Request: cmd(1) pad(1) DTRefNum(2) creator(4) type(4) iconType(1) pad(1)
//
//	tag(4) size(2) bitmap...
//
// Reply: empty. A replacement icon of a different size → kFPIconTypeError.
func (s *Service) afpAddIcon(a *afpSession, block []byte) ([]byte, int32) {
	if len(block) < 20 {
		return nil, afpErrParamErr
	}
	vol, ok := a.dt.lookup(bp.BE16(block[2:4]))
	if !ok {
		return nil, afpErrParamErr
	}
	var creator, fileType [4]byte
	copy(creator[:], block[4:8])
	copy(fileType[:], block[8:12])
	iconType := block[12]
	tag := bp.BE32(block[14:18])
	size := int(bp.BE16(block[18:20]))
	bitmap := block[20:]
	if len(bitmap) > size {
		bitmap = bitmap[:size]
	}
	if len(bitmap) < size {
		return nil, afpErrParamErr // data short of the declared size
	}
	return nil, vol.desktop().setIcon(creator, fileType, iconType, tag, bitmap)
}

// afpGetIcon retrieves an icon bitmap from a volume's Desktop database.
//
// Request: cmd(1) pad(1) DTRefNum(2) creator(4) type(4) iconType(1) pad(1)
//
//	length(2).
//
// Reply: the icon bitmap (truncated to length; a length of 0 tests presence and
// returns an empty success). kFPItemNotFound if the icon is not registered.
func (s *Service) afpGetIcon(a *afpSession, block []byte) ([]byte, int32) {
	if len(block) < 16 {
		return nil, afpErrParamErr
	}
	vol, ok := a.dt.lookup(bp.BE16(block[2:4]))
	if !ok {
		return nil, afpErrParamErr
	}
	var creator, fileType [4]byte
	copy(creator[:], block[4:8])
	copy(fileType[:], block[8:12])
	iconType := block[12]
	length := int(bp.BE16(block[14:16]))

	entry, found := vol.desktop().getIcon(creator, fileType, iconType)
	if !found {
		return nil, afpErrItemNotFound
	}
	if length == 0 {
		return nil, afpNoErr // presence test
	}
	data := entry.bitmap
	if length < len(data) {
		data = data[:length]
	}
	return append([]byte(nil), data...), afpNoErr
}

// afpGetIconInfo returns metadata for the index-th icon registered for a creator,
// so the Finder can iterate a creator's icon set.
//
// Request: cmd(1) pad(1) DTRefNum(2) creator(4) iconIndex(2).
// Reply: tag(4) fileType(4) iconType(1) pad(1) size(2). kFPItemNotFound past the
// last icon.
func (s *Service) afpGetIconInfo(a *afpSession, block []byte) ([]byte, int32) {
	if len(block) < 10 {
		return nil, afpErrParamErr
	}
	vol, ok := a.dt.lookup(bp.BE16(block[2:4]))
	if !ok {
		return nil, afpErrParamErr
	}
	var creator [4]byte
	copy(creator[:], block[4:8])
	index := bp.BE16(block[8:10])

	entry, fileType, iconType, found := vol.desktop().iconInfoByIndex(creator, index)
	if !found {
		return nil, afpErrItemNotFound
	}
	out := make([]byte, 0, 12)
	out = bp.AppendBE32(out, entry.tag)
	out = append(out, fileType[:]...)
	out = append(out, iconType, 0) // iconType + pad
	out = bp.AppendBE16(out, uint16(len(entry.bitmap)))
	return out, afpNoErr
}

// afpAddAPPL registers an APPL (creator → application) mapping after verifying the
// application file exists.
//
// Request: cmd(1) pad(1) DTRefNum(2) dirID(4) creator(4) tag(4) pathType(1)
//
//	pathname...
//
// Reply: empty.
func (s *Service) afpAddAPPL(a *afpSession, block []byte) ([]byte, int32) {
	if len(block) < 17 {
		return nil, afpErrParamErr
	}
	vol, ok := a.dt.lookup(bp.BE16(block[2:4]))
	if !ok {
		return nil, afpErrParamErr
	}
	dirID := bp.BE32(block[4:8])
	var creator [4]byte
	copy(creator[:], block[8:12])
	tag := bp.BE32(block[12:16])
	pathType := block[16]
	store, code := resolveCatalogPath(vol, dirID, block, 17, pathType)
	if code != afpNoErr {
		return nil, code
	}
	info, err := vol.Stat(store)
	if err != nil {
		return nil, mapStatErr(err)
	}
	if info.IsDir() {
		return nil, afpErrObjectTypeErr // an APPL must name a file
	}
	vol.desktop().addAPPL(creator, tag, dirID, store)
	return nil, afpNoErr
}

// afpRemoveAPPL drops an APPL mapping.
//
// Request: cmd(1) pad(1) DTRefNum(2) dirID(4) creator(4) pathType(1) pathname...
// Reply: empty.
func (s *Service) afpRemoveAPPL(a *afpSession, block []byte) ([]byte, int32) {
	if len(block) < 13 {
		return nil, afpErrParamErr
	}
	vol, ok := a.dt.lookup(bp.BE16(block[2:4]))
	if !ok {
		return nil, afpErrParamErr
	}
	dirID := bp.BE32(block[4:8])
	var creator [4]byte
	copy(creator[:], block[8:12])
	pathType := block[12]
	store, code := resolveCatalogPath(vol, dirID, block, 13, pathType)
	if code != afpNoErr {
		return nil, code
	}
	vol.desktop().removeAPPL(creator, dirID, store)
	return nil, afpNoErr
}

// afpGetAPPL returns the index-th APPL mapping for a creator plus the application
// file's requested parameters, so the Finder can resolve "open with".
//
// Request: cmd(1) pad(1) DTRefNum(2) creator(4) applIndex(2) bitmap(2).
// Reply: bitmap(2) applTag(4) <file params>. kFPItemNotFound past the last entry.
func (s *Service) afpGetAPPL(a *afpSession, block []byte) ([]byte, int32) {
	if len(block) < 12 {
		return nil, afpErrParamErr
	}
	vol, ok := a.dt.lookup(bp.BE16(block[2:4]))
	if !ok {
		return nil, afpErrParamErr
	}
	var creator [4]byte
	copy(creator[:], block[4:8])
	index := bp.BE16(block[8:10])
	bitmap := bp.BE16(block[10:12])

	entry, found := vol.desktop().applByIndex(creator, index)
	if !found {
		return nil, afpErrItemNotFound
	}
	info, err := vol.Stat(entry.pathname)
	if err != nil {
		return nil, afpErrObjectNotFnd // the application file is gone
	}

	out := make([]byte, 0, 32)
	out = bp.AppendBE16(out, bitmap)
	out = bp.AppendBE32(out, entry.tag)
	// APPL entries are always files; pack the file half of the bitmap. pathType 0
	// keeps any LongName store-native (the request carries no path-type byte).
	out = vol.fileDirParams(out, entry.pathname, info, bitmap, 0)
	return out, afpNoErr
}

// resolveDTPath decodes the (DTRefNum, dirID, pathType, pathname) shared by the
// three comment commands and resolves the target store path. It returns the
// volume, the store path, the offset just past the pathname (for AddComment's
// trailing comment), and a result code.
//
// Request prefix: cmd(1) pad(1) DTRefNum(2) dirID(4) pathType(1) pathname...
func (s *Service) resolveDTPath(a *afpSession, block []byte) (*Volume, string, int, int32) {
	if len(block) < 9 {
		return nil, "", 0, afpErrParamErr
	}
	vol, ok := a.dt.lookup(bp.BE16(block[2:4]))
	if !ok {
		return nil, "", 0, afpErrParamErr
	}
	dirID := bp.BE32(block[4:8])
	pathType := block[8]
	parent, code := dirPath(vol, dirID)
	if code != afpNoErr {
		return nil, "", 0, code
	}
	name, next, ok := pString(block, 9)
	if !ok {
		return nil, "", 0, afpErrParamErr
	}
	store, err := vol.ResolvePath(parent, string(name), pathType)
	if err != nil {
		return nil, "", 0, afpErrParamErr
	}
	if _, err := vol.Stat(store); err != nil {
		return nil, "", 0, mapStatErr(err)
	}
	return vol, store, next, afpNoErr
}
