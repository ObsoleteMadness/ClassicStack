package afp

import (
	"errors"
	stdfs "io/fs"
	"os"
	"strings"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"

	"github.com/ObsoleteMadness/ClassicStack/core/metastore"
)

// Catalog-mutation handlers (Inside Macintosh: Networking, AFP 2.x §6): the
// commands that create, delete, rename, and open directories on a volume. They
// reach storage only through the FileSystem half of the §9 seam
// (v.FS().CreateFile/CreateDir/Remove plus v.renamePath, which carries the
// metadata container and rebinds CNIDs); the spine itself holds no
// storage-layout knowledge.
//
// Directory ids are AFP CatalogNodeIDs (CNIDs): the volume's CNIDStore maps a
// dirID to a store path and back (root == CNIDRoot == 2). Every path-bearing
// request resolves its target as dirID + relative pathname, so a client that
// walked into a subdirectory via FPOpenDir addresses children from there — the
// same model the catalog-read commands now share through resolveCatalogPath.

// FPCreateFile CreateFlag (Inside Macintosh: Networking, "CreateFile"): bit 7 of
// the flag byte selects a hard create (truncate/replace an existing file)
// instead of a soft create (fail if the object already exists).
const createFlagHard uint8 = 0x80

// afpCreateFile creates a file in a directory.
//
// Request: cmd(1) flag(1) volID(2) dirID(4) pathType(1) pathname...
// Reply: empty. A soft create over an existing object → kFPObjectExists.
func (s *Service) afpCreateFile(a *afpSession, block []byte) ([]byte, int32) {
	if len(block) < 9 {
		return nil, afpErrParamErr
	}
	hardCreate := block[1]&createFlagHard != 0
	vol, ok := a.openVols[bp.BE16(block[2:4])]
	if !ok {
		return nil, afpErrParamErr
	}
	dirID := bp.BE32(block[4:8])
	pathType := block[8]
	store, code := resolveCatalogPath(vol, dirID, block, 9, pathType)
	if code != afpNoErr {
		return nil, code
	}

	if !hardCreate {
		if _, err := vol.Stat(store); err == nil {
			return nil, afpErrObjectExists
		}
	}
	f, err := vol.FS().CreateFile(store)
	if err != nil {
		return nil, mapCreateErr(err)
	}
	_ = f.Close()
	vol.CNID(store) // allocate the new file's catalog id
	return nil, afpNoErr
}

// afpCreateDir creates a directory and returns its newly allocated directory id.
//
// Request: cmd(1) pad(1) volID(2) dirID(4) pathType(1) pathname...
// Reply: dirID(4) of the new directory.
func (s *Service) afpCreateDir(a *afpSession, block []byte) ([]byte, int32) {
	if len(block) < 9 {
		return nil, afpErrParamErr
	}
	vol, ok := a.openVols[bp.BE16(block[2:4])]
	if !ok {
		return nil, afpErrParamErr
	}
	dirID := bp.BE32(block[4:8])
	pathType := block[8]
	store, code := resolveCatalogPath(vol, dirID, block, 9, pathType)
	if code != afpNoErr {
		return nil, code
	}

	if _, err := vol.Stat(store); err == nil {
		return nil, afpErrObjectExists
	}
	if err := vol.FS().CreateDir(store); err != nil {
		return nil, mapCreateErr(err)
	}
	newID := vol.CNID(store)
	out := bp.AppendBE32(nil, newID)
	return out, afpNoErr
}

// afpDelete deletes a file or an empty directory.
//
// Request: cmd(1) pad(1) volID(2) dirID(4) pathType(1) pathname...
// Reply: empty. The volume root cannot be deleted (kFPAccessDenied); a missing
// object → kFPObjectNotFound.
func (s *Service) afpDelete(a *afpSession, block []byte) ([]byte, int32) {
	if len(block) < 9 {
		return nil, afpErrParamErr
	}
	vol, ok := a.openVols[bp.BE16(block[2:4])]
	if !ok {
		return nil, afpErrParamErr
	}
	dirID := bp.BE32(block[4:8])
	pathType := block[8]
	store, code := resolveCatalogPath(vol, dirID, block, 9, pathType)
	if code != afpNoErr {
		return nil, code
	}
	if store == "" {
		return nil, afpErrAccessDenied // refuse to delete the volume root
	}
	if _, err := vol.Stat(store); err != nil {
		return nil, mapStatErr(err)
	}
	if err := vol.removePath(store); err != nil {
		return nil, mapDeleteErr(err)
	}
	return nil, afpNoErr
}

