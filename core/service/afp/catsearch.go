package afp

import (
	"errors"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	protocol "github.com/ObsoleteMadness/ClassicStack/core/protocol/afp"
)

// FPCatSearch (Inside Macintosh: Networking, AFP 2.1 §"FPCatSearch") searches a
// volume's whole catalog for files and directories matching a set of criteria,
// returning their parameters a page at a time. It is the wire behind the Finder's
// "Find File".
//
// The search SEMANTICS belong to the FileSystem backend, not to this spine: a
// plain hierarchical backend walks its tree, while a synthetic backend redefines
// "search" entirely (MacGarden turns a CatSearch into an explicit archive query
// and materialises the HTML results as virtual files — entries an Enumerate of
// the volume would never surface). So this handler does NOT impose a tree-walk.
// It decodes the AFP wire criteria into the backend-neutral fs.CatSearchCriteria,
// delegates to the bound FileSystem through its optional fs.CatSearcher
// capability, and packs whatever store paths the backend returns with the same
// parameter packer the catalog-read commands use (parms.go). A volume whose
// backend does not advertise Capabilities().CatSearch answers kFPCallNotSupported
// — the AFP-correct result for a backend that declines the search.

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
// single-quantum ASP response (24-byte fixed reply header below the 4624 quantum;
// 4096 is a conservative round figure). When the packed records would overflow it
// the handler stops early and the backend's cursor resumes the rest.
const catSearchMaxData = 4096

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
	vol, ok := a.openVols[bp.BE16(block[2:4])]
	if !ok {
		return nil, afpErrParamErr
	}

	// The backend defines the search; a volume whose backend declines CatSearch
	// answers kFPCallNotSupported rather than a half-emulated walk.
	searcher, ok := vol.catSearcher()
	if !ok {
		return nil, afpErrCallNotSuppt
	}

	reqMatches := int(bp.BE32(block[4:8]))
	if reqMatches <= 0 {
		return nil, afpErrParamErr
	}
	// catalogPosition is the resumption token the backend defines: a 16-byte blob
	// the client echoes verbatim. We carry the backend's cursor bytes in its tail
	// (after a 4-byte length) and round-trip them without interpreting them.
	var pos [16]byte
	copy(pos[:], block[12:28])
	cursor := decodeCatCursor(pos)

	fileBitmap := bp.BE16(block[28:30])
	dirBitmap := bp.BE16(block[30:32])
	reqBitmap := bp.BE32(block[32:36])

	crit, code := vol.decodeCatSearchCriteria(reqBitmap, block[36:])
	if code != afpNoErr {
		return nil, code
	}
	crit.Max = reqMatches

	// A search asking for neither file nor dir parameters has nothing to return;
	// default both to long-name+parent so a bitmap-0 client still gets usable hits.
	if fileBitmap == 0 && dirBitmap == 0 {
		fileBitmap = protocol.FDBitmapLongName | protocol.FDBitmapParentDID | protocol.FileBitmapFileNum
		dirBitmap = protocol.FDBitmapLongName | protocol.FDBitmapParentDID | protocol.DirBitmapDirID
	}

	results, next, err := searcher.CatSearch(crit, cursor)
	if err != nil {
		if errors.Is(err, fs.ErrCatSearchUnsupported) {
			return nil, afpErrCallNotSuppt
		}
		return nil, afpErrMiscErr
	}

	out := make([]byte, 0, 32+catSearchMaxData)
	out = append(out, make([]byte, 16)...) // CatalogPosition, patched below
	out = bp.AppendBE16(out, fileBitmap)
	out = bp.AppendBE16(out, dirBitmap)
	countOff := len(out)
	out = bp.AppendBE32(out, 0) // ActualCount, patched below

	actual := 0
	capped := false
	for _, m := range results {
		rec := vol.packCatSearchRecord(m, fileBitmap, dirBitmap)
		// Always emit at least one record so a single over-large record still makes
		// progress rather than stalling the search (the FPEnumerate convention).
		if actual > 0 && len(out)-countOff-4+len(rec) > catSearchMaxData {
			capped = true // payload cap reached before the backend's page ended
			break
		}
		out = append(out, rec...)
		actual++
	}

	// Determine the reply cursor. If we packed every result the backend gave us
	// and the backend reported no continuation, the search is done: last page,
	// kFPEOFErr, zero cursor (the AFP/Netatalk convention). Otherwise carry a
	// cursor that resumes AFTER the records we actually delivered.
	var replyPos [16]byte
	result := afpErrEOFErr
	switch {
	case capped:
		// We packed fewer records than the backend handed us, so the backend's own
		// cursor points past results the client never saw. Re-run the search bounded
		// to what we DID deliver: the backend then reports the cursor that resumes
		// after them. Echoing the request's cursor instead re-delivers this page
		// verbatim on every follow-up call — the client never advances, so a search
		// with more matches than fit one reply repeats forever.
		capCrit := crit
		capCrit.Max = actual
		if _, capNext, err := searcher.CatSearch(capCrit, cursor); err == nil && len(capNext) > 0 {
			replyPos = encodeCatCursor(capNext)
			result = afpNoErr
		}
		// A backend that cannot produce a mid-page cursor ends the search here
		// (zero cursor + kFPEOFErr): dropping the tail beats looping forever.
	case len(next) > 0:
		replyPos = encodeCatCursor(next)
		result = afpNoErr
	}
	copy(out[0:16], replyPos[:])
	out[countOff] = byte(actual >> 24)
	out[countOff+1] = byte(actual >> 16)
	out[countOff+2] = byte(actual >> 8)
	out[countOff+3] = byte(actual)

	return out, result
}

