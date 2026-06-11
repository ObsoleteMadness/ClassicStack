package afp

import (
	stdfs "io/fs"
	"strings"
)

// FPCatSearch (Inside Macintosh: Networking, AFP 2.1 §"FPCatSearch") searches a
// volume's whole catalog for files and directories matching a set of criteria,
// returning their parameters a page at a time. It is the wire behind the Finder's
// "Find File" — the dominant use is a partial- or full-name match.
//
// This implementation walks the catalog through the §9 FileSystem seam
// (v.Enumerate, recursively) and packs each match with the same parameter packer
// the catalog-read commands use (parms.go), so it carries no storage-layout
// knowledge. It honours the criteria the field actually exercises — name
// (partial/full), parent dir id, and the file/dir discriminator implied by which
// result bitmap is non-empty — and ignores criteria bits it does not model rather
// than failing the search, matching the lenient behaviour real servers show
// against the assorted clients in the wild (documented in spec/errata.md).

// cmdCatSearch is the AFP command code for FPCatSearch.
const cmdCatSearch uint8 = 43

// CatSearch request bitmap bits (Inside Macintosh: Networking, "FPCatSearch",
// "ReqBitMap"). They select which fields of the spec1/spec2 records participate
// in the match; the low bits mirror the file/dir parameter bitmap (fdBitmap*).
const (
	catSearchBitPartialName uint32 = 1 << 0 // partial-name match (substring)
	catSearchBitFullName    uint32 = 1 << 1 // full-name match (exact)
	catSearchBitParentDID   uint32 = 1 << 4 // parent directory id
)

// catSearchMaxData caps one reply's ResultsRecord area so the packed reply fits a
// single-quantum ASP response. ASP.QuantumSize less the fixed reply header (24
// bytes: 16-byte CatalogPosition + 2+2 bitmaps + 4 ActualCount) leaves room for
// the records; 4096 is a conservative round figure under the 4624 quantum.
const catSearchMaxData = 4096

// catSearchCriteria is the decoded match: the values from spec1 (and, for ranged
// fields, spec2), reduced to the predicates this server models. A zero criteria
// (reqBitmap == 0) matches everything, so a name-less search enumerates the
// volume — the behaviour a "find every item" client expects.
type catSearchCriteria struct {
	reqBitmap   uint32
	matchName   bool   // a name predicate (partial or full) is present
	partial     bool   // true: substring match; false: exact match
	name        string // store-native name to match against (decoded from spec1)
	matchParent bool   // a ParentDirID predicate is present
	parentID    uint32 // the parent dir id to match
}

// afpCatSearch handles FPCatSearch.
//
// Request: cmd(1) pad(1) volID(2) reqMatches(4) reserved(4) catalogPosition(16)
//
//	fileRsltBitmap(2) dirRsltBitmap(2) reqBitmap(4) spec1 spec2
//
// where each spec is len(2) + a parameter block matching reqBitmap. The reply is
// catalogPosition(16) fileRsltBitmap(2) dirRsltBitmap(2) actualCount(4) then one
// ResultsRecord per match (each: len(1) fileDir(1) <packed params>, padded even).
func (s *Service) afpCatSearch(a *afpSession, block []byte) ([]byte, int32) {
	if len(block) < 36 {
		return nil, afpErrParamErr
	}
	vol, ok := a.openVols[be16(block[2:4])]
	if !ok {
		return nil, afpErrParamErr
	}
	reqMatches := int(be32(block[4:8]))
	if reqMatches <= 0 {
		return nil, afpErrParamErr
	}
	// catalogPosition is the resumption cursor: a server-defined 16-byte blob the
	// client echoes verbatim. We use a flat 1-based visit index in its last 4
	// bytes (0 = start a new search); the first byte flags a live continuation.
	var cursor [16]byte
	copy(cursor[:], block[12:28])
	startIndex := int(be32(cursor[12:16]))

	fileBitmap := be16(block[28:30])
	dirBitmap := be16(block[30:32])
	reqBitmap := be32(block[32:36])

	crit, code := vol.decodeCatSearchCriteria(reqBitmap, block[36:])
	if code != afpNoErr {
		return nil, code
	}

	// A search asking for neither file nor dir parameters has nothing to return;
	// default both to long-name+parent so a bitmap-0 client still gets usable hits.
	if fileBitmap == 0 && dirBitmap == 0 {
		fileBitmap = fdBitmapLongName | fdBitmapParentDID | fileBitmapFileNum
		dirBitmap = fdBitmapLongName | fdBitmapParentDID | dirBitmapDirID
	}

	matches := vol.walkCatSearch(crit, fileBitmap, dirBitmap, startIndex, reqMatches)

	out := make([]byte, 0, 32+catSearchMaxData)
	out = append(out, make([]byte, 16)...) // CatalogPosition, patched below
	out = putBE16(out, fileBitmap)
	out = putBE16(out, dirBitmap)
	countOff := len(out)
	out = putBE32(out, 0) // ActualCount, patched below

	actual := 0
	for _, m := range matches {
		if len(out)-countOff-4+len(m.record) > catSearchMaxData {
			break // payload cap: stop and continue from here on the next call
		}
		out = append(out, m.record...)
		actual++
	}

	// Build the reply cursor. If we packed every remaining match (the walk found
	// no more after the last one we kept), the search is done: signal last-page
	// with kFPEOFErr and a zero cursor. Otherwise stamp the next visit index so
	// the client resumes mid-catalog.
	last := startIndex
	if actual > 0 {
		last = matches[actual-1].index
	}
	more := actual < len(matches) || (len(matches) > 0 && matches[len(matches)-1].more)

	var replyCursor [16]byte
	result := afpErrEOFErr
	if more {
		replyCursor[0] = 0x01 // continuation flag
		replyCursor[12] = byte(last >> 24)
		replyCursor[13] = byte(last >> 16)
		replyCursor[14] = byte(last >> 8)
		replyCursor[15] = byte(last)
		result = afpNoErr
	}
	copy(out[0:16], replyCursor[:])
	out[countOff] = byte(actual >> 24)
	out[countOff+1] = byte(actual >> 16)
	out[countOff+2] = byte(actual >> 8)
	out[countOff+3] = byte(actual)

	return out, result
}