// afpRename renames a file or directory in place (same parent directory).
//
// Request: cmd(1) pad(1) volID(2) dirID(4) pathType(1) pathname...
//
//	newType(1) newName...
//
// Reply: empty. The new name must not already exist (kFPObjectExists); the CNID
// rides the rename so the object's directory id survives (v.renamePath).
func (s *Service) afpRename(a *afpSession, block []byte) ([]byte, int32) {
	if len(block) < 9 {
		return nil, afpErrParamErr
	}
	vol, ok := a.openVols[bp.BE16(block[2:4])]
	if !ok {
		return nil, afpErrParamErr
	}
	dirID := bp.BE32(block[4:8])
	pathType := block[8]
	// First pathname: the object to rename.
	name, nameEnd, ok := pString(block, 9)
	if !ok {
		return nil, afpErrParamErr
	}
	parent, code := dirPath(vol, dirID)
	if code != afpNoErr {
		return nil, code
	}
	oldStore, err := vol.ResolvePath(parent, string(name), pathType)
	if err != nil {
		return nil, afpErrParamErr
	}
	if oldStore == "" {
		return nil, afpErrAccessDenied // cannot rename the volume root
	}
	// Second pathname (after newType byte): the new (leaf) name. It is always a
	// single element in the same parent directory — FPRename never moves.
	if nameEnd >= len(block) {
		return nil, afpErrParamErr
	}
	newType := block[nameEnd]
	newName, _, ok := pString(block, nameEnd+1)
	if !ok {
		return nil, afpErrParamErr
	}
	dir, _ := splitStore(oldStore)
	newStore, err := vol.ResolvePath(dir, string(newName), newType)
	if err != nil {
		return nil, afpErrParamErr
	}

	if _, err := vol.Stat(oldStore); err != nil {
		return nil, mapStatErr(err)
	}
	if _, err := vol.Stat(newStore); err == nil {
		return nil, afpErrObjectExists
	}
	if err := vol.renamePath(oldStore, newStore); err != nil {
		return nil, mapRenameErr(err)
	}
	return nil, afpNoErr
}

// afpOpenDir opens a directory on a variable-Directory-ID volume and returns its
// directory id, so a client can address that directory's children directly in
// later requests (FPEnumerate, FPCreateFile, …).
//
// Request: cmd(1) pad(1) volID(2) dirID(4) pathType(1) pathname...
// Reply: dirID(4). A path naming a non-directory → kFPObjectTypeErr.
func (s *Service) afpOpenDir(a *afpSession, block []byte) ([]byte, int32) {
	if len(block) < 9 {
		return nil, afpErrParamErr
	}
	vol, ok := a.openVols[bp.BE16(block[2:4])]
	if !ok {
		return nil, afpErrParamErr
	}
	dirID := bp.BE32(block[4:8])
	pathType := block[8]
	store, code := resolveCatalogPath(vol, dirID, block, 9, pathType)
	if code != afpNoErr {
		return nil, code
	}
	info, err := vol.Stat(store)
	if err != nil {
		return nil, mapStatErr(err)
	}
	if !info.IsDir() {
		return nil, afpErrObjectTypeErr
	}
	out := bp.AppendBE32(nil, vol.CNID(store))
	return out, afpNoErr
}

// afpCloseDir releases a directory id a client opened with FPOpenDir. The spine
// keeps directory ids resident in the CNID store (they are stable catalog ids,
// not per-open handles), so the close is a no-op acknowledged with success — the
// id stays valid, matching how AFP servers that key dirIDs on CNIDs behave.
//
// Request: cmd(1) pad(1) volID(2) dirID(4). Reply: empty.
func (s *Service) afpCloseDir(a *afpSession, block []byte) ([]byte, int32) {
	if len(block) < 8 {
		return nil, afpErrParamErr
	}
	if _, ok := a.openVols[bp.BE16(block[2:4])]; !ok {
		return nil, afpErrParamErr
	}
	return nil, afpNoErr
}

// --- dirID-relative path resolution shared by the catalog commands ---

// dirPath maps an AFP directory id to its store path. The root id (CNIDRoot)
// always resolves to the volume root (""); any other id must have been minted by
// a prior FPOpenVol/FPCreateDir/FPOpenDir on this volume's CNID store.
func dirPath(vol *Volume, dirID uint32) (string, int32) {
	if dirID == metastore.CNIDRoot {
		return "", afpNoErr
	}
	p, ok := vol.PathForCNID(dirID)
	if !ok {
		return "", afpErrDirNotFound
	}
	return p, afpNoErr
}

