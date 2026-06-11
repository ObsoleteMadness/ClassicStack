package fs

import (
	"errors"
	"io/fs"
	"strings"
)

// ErrCatSearchUnsupported is returned by CatSearch when the backend does not
// implement catalog search. The file service maps it to the protocol's
// "call not supported" result. Backends should advertise support consistently via
// Capabilities().CatSearch, which the service checks before calling CatSearch.
var ErrCatSearchUnsupported = errors.New("fs: CatSearch not supported by backend")

// CatSearch is an OPTIONAL FileSystem capability: the backend-defined catalog
// search behind AFP's FPCatSearch (and an SMB Trans2 find could share it). It is
// the FileSystem's to define — and to decline. A plain hierarchical backend
// (local_fs, memfs) walks its own tree; a synthetic backend redefines "search"
// entirely. MacGarden, for instance, turns a CatSearch into an explicit query
// against its upstream archive and materialises the HTML results as virtual
// folders and files — so the search results are not even entries that an
// Enumerate of the volume would surface. Because the semantics belong to the
// backend, the file service must NOT impose a fixed tree-walk; it decodes the
// protocol criteria into the neutral CatSearchCriteria below, hands them to the
// backend, and packs whatever the backend returns. A backend that does not
// implement CatSearcher (or whose Capabilities().CatSearch is false) makes the
// service answer the protocol's "not supported" result.

// CatSearchCriteria is the backend-neutral search request: the predicates a file
// service decoded from its wire protocol, reduced to a form every backend can act
// on without knowing AFP or SMB. A backend matches the predicates it understands
// and may ignore those it does not (a partial-name backend need not honour a date
// range); a synthetic backend may consult only Query. All-zero criteria
// (no predicate set) means "every catalog entry".
type CatSearchCriteria struct {
	// Name, when MatchName, is the store-native name to match. Partial selects a
	// case-insensitive substring match; otherwise it is a case-insensitive exact
	// match. The file service has already decoded the wire name to store-native
	// bytes through the share codec, so the backend compares against its own
	// names directly.
	MatchName bool
	Partial   bool
	Name      string

	// ParentPath, when MatchParent, restricts matches to direct children of this
	// store path (the file service resolved the protocol's parent dir id to a
	// path through its CNID store). The empty string is the volume root.
	MatchParent bool
	ParentPath  string

	// Query is a free-form search string for synthetic backends that run an
	// explicit query rather than a predicate walk (e.g. MacGarden's archive
	// search). Predicate backends ignore it; the file service fills it with the
	// human-readable search text when the protocol carries one.
	Query string

	// Max is the most results the caller wants this page (the protocol's
	// ReqMatches). A backend should return no more than Max results and report a
	// resumable cursor when more remain. Zero means the backend chooses.
	Max int
}

// CatSearchResult is one match: its '/'-separated store path and the FileInfo the
// backend already holds, so the file service packs catalog parameters without a
// second Stat. A synthetic backend returns the path of a virtual entry it
// materialised; the file service treats it like any other store path.
type CatSearchResult struct {
	Path string
	Info fs.FileInfo
}

// CatSearchCursor is the opaque resumption token a backend defines to page a
// search. The file service round-trips it verbatim through the protocol's
// position field (it does not interpret the bytes), so a backend can encode a
// flat index, a tree position, or an upstream pagination token as it sees fit. A
// nil/empty cursor starts a new search; a backend returns an empty Next to signal
// the last page.
type CatSearchCursor []byte

// CatSearcher is implemented by a FileSystem that supports catalog search. The
// file service type-asserts the bound FileSystem to it; a backend that does not
// implement it (or reports Capabilities().CatSearch == false) is treated as "no
// CatSearch", and the service returns the protocol's not-supported result.
//
// CatSearch runs one page of the search described by crit, resuming from cursor
// (nil to start). It returns the matches for this page, the cursor to pass next
// (empty when the search is exhausted), and any backend error. An error other
// than a clean end-of-results should be surfaced to the protocol as a generic
// search failure.
type CatSearcher interface {
	CatSearch(crit CatSearchCriteria, cursor CatSearchCursor) (results []CatSearchResult, next CatSearchCursor, err error)
}