// decodeCatSearchCriteria decodes the spec1/spec2 records into the predicates the
// server models. Each spec is a 2-byte length followed by a parameter block laid
// out in the same ascending-bit order as a catalog parameter block, but only the
// reqBitmap-selected fields are present. We read the fields we match on (name,
// parent dir id) and skip the rest. spec2 carries the upper bound of ranged
// fields and the Finder-info mask; the only ranged predicate we honour is the
// name (spec2 name unused — name is an exact/partial single value in spec1).
func (v *Volume) decodeCatSearchCriteria(reqBitmap uint32, specs []byte) (catSearchCriteria, int32) {
	crit := catSearchCriteria{reqBitmap: reqBitmap}
	if reqBitmap == 0 {
		return crit, afpNoErr // match-everything search
	}
	spec1, _, ok := catSearchSpec(specs, 0)
	if !ok {
		return crit, afpErrParamErr
	}

	off := 0
	// The spec block opens with a 2-byte bitmap of its own in some client
	// encodings; AFP 2.1 defines the spec block as a bare parameter block keyed by
	// reqBitmap, so we decode strictly by reqBitmap. Fields appear in ascending
	// bit order; name fields are a 2-byte offset pointer into the block's tail.
	nameOffsetPos := -1
	if reqBitmap&catSearchBitParentDID != 0 {
		if off+4 > len(spec1) {
			return crit, afpErrParamErr
		}
		crit.matchParent = true
		crit.parentID = be32(spec1[off : off+4])
		off += 4
	}
	if reqBitmap&(catSearchBitPartialName|catSearchBitFullName) != 0 {
		if off+2 > len(spec1) {
			return crit, afpErrParamErr
		}
		nameOffsetPos = int(be16(spec1[off : off+2]))
		off += 2
		crit.matchName = true
		crit.partial = reqBitmap&catSearchBitPartialName != 0
	}

	if crit.matchName {
		name, ok := catSearchName(spec1, nameOffsetPos)
		if !ok {
			return crit, afpErrParamErr
		}
		// The wire name is MacRoman in a CatSearch spec (the path-type byte does
		// not ride this request); decode through the volume codec to store-native.
		stored, err := v.codec().Decode(name, wireFor(PathTypeShortNames))
		if err != nil {
			return crit, afpErrParamErr
		}
		crit.name = string(stored)
	}
	return crit, afpNoErr
}

// catSearchSpec reads one length-prefixed spec block (2-byte big-endian length +
// that many bytes) at off, returning the block and the offset past it.
func catSearchSpec(b []byte, off int) (spec []byte, next int, ok bool) {
	if off+2 > len(b) {
		return nil, off, false
	}
	n := int(be16(b[off : off+2]))
	off += 2
	if off+n > len(b) {
		return nil, off, false
	}
	return b[off : off+n], off + n, true
}