// pascalPathAt reads the AFP pathname at off in a command block. For every path
// type (short/long/UTF-8) the pathname on the wire is a Pascal string: a 1-byte
// length followed by that many name bytes (the bytes may themselves contain the
// interior \x00 separators of a multi-level path). It returns just the name
// bytes, WITHOUT the length prefix — the form ResolvePath expects. Failing to
// strip this length byte makes it the first character of the first path element,
// so every non-empty by-name lookup resolves to a bogus store path and returns
// kFPObjectNotFound (the mount-blocking regression, observed on the wire as
// FPGetFileDirParms Name=… → object not found -5018).
func pascalPathAt(block []byte, off int) (string, bool) {
	if off >= len(block) {
		// No pathname present at all is treated as the empty (this-dir) path.
		return "", off == len(block)
	}
	n := int(block[off])
	off++
	if off+n > len(block) {
		return "", false
	}
	return string(block[off : off+n]), true
}

// wantsVolumeRoot reports whether a parent-of-root (DID 1) request names the
// volume itself: an empty path (the root implicitly) or a path whose single
// element decodes to the volume's display name. The comparison is done on the
// decoded, store-charset name so it is codec-consistent with the volume Name the
// server advertises in FPOpenVol / FPGetVolParms.
func wantsVolumeRoot(vol *Volume, name string, pathType uint8) bool {
	// Strip a leading NUL and a trailing NUL terminator, matching ResolvePath's
	// element convention, so "\x00Test Volume" and "Test Volume\x00" both match.
	name = strings.Trim(name, "\x00")
	if name == "" {
		return true
	}
	if strings.Contains(name, "\x00") {
		return false // a multi-level path can't name the volume root
	}
	decoded, err := vol.codec().Decode([]byte(name), wireFor(pathType))
	if err != nil {
		return false
	}
	return strings.EqualFold(string(decoded), vol.Name())
}

// resolveCatalogPath resolves a command block's pathname (a Pascal string at off)
// relative to dirID, returning the target store path. It is the dirID-aware
// successor to resolveBlockPath: the directory id selects the base, then the
// volume's FilenameCodec decodes each wire element to a store-native name.
func resolveCatalogPath(vol *Volume, dirID uint32, block []byte, off int, pathType uint8) (string, int32) {
	name, ok := pascalPathAt(block, off)
	if !ok {
		return "", afpErrParamErr
	}
	// Parent-of-root (DID 1) is the synthetic directory whose sole child is the
	// volume itself. The Finder resolves a freshly-mounted volume with
	// FPGetFileDirParms DID=1 Name="<volume name>"; the only valid target is the
	// volume root. Without this it returned kFPDirNotFound (-5029) and the volume
	// mounted nameless. Inside Macintosh: Networking — ParentDirID of the root is 1.
	if dirID == metastore.CNIDParentOfRoot {
		if wantsVolumeRoot(vol, name, pathType) {
			return "", afpNoErr
		}
		return "", afpErrObjectNotFnd
	}
	parent, code := dirPath(vol, dirID)
	if code != afpNoErr {
		return "", code
	}
	store, err := vol.ResolvePath(parent, name, pathType)
	if err != nil {
		return "", afpErrParamErr // unrepresentable name → "illegal name"
	}
	return store, afpNoErr
}

// --- error mapping (store error → AFP result code) ---

// mapCreateErr maps a CreateFile/CreateDir error to an AFP result code.
func mapCreateErr(err error) int32 {
	switch {
	case err == nil:
		return afpNoErr
	case os.IsExist(err):
		return afpErrObjectExists
	case errors.Is(err, stdfs.ErrPermission):
		return afpErrAccessDenied
	case isNotExist(err):
		return afpErrObjectNotFnd // a missing parent directory
	default:
		return afpErrAccessDenied
	}
}

// mapDeleteErr maps a Remove error to an AFP result code.
func mapDeleteErr(err error) int32 {
	switch {
	case err == nil:
		return afpNoErr
	case errors.Is(err, stdfs.ErrPermission):
		return afpErrAccessDenied
	case isNotExist(err):
		return afpErrObjectNotFnd
	default:
		return afpErrAccessDenied
	}
}

// mapRenameErr maps a Rename error to an AFP result code.
func mapRenameErr(err error) int32 {
	switch {
	case err == nil:
		return afpNoErr
	case os.IsExist(err):
		return afpErrObjectExists
	case errors.Is(err, stdfs.ErrPermission):
		return afpErrAccessDenied
	case isNotExist(err):
		return afpErrObjectNotFnd
	default:
		return afpErrCantMove
	}
}