// WalkCatSearch is the default predicate tree-walk a plain hierarchical backend
// can use to satisfy CatSearcher: it walks the volume depth-first through the
// FileSystem's own ReadDir, descending into every subdirectory, and returns the
// entries matching crit's name/parent predicates. It is exported so a backend
// implements CatSearch in one line — `return fs.WalkCatSearch(b, crit, cursor)` —
// while a synthetic backend (MacGarden) ignores it and runs its own search.
//
// The cursor is a flat depth-first visit index (4 big-endian bytes): a resumed
// walk re-walks the tree but skips the entries already returned, so paging
// neither repeats nor drops matches while the catalog is unchanged. crit.Max caps
// the page; an empty Next signals the last page.
func WalkCatSearch(fsys FileSystem, crit CatSearchCriteria, cursor CatSearchCursor) ([]CatSearchResult, CatSearchCursor, error) {
	start := decodeWalkCursor(cursor)
	w := &catWalk{fsys: fsys, crit: crit, start: start, max: crit.Max}
	w.descend("")
	var next CatSearchCursor
	if w.more {
		next = encodeWalkCursor(w.last)
	}
	return w.out, next, nil
}

// catWalk carries the recursion state for one WalkCatSearch.
type catWalk struct {
	fsys  FileSystem
	crit  CatSearchCriteria
	start int // flat index already returned on earlier pages
	max   int

	visited int
	last    int  // visit index of the last result kept (the next cursor)
	more    bool // a further match exists past this page
	out     []CatSearchResult
}

// descend walks one directory's children depth-first, considering each entry and
// recursing into subdirectories. It keeps advancing the visit counter even after
// the page is full so a resumed search lands on the right entry.
func (w *catWalk) descend(dir string) {
	entries, err := w.fsys.ReadDir(dir)
	if err != nil {
		return
	}
	for _, de := range entries {
		if isMetadataShadow(de.Name()) {
			continue
		}
		child := joinStorePath(dir, de.Name())
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

// consider tests one entry against the criteria, collecting it if it matches and
// falls past the resumption point and the page is not yet full.
func (w *catWalk) consider(path string, info fs.FileInfo) {
	w.visited++
	if w.visited <= w.start {
		return
	}
	if !w.matches(path, info) {
		return
	}
	if w.max > 0 && len(w.out) >= w.max {
		w.more = true // a further match exists → caller pages again
		return
	}
	w.out = append(w.out, CatSearchResult{Path: path, Info: info})
	w.last = w.visited
}

// matches applies the name and parent predicates (ANDed). Empty criteria match
// every entry.
func (w *catWalk) matches(path string, info fs.FileInfo) bool {
	_ = info
	if w.crit.MatchParent {
		if parentStorePath(path) != w.crit.ParentPath {
			return false
		}
	}
	if w.crit.MatchName {
		base := baseStorePath(path)
		if w.crit.Partial {
			if !strings.Contains(strings.ToLower(base), strings.ToLower(w.crit.Name)) {
				return false
			}
		} else if !strings.EqualFold(base, w.crit.Name) {
			return false
		}
	}
	return true
}

// --- cursor codec (flat 4-byte big-endian visit index) ---

func decodeWalkCursor(c CatSearchCursor) int {
	if len(c) < 4 {
		return 0
	}
	return int(c[0])<<24 | int(c[1])<<16 | int(c[2])<<8 | int(c[3])
}

func encodeWalkCursor(n int) CatSearchCursor {
	return CatSearchCursor{byte(n >> 24), byte(n >> 16), byte(n >> 8), byte(n)}
}

// --- store-path helpers (store paths are always '/'-joined) ---

func joinStorePath(dir, elem string) string {
	if dir == "" {
		return elem
	}
	return dir + "/" + elem
}

func parentStorePath(path string) string {
	i := strings.LastIndexByte(path, '/')
	if i < 0 {
		return ""
	}
	return path[:i]
}

func baseStorePath(path string) string {
	i := strings.LastIndexByte(path, '/')
	if i < 0 {
		return path
	}
	return path[i+1:]
}

// isMetadataShadow reports whether a store-native name is a metadata shadow that
// must not surface as a search hit: an AppleDouble "._" sidecar, or the EA /
// stream shadow paths the fork engines address through the FileSystem. It mirrors
// the file service's own metadata-hiding so a default walk does not return fork
// containers as files.
func isMetadataShadow(name string) bool {
	if strings.HasPrefix(name, "._") {
		return true
	}
	if strings.Contains(name, "\x00ea\x00") {
		return true
	}
	if strings.Contains(name, ":AFP_") {
		return true
	}
	return false
}