// catSearchName reads the Pascal-string name a spec block's name field points at.
// The name offset is measured from the start of the spec parameter block. An
// out-of-range offset yields ok=false.
func catSearchName(spec []byte, nameOffset int) (name []byte, ok bool) {
	if nameOffset < 0 || nameOffset >= len(spec) {
		return nil, false
	}
	n := int(spec[nameOffset])
	if nameOffset+1+n > len(spec) {
		return nil, false
	}
	return spec[nameOffset+1 : nameOffset+1+n], true
}

// catSearchMatch is one packed result plus its flat visit index (the resumption
// cursor value) and whether the walk could see further entries after it.
type catSearchMatch struct {
	record []byte
	index  int
	more   bool
}

// walkCatSearch walks the whole volume catalog depth-first, packing a
// ResultsRecord for every entry past startIndex that satisfies the criteria, up
// to reqMatches records. The flat visit index lets a paged search resume without
// re-matching the entries it already returned. The walk visits the root's
// children and descends into each subdirectory, so a name search finds matches at
// any depth (the Finder "Find File" semantics).
func (v *Volume) walkCatSearch(crit catSearchCriteria, fileBitmap, dirBitmap uint16, startIndex, reqMatches int) []catSearchMatch {
	w := &catSearchWalk{
		vol:        v,
		crit:       crit,
		fileBitmap: fileBitmap,
		dirBitmap:  dirBitmap,
		startIndex: startIndex,
		reqMatches: reqMatches,
	}
	w.descend("")
	return w.out
}

// catSearchWalk carries the recursion state for one walkCatSearch.
type catSearchWalk struct {
	vol        *Volume
	crit       catSearchCriteria
	fileBitmap uint16
	dirBitmap  uint16
	startIndex int
	reqMatches int

	visited int // flat 1-based index of the entry we are about to consider
	out     []catSearchMatch
}

// descend walks one directory's children, considering each (skipping startIndex
// entries already returned) and recursing into subdirectories. It stops growing
// out once reqMatches records are collected, but keeps incrementing the visit
// counter so a resumed search lands on the right entry.
func (w *catSearchWalk) descend(dir string) {
	entries, err := w.vol.Enumerate(dir)
	if err != nil {
		return
	}
	for _, de := range entries {
		if isMetadataName(de.Name()) {
			continue
		}
		child := joinStore(dir, de.Name())
		info, err := de.Info()
		if err != nil {
			continue
		}
		w.consider(child, info)
		if de.IsDir() {
			w.descend(child)
		}
	}
}

// consider tests one entry against the criteria, packing it if it matches and
// falls past the resumption point. It advances the flat visit counter on every
// entry so cursors stay stable across pages.
func (w *catSearchWalk) consider(store string, info stdfs.FileInfo) {
	w.visited++
	if w.visited <= w.startIndex {
		return // already returned on an earlier page
	}
	if !w.crit.matches(w.vol, store, info) {
		return
	}
	if len(w.out) >= w.reqMatches {
		// Past the page limit but a further match exists → tell the caller more
		// pages follow, without packing this one.
		if len(w.out) > 0 {
			w.out[len(w.out)-1].more = true
		}
		return
	}
	bitmap := w.dirBitmap
	if !info.IsDir() {
		bitmap = w.fileBitmap
	}
	rec := make([]byte, 0, 64)
	rec = append(rec, 0) // length byte, patched below
	if info.IsDir() {
		rec = append(rec, isDirFlag)
	} else {
		rec = append(rec, 0)
	}
	rec = w.vol.fileDirParams(rec, store, info, bitmap, PathTypeLongNames)
	if len(rec)%2 != 0 {
		rec = append(rec, 0)
	}
	rec[0] = byte(len(rec)) // StructLength includes the length byte itself
	w.out = append(w.out, catSearchMatch{record: rec, index: w.visited})
}

// matches reports whether one catalog entry satisfies the search criteria. An
// empty criteria (no predicates) matches every entry. Predicates are ANDed.
func (c catSearchCriteria) matches(vol *Volume, store string, info stdfs.FileInfo) bool {
	if c.matchParent {
		if vol.ParentCNID(store) != c.parentID {
			return false
		}
	}
	if c.matchName {
		_, base := splitStore(store)
		if c.partial {
			if !containsFold(base, c.name) {
				return false
			}
		} else if !strings.EqualFold(base, c.name) {
			return false
		}
	}
	return true
}

// containsFold reports whether substr occurs in s, ignoring case. AFP/HFS+
// filename matching is case-insensitive, so the Finder's partial-name search must
// be too. It folds both operands to lower case before the substring test — a
// pragmatic ASCII/MacRoman fold, sufficient for the filenames this server holds.
func containsFold(s, substr string) bool {
	if substr == "" {
		return true
	}
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}