// packCatSearchRecord packs one backend result as an AFP ResultsRecord:
// StructLength(1) fileDir(1) then the file or directory parameter block, padded
// to an even total. StructLength counts the length byte itself.
func (v *Volume) packCatSearchRecord(m fs.CatSearchResult, fileBitmap, dirBitmap uint16) []byte {
	bitmap := dirBitmap
	if !m.Info.IsDir() {
		bitmap = fileBitmap
	}
	rec := make([]byte, 0, 64)
	rec = append(rec, 0) // length byte, patched below
	if m.Info.IsDir() {
		rec = append(rec, isDirFlag)
	} else {
		rec = append(rec, 0)
	}
	rec = v.fileDirParams(rec, m.Path, m.Info, bitmap, PathTypeLongNames)
	if len(rec)%2 != 0 {
		rec = append(rec, 0)
	}
	rec[0] = byte(len(rec))
	return rec
}

// catSearcher returns the bound FileSystem's optional catalog-search capability,
// gated on the backend advertising it: a backend that implements CatSearcher but
// reports Capabilities().CatSearch == false is treated as declining the search.
func (v *Volume) catSearcher() (fs.CatSearcher, bool) {
	if !v.FS().Capabilities().CatSearch {
		return nil, false
	}
	cs, ok := v.FS().(fs.CatSearcher)
	return cs, ok
}

// decodeCatSearchCriteria decodes the spec1/spec2 records into the backend-neutral
// fs.CatSearchCriteria. Each spec is a 2-byte length followed by a parameter block
// laid out in the same ascending-bit order as a catalog parameter block, but only
// the reqBitmap-selected fields are present. We read the predicate fields the seam
// models — name (partial/full) and parent dir id — and pass them store-native so
// the backend matches against its own names. The human-readable name also fills
// Query, so a synthetic backend (MacGarden) that runs an explicit search has the
// search text without re-decoding the AFP wire.
func (v *Volume) decodeCatSearchCriteria(reqBitmap uint32, specs []byte) (fs.CatSearchCriteria, int32) {
	var crit fs.CatSearchCriteria
	if reqBitmap == 0 {
		return crit, afpNoErr // match-everything search
	}
	spec1, _, ok := catSearchSpec(specs, 0)
	if !ok {
		return crit, afpErrParamErr
	}

	off := 0
	nameOffsetPos := -1
	if reqBitmap&catSearchBitParentDID != 0 {
		if off+4 > len(spec1) {
			return crit, afpErrParamErr
		}
		parentID := bp.BE32(spec1[off : off+4])
		off += 4
		path, code := dirPath(v, parentID)
		if code != afpNoErr {
			return crit, code
		}
		crit.MatchParent = true
		crit.ParentPath = path
	}
	if reqBitmap&(catSearchBitPartialName|catSearchBitFullName) != 0 {
		if off+2 > len(spec1) {
			return crit, afpErrParamErr
		}
		nameOffsetPos = int(bp.BE16(spec1[off : off+2]))
		crit.MatchName = true
		crit.Partial = reqBitmap&catSearchBitPartialName != 0
	}

	if crit.MatchName {
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
		crit.Name = string(stored)
		crit.Query = string(stored)
	}
	return crit, afpNoErr
}

// catSearchSpec reads one length-prefixed spec block (2-byte big-endian length +
// that many bytes) at off, returning the block and the offset past it.
func catSearchSpec(b []byte, off int) (spec []byte, next int, ok bool) {
	if off+2 > len(b) {
		return nil, off, false
	}
	n := int(bp.BE16(b[off : off+2]))
	off += 2
	if off+n > len(b) {
		return nil, off, false
	}
	return b[off : off+n], off + n, true
}

// catSearchName reads the Pascal-string name a spec block's name field points at.
// The name offset is measured from the start of the spec parameter block.
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

// --- catalogPosition codec: carry the backend's opaque cursor in the 16-byte
// position blob. Byte 0 flags a live continuation; byte 1 holds the cursor length
// (0–14); bytes 2.. hold the cursor bytes. The handler never interprets the
// cursor — only the backend does — so any backend pagination scheme survives the
// round trip as long as it fits 14 bytes (the WalkCatSearch default uses 4). ---

func decodeCatCursor(pos [16]byte) fs.CatSearchCursor {
	if pos[0] == 0 {
		return nil // new search
	}
	n := int(pos[1])
	if n == 0 || 2+n > len(pos) {
		return nil
	}
	return fs.CatSearchCursor(append([]byte(nil), pos[2:2+n]...))
}

func encodeCatCursor(c fs.CatSearchCursor) [16]byte {
	var pos [16]byte
	n := len(c)
	if n == 0 {
		return pos
	}
	if n > 14 {
		n = 14
	}
	pos[0] = 0x01
	pos[1] = byte(n)
	copy(pos[2:2+n], c[:n])
	return pos
}
