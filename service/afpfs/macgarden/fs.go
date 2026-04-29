//go:build (afp && macgarden) || all

// Package macgarden implements an AFP FileSystem backend that exposes
// macintoshgarden.org as a read-only volume tree (Apps/, Games/,
// search/). It plugs into the AFP FileSystem registry under the
// "macgarden" type and is gated behind the `macgarden` build tag.
//
// Lives in service/afpfs/ alongside future AFP filesystem backends so
// the core AFP package never imports any specific filesystem.
package macgarden

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/pgodw/omnitalk/netlog"
	"github.com/pgodw/omnitalk/service/afp"
	garden "github.com/pgodw/omnitalk/service/macgarden"
)

const macGardenEnumerateWindow = 10
const macGardenSearchPageSize = 20

type macGardenFileInfo struct {
	name    string
	size    int64
	mode    fs.FileMode
	modTime time.Time
	isDir   bool
}

func (i *macGardenFileInfo) Name() string       { return i.name }
func (i *macGardenFileInfo) Size() int64        { return i.size }
func (i *macGardenFileInfo) Mode() fs.FileMode  { return i.mode }
func (i *macGardenFileInfo) ModTime() time.Time { return i.modTime }
func (i *macGardenFileInfo) IsDir() bool        { return i.isDir }
func (i *macGardenFileInfo) Sys() any           { return nil }

type macGardenDirEntry struct{ info fs.FileInfo }

func (d macGardenDirEntry) Name() string               { return d.info.Name() }
func (d macGardenDirEntry) IsDir() bool                { return d.info.IsDir() }
func (d macGardenDirEntry) Type() fs.FileMode          { return d.info.Mode().Type() }
func (d macGardenDirEntry) Info() (fs.FileInfo, error) { return d.info, nil }

type macGardenCachedResult struct {
	Name string
	URL  string
}

type macGardenAsset struct {
	Name    string
	URL     string
	Size    int64
	Content []byte
}

type macGardenCategoryPageMeta struct {
	TotalCount     uint16
	PageSize       int
	LastPageNumber int
	LastPageCount  int
}

type macGardenFile struct {
	asset  macGardenAsset
	client *garden.Client
}

func (f *macGardenFile) ReadAt(p []byte, off int64) (n int, err error) {
	if off < 0 {
		return 0, fs.ErrInvalid
	}
	if len(f.asset.Content) > 0 {
		if off >= int64(len(f.asset.Content)) {
			return 0, io.EOF
		}
		n = copy(p, f.asset.Content[off:])
		if n < len(p) {
			return n, io.EOF
		}
		return n, nil
	}
	// ReadURLRange applies the client's maxRangeSize cap internally, so it may
	// return fewer bytes than len(p). Signal io.EOF only when the HTTP response
	// is shorter than the bytes we actually requested — meaning we hit real EOF,
	// not just the range cap. FPRead buffers are already bounded by the same cap
	// (via handleRead.maxReadSize), so for that path len(data)==len(p) always.
	// FPCopyFile re-reads in a loop, so getting n<len(p) with nil error is fine.
	requested := len(p)
	if max := f.client.MaxRangeSize(); max > 0 && requested > max {
		requested = max
	}
	data, readErr := f.client.ReadURLRange(f.asset.URL, off, len(p))
	if readErr != nil {
		return 0, fmt.Errorf("%w: %v", afp.ErrCopySourceReadEOF, readErr)
	}
	n = copy(p, data)
	if len(data) < requested {
		return n, io.EOF
	}
	return n, nil
}

func (f *macGardenFile) WriteAt(_ []byte, _ int64) (n int, err error) { return 0, fs.ErrPermission }
func (f *macGardenFile) Truncate(_ int64) error                       { return fs.ErrPermission }
func (f *macGardenFile) Close() error                                 { return nil }
func (f *macGardenFile) Sync() error                                  { return nil }
func (f *macGardenFile) Stat() (fs.FileInfo, error) {
	size := f.asset.Size
	if size == 0 && f.asset.URL != "" {
		if s, err := f.client.GetContentLength(f.asset.URL); err == nil {
			size = s
		}
	}
	return &macGardenFileInfo{name: filepath.Base(f.asset.Name), size: size, mode: 0o444, modTime: time.Now().UTC()}, nil
}

// fetchAndCacheScreenshot downloads a screenshot URL and stores it in the
// in-memory cache. Subsequent OpenFile calls serve from cache without network I/O.
func (m *MacGardenFileSystem) fetchAndCacheScreenshot(url string) ([]byte, error) {
	m.screenshotMu.RLock()
	if data, ok := m.screenshotCache[url]; ok {
		m.screenshotMu.RUnlock()
		return data, nil
	}
	m.screenshotMu.RUnlock()
	data, err := m.client.FetchFull(url)
	if err != nil {
		return nil, err
	}
	m.screenshotMu.Lock()
	m.screenshotCache[url] = data
	m.screenshotMu.Unlock()
	return data, nil
}

// resolveAssetSize returns the known size, or triggers a size fetch appropriate
// for the asset type. Called during FPGetFileDirParms so Finder sees the real size.
// Screenshots: full download cached in memory (avoids HEAD which gets blocked).
// Downloads:   ranged GET to read the Content-Range total only.
func (m *MacGardenFileSystem) resolveAssetSize(a macGardenAsset) int64 {
	if a.Size > 0 || a.URL == "" {
		return a.Size
	}
	if strings.HasPrefix(a.Name, "Screenshots/") {
		if data, err := m.fetchAndCacheScreenshot(a.URL); err == nil {
			return int64(len(data))
		}
		return 0
	}
	if s, err := m.client.GetContentLength(a.URL); err == nil {
		return s
	}
	return 0
}

// MacGardenFileSystem is a read-only virtual filesystem backed by macintoshgarden.org.
type macGardenSearchCache struct {
	pages     map[int][]garden.SearchResult // pageNumber -> results
	exhausted bool                          // true when all pages have been fetched
}

type MacGardenFileSystem struct {
	root   string
	client *garden.Client

	mu                sync.RWMutex
	categories        []garden.Category
	searchByName      map[string]macGardenCachedResult
	itemURLByDir      map[string]string
	itemByURL         map[string]*garden.SoftwareItem
	itemsInCategory   map[string][]garden.SearchResult // categoryURL -> items
	categoryItemCount map[string]uint16
	categoryPageMeta  map[string]macGardenCategoryPageMeta
	categoryPageItems map[string]map[int][]garden.SearchResult
	downloadByPath    map[string]macGardenAsset
	screenshotByPath  map[string]macGardenAsset
	descriptionByPath map[string]macGardenAsset
	catSearchCache    map[string]*macGardenSearchCache // normalized query -> cached results

	screenshotMu    sync.RWMutex
	screenshotCache map[string][]byte // URL -> full image bytes

	stop     chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

func init() {
	afp.RegisterFS(afp.FSTypeMacGarden, func(cfg afp.VolumeConfig) (afp.FileSystem, error) {
		return NewMacGardenFileSystem(filepath.Clean(cfg.Path)), nil
	})
}

func NewMacGardenFileSystem(root string) *MacGardenFileSystem {
	gc := garden.NewClient()
	gc.Prime()
	fsys := &MacGardenFileSystem{
		root:              filepath.Clean(root),
		client:            gc,
		searchByName:      make(map[string]macGardenCachedResult),
		itemURLByDir:      make(map[string]string),
		itemByURL:         make(map[string]*garden.SoftwareItem),
		itemsInCategory:   make(map[string][]garden.SearchResult),
		categoryItemCount: make(map[string]uint16),
		categoryPageMeta:  make(map[string]macGardenCategoryPageMeta),
		categoryPageItems: make(map[string]map[int][]garden.SearchResult),
		downloadByPath:    make(map[string]macGardenAsset),
		screenshotByPath:  make(map[string]macGardenAsset),
		descriptionByPath: make(map[string]macGardenAsset),
		catSearchCache:    make(map[string]*macGardenSearchCache),
		screenshotCache:   make(map[string][]byte),
		stop:              make(chan struct{}),
	}
	fsys.loadCategories()
	return fsys
}

func (m *MacGardenFileSystem) loadCategories() {
	m.mu.RLock()
	if len(m.categories) > 0 {
		m.mu.RUnlock()
		return
	}
	m.mu.RUnlock()
	cats, err := m.client.GetCategories()
	if err != nil {
		netlog.Warn("[AFP][MacGarden] failed to fetch categories: %v", err)
		return
	}
	sort.Slice(cats, func(i, j int) bool { return strings.ToLower(cats[i].Name) < strings.ToLower(cats[j].Name) })
	m.mu.Lock()
	if len(m.categories) == 0 {
		m.categories = cats
	}
	m.mu.Unlock()
	if len(cats) == 0 {
		netlog.Warn("[AFP][MacGarden] category fetch succeeded but returned no categories")
	}
}

func (m *MacGardenFileSystem) normalize(path string) (string, error) {
	clean := filepath.Clean(path)
	rel, err := filepath.Rel(m.root, clean)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fs.ErrPermission
	}
	if rel == "." {
		return "", nil
	}
	return filepath.ToSlash(rel), nil
}

// readDirCore resolves a normalized relative path to directory entries. It is
// the shared implementation used by both ReadDir and ReadDirRange. Callers are
// responsible for running it in a goroutine if a timeout is needed.
func (m *MacGardenFileSystem) readDirCore(rel string) ([]fs.DirEntry, error) {
	if rel == "" {
		netlog.Debug("[AFP][MacGarden] ReadDir root")
		return []fs.DirEntry{
			macGardenDirEntry{info: &macGardenFileInfo{name: "Apps", mode: fs.ModeDir | 0o555, isDir: true, modTime: time.Now().UTC()}},
			macGardenDirEntry{info: &macGardenFileInfo{name: "Games", mode: fs.ModeDir | 0o555, isDir: true, modTime: time.Now().UTC()}},
			macGardenDirEntry{info: &macGardenFileInfo{name: "search", mode: fs.ModeDir | 0o555, isDir: true, modTime: time.Now().UTC()}},
		}, nil
	}

	parts := strings.Split(rel, "/")

	// Apps or Games level: show categories for that type.
	if len(parts) == 1 && (parts[0] == "Apps" || parts[0] == "Games") {
		netlog.Debug("[AFP][MacGarden] ReadDir %s", parts[0])
		m.loadCategories()
		catType := parts[0]
		urlPrefix := "/apps/"
		if catType == "Games" {
			urlPrefix = "/games/"
		}
		m.mu.RLock()
		defer m.mu.RUnlock()
		entries := make([]fs.DirEntry, 0, len(m.categories))
		for _, cat := range m.categories {
			if strings.HasPrefix(strings.ToLower(urlPathFromAbsolute(cat.URL)), urlPrefix) {
				entries = append(entries, macGardenDirEntry{info: &macGardenFileInfo{name: cat.Name, mode: fs.ModeDir | 0o555, isDir: true, modTime: time.Now().UTC()}})
			}
		}
		netlog.Info("[AFP][MacGarden] ReadDir %s returning %d entries", catType, len(entries))
		return entries, nil
	}

	// /search — list all cached search queries as subdirectories.
	if len(parts) == 1 && parts[0] == "search" {
		m.mu.RLock()
		queries := slices.Sorted(maps.Keys(m.catSearchCache))
		m.mu.RUnlock()
		entries := make([]fs.DirEntry, 0, len(queries))
		for _, q := range queries {
			entries = append(entries, macGardenDirEntry{info: &macGardenFileInfo{name: q, mode: fs.ModeDir | 0o555, isDir: true, modTime: time.Now().UTC()}})
		}
		return entries, nil
	}

	// /search/<query> — list type subdirectories (App, Game) plus untyped items.
	if len(parts) == 2 && parts[0] == "search" {
		m.mu.RLock()
		cache, ok := m.catSearchCache[parts[1]]
		m.mu.RUnlock()
		if !ok {
			return nil, fs.ErrNotExist
		}
		pageNums := slices.Sorted(maps.Keys(cache.pages))
		typesSeen := map[string]struct{}{}
		untypedSeen := map[string]struct{}{}
		var typeNames, untypedNames []string
		for _, pn := range pageNums {
			for _, r := range cache.pages[pn] {
				if r.Type != "" {
					if _, exists := typesSeen[r.Type]; !exists {
						typesSeen[r.Type] = struct{}{}
						typeNames = append(typeNames, r.Type)
					}
				} else {
					if name := sanitizeGardenName(r.Name); name != "" {
						if _, exists := untypedSeen[name]; !exists {
							untypedSeen[name] = struct{}{}
							untypedNames = append(untypedNames, name)
						}
					}
				}
			}
		}
		sort.Strings(typeNames)
		sort.Strings(untypedNames)
		entries := make([]fs.DirEntry, 0, len(typeNames)+len(untypedNames))
		for _, name := range typeNames {
			entries = append(entries, macGardenDirEntry{info: &macGardenFileInfo{name: name, mode: fs.ModeDir | 0o555, isDir: true, modTime: time.Now().UTC()}})
		}
		for _, name := range untypedNames {
			entries = append(entries, macGardenDirEntry{info: &macGardenFileInfo{name: name, mode: fs.ModeDir | 0o555, isDir: true, modTime: time.Now().UTC()}})
		}
		return entries, nil
	}

	// /search/<query>/<type> — virtual type subdirectory (App/Game).
	if len(parts) == 3 && parts[0] == "search" && isSearchResultType(parts[2]) {
		m.mu.RLock()
		cache, ok := m.catSearchCache[parts[1]]
		m.mu.RUnlock()
		if !ok {
			return nil, fs.ErrNotExist
		}
		resultType := parts[2]
		var names []string
		for _, page := range cache.pages {
			for _, r := range page {
				if r.Type == resultType {
					if name := sanitizeGardenName(r.Name); name != "" {
						names = append(names, name)
					}
				}
			}
		}
		sort.Strings(names)
		entries := make([]fs.DirEntry, 0, len(names))
		for _, name := range names {
			entries = append(entries, macGardenDirEntry{info: &macGardenFileInfo{name: name, mode: fs.ModeDir | 0o555, isDir: true, modTime: time.Now().UTC()}})
		}
		return entries, nil
	}

	// /search/<query>/<item> — assets for that item.
	if len(parts) == 3 && parts[0] == "search" {
		itemName := parts[2]
		m.mu.RLock()
		search, ok := m.searchByName[itemName]
		m.mu.RUnlock()
		if !ok {
			return nil, fs.ErrNotExist
		}
		if err := m.ensureItemForDir(itemName, search.URL); err != nil {
			return nil, err
		}
		assets, err := m.itemAssetsByDir(itemName)
		if err != nil {
			return nil, err
		}
		return buildItemDirEntries(assets, ""), nil
	}

	// /search/<query>/<type>/<item>[/<subdir...>] — typed item or its subdirectory.
	if len(parts) >= 4 && parts[0] == "search" && isSearchResultType(parts[2]) {
		itemName := parts[3]
		subPath := filepath.ToSlash(filepath.Join(parts[4:]...))
		m.mu.RLock()
		search, ok := m.searchByName[itemName]
		m.mu.RUnlock()
		if !ok {
			return nil, fs.ErrNotExist
		}
		if err := m.ensureItemForDir(itemName, search.URL); err != nil {
			return nil, err
		}
		assets, err := m.itemAssetsByDir(itemName)
		if err != nil {
			return nil, err
		}
		return buildItemDirEntries(assets, subPath), nil
	}

	// /search/<query>/<item>/<subdir...> — subdirectory within an item.
	if len(parts) >= 4 && parts[0] == "search" {
		itemName := parts[2]
		subPath := filepath.ToSlash(filepath.Join(parts[3:]...))
		m.mu.RLock()
		search, ok := m.searchByName[itemName]
		m.mu.RUnlock()
		if !ok {
			return nil, fs.ErrNotExist
		}
		if err := m.ensureItemForDir(itemName, search.URL); err != nil {
			return nil, err
		}
		assets, err := m.itemAssetsByDir(itemName)
		if err != nil {
			return nil, err
		}
		return buildItemDirEntries(assets, subPath), nil
	}

	// Apps/Games/CategoryName/ItemName — assets for a software item
	if len(parts) == 3 && (parts[0] == "Apps" || parts[0] == "Games") {
		catName, itemName := parts[1], parts[2]
		catURL := m.getCategoryURL(catName)
		if catURL == "" {
			return nil, fs.ErrNotExist
		}
		itemURL, err := m.getItemURLInCategory(catURL, itemName)
		if err != nil {
			return nil, fs.ErrNotExist
		}
		if err := m.ensureItemForDir(itemName, itemURL); err != nil {
			return nil, err
		}
		assets, err := m.itemAssetsByDir(itemName)
		if err != nil {
			return nil, err
		}
		return buildItemDirEntries(assets, ""), nil
	}

	// Apps/Games/CategoryName/ItemName/SubDir... — subdirectory within an item
	if len(parts) >= 4 && (parts[0] == "Apps" || parts[0] == "Games") {
		catName, itemName := parts[1], parts[2]
		subPath := filepath.ToSlash(filepath.Join(parts[3:]...))
		catURL := m.getCategoryURL(catName)
		if catURL == "" {
			return nil, fs.ErrNotExist
		}
		itemURL, err := m.getItemURLInCategory(catURL, itemName)
		if err != nil {
			return nil, fs.ErrNotExist
		}
		if err := m.ensureItemForDir(itemName, itemURL); err != nil {
			return nil, err
		}
		assets, err := m.itemAssetsByDir(itemName)
		if err != nil {
			return nil, err
		}
		return buildItemDirEntries(assets, subPath), nil
	}

	return nil, fs.ErrNotExist
}

func (m *MacGardenFileSystem) ReadDir(path string) ([]fs.DirEntry, error) {
	rel, err := m.normalize(path)
	if err != nil {
		return nil, err
	}
	return m.readDirCore(rel)
}

func (m *MacGardenFileSystem) ReadDirRange(path string, startIndex uint16, reqCount uint16) ([]fs.DirEntry, uint16, error) {
	if reqCount == 0 {
		return nil, 0, nil
	}
	rel, err := m.normalize(path)
	if err != nil {
		return nil, 0, err
	}
	parts := strings.Split(rel, "/")
	if len(parts) == 1 && (parts[0] == "Apps" || parts[0] == "Games") {
		m.loadCategories()
		prefix := "/apps/"
		if parts[0] == "Games" {
			prefix = "/games/"
		}
		m.mu.RLock()
		filtered := make([]fs.DirEntry, 0, len(m.categories))
		for _, cat := range m.categories {
			if strings.HasPrefix(strings.ToLower(urlPathFromAbsolute(cat.URL)), prefix) {
				filtered = append(filtered, macGardenDirEntry{info: &macGardenFileInfo{name: cat.Name, mode: fs.ModeDir | 0o555, isDir: true, modTime: time.Now().UTC()}})
			}
		}
		m.mu.RUnlock()
		total := uint16(len(filtered))
		if startIndex < 1 {
			startIndex = 1
		}
		if int(startIndex) > len(filtered) {
			return nil, total, nil
		}
		start := int(startIndex) - 1
		end := start + int(reqCount)
		if end > len(filtered) {
			end = len(filtered)
		}
		return append([]fs.DirEntry(nil), filtered[start:end]...), total, nil
	}
	if len(parts) == 2 && (parts[0] == "Apps" || parts[0] == "Games") {
		catURL := m.getCategoryURL(parts[1])
		if catURL == "" {
			return nil, 0, fs.ErrNotExist
		}
		return m.readCategoryDirRange(catURL, startIndex, reqCount)
	}
	entries, err := m.readDirCore(rel)
	if err != nil {
		return nil, 0, err
	}
	total := uint16(len(entries))
	if startIndex < 1 {
		startIndex = 1
	}
	if int(startIndex) > len(entries) {
		return nil, total, nil
	}
	start := int(startIndex) - 1
	end := start + int(reqCount)
	if end > len(entries) {
		end = len(entries)
	}
	return append([]fs.DirEntry(nil), entries[start:end]...), total, nil
}

func (m *MacGardenFileSystem) Stat(path string) (fs.FileInfo, error) {
	rel, err := m.normalize(path)
	if err != nil {
		return nil, err
	}
	if rel == "" {
		return &macGardenFileInfo{name: filepath.Base(m.root), mode: fs.ModeDir | 0o555, isDir: true, modTime: time.Now().UTC()}, nil
	}

	parts := strings.Split(rel, "/")

	// Apps or Games level
	if len(parts) == 1 && (parts[0] == "Apps" || parts[0] == "Games") {
		return &macGardenFileInfo{name: parts[0], mode: fs.ModeDir | 0o555, isDir: true, modTime: time.Now().UTC()}, nil
	}

	// /search virtual directory
	if len(parts) == 1 && parts[0] == "search" {
		return &macGardenFileInfo{name: "search", mode: fs.ModeDir | 0o555, isDir: true, modTime: time.Now().UTC()}, nil
	}

	// /search/<query>
	if len(parts) == 2 && parts[0] == "search" {
		m.mu.RLock()
		_, ok := m.catSearchCache[parts[1]]
		m.mu.RUnlock()
		if ok {
			return &macGardenFileInfo{name: parts[1], mode: fs.ModeDir | 0o555, isDir: true, modTime: time.Now().UTC()}, nil
		}
		return nil, fs.ErrNotExist
	}

	// /search/<query>/<type> — virtual type subdirectory (App/Game)
	// /search/<query>/<item> — item directory
	if len(parts) == 3 && parts[0] == "search" {
		if isSearchResultType(parts[2]) {
			return &macGardenFileInfo{name: parts[2], mode: fs.ModeDir | 0o555, isDir: true, modTime: time.Now().UTC()}, nil
		}
		itemName := parts[2]
		m.mu.RLock()
		cache, ok := m.catSearchCache[parts[1]]
		m.mu.RUnlock()
		if !ok {
			return nil, fs.ErrNotExist
		}
		for _, page := range cache.pages {
			for _, r := range page {
				if sanitizeGardenName(r.Name) == itemName {
					return &macGardenFileInfo{name: itemName, mode: fs.ModeDir | 0o555, isDir: true, modTime: time.Now().UTC()}, nil
				}
			}
		}
		return nil, fs.ErrNotExist
	}

	// /search/<query>/<type>/<item>[/<asset...>] or /search/<query>/<item>/<asset...>
	if len(parts) >= 4 && parts[0] == "search" {
		var itemName, fileName string
		if isSearchResultType(parts[2]) {
			itemName = parts[3]
			fileName = strings.Join(parts[4:], "/")
		} else {
			itemName = parts[2]
			fileName = strings.Join(parts[3:], "/")
		}
		if fileName == "" {
			// It's the item directory itself under a type subdirectory
			return &macGardenFileInfo{name: itemName, mode: fs.ModeDir | 0o555, isDir: true, modTime: time.Now().UTC()}, nil
		}
		m.mu.RLock()
		search, ok := m.searchByName[itemName]
		loaded := false
		if ok {
			_, loaded = m.itemByURL[search.URL]
		}
		m.mu.RUnlock()
		if !ok || !loaded {
			return nil, fs.ErrNotExist
		}
		assets, err := m.itemAssetsByDir(itemName)
		if err != nil {
			return nil, err
		}
		for _, a := range assets {
			if a.Name == fileName {
				return &macGardenFileInfo{name: filepath.Base(a.Name), size: m.resolveAssetSize(a), mode: 0o444, modTime: time.Now().UTC()}, nil
			}
		}
		prefix := fileName + "/"
		for _, a := range assets {
			if strings.HasPrefix(a.Name, prefix) {
				return &macGardenFileInfo{name: filepath.Base(fileName), mode: fs.ModeDir | 0o555, isDir: true, modTime: time.Now().UTC()}, nil
			}
		}
		return nil, fs.ErrNotExist
	}

	// Search-hit item directory at root level (legacy, retained for compatibility).
	if len(parts) == 1 {
		m.mu.RLock()
		_, ok := m.searchByName[parts[0]]
		m.mu.RUnlock()
		if ok {
			return &macGardenFileInfo{name: parts[0], mode: fs.ModeDir | 0o555, isDir: true, modTime: time.Now().UTC()}, nil
		}
	}

	// Category level - return immediately without fetching items
	// Stat should be lightweight; items are fetched lazily only on ReadDir
	if len(parts) == 2 && (parts[0] == "Apps" || parts[0] == "Games") {
		catName := parts[1]
		catURL := m.getCategoryURL(catName)
		if catURL != "" {
			netlog.Debug("[AFP][MacGarden] Stat returning category (no lazy fetch): %s", catName)
			return &macGardenFileInfo{name: catName, mode: fs.ModeDir | 0o555, isDir: true, modTime: time.Now().UTC()}, nil
		}
		return nil, fs.ErrNotExist
	}

	// Item level - return immediately without fetching items
	if len(parts) == 3 && (parts[0] == "Apps" || parts[0] == "Games") {
		itemName := parts[2]
		// Don't fetch the item here; just return dir info
		// Real items are fetched lazily when ReadDir is called
		netlog.Debug("[AFP][MacGarden] Stat returning item (no lazy fetch): %s", itemName)
		return &macGardenFileInfo{name: itemName, mode: fs.ModeDir | 0o555, isDir: true, modTime: time.Now().UTC()}, nil
	}

	// macOS probes certain well-known system paths on every directory it visits.
	// Reject them quickly so we never trigger network fetches for them.
	macSystemNames := map[string]bool{
		"Configuration":           true,
		"Network Trash Folder":    true,
		"TheVolumeSettingsFolder": true,
		"Temporary Items":         true,
		".DS_Store":               true,
		"Icon\r":                  true,
	}
	if len(parts) >= 3 && macSystemNames[parts[len(parts)-1]] {
		return nil, fs.ErrNotExist
	}

	// Asset level (file)
	if len(parts) >= 4 && (parts[0] == "Apps" || parts[0] == "Games") {
		catName := parts[1]
		itemName := parts[2]
		fileName := strings.Join(parts[3:], "/")

		catURL := m.getCategoryURL(catName)
		if catURL == "" {
			return nil, fs.ErrNotExist
		}

		itemURL, err := m.getItemURLInCategory(catURL, itemName)
		if err != nil {
			return nil, fs.ErrNotExist
		}

		// Keep Stat lazy for item children: if the item has not been opened yet,
		// do not fetch details just to probe a potential child path.
		m.mu.RLock()
		_, loaded := m.itemByURL[itemURL]
		m.mu.RUnlock()
		if !loaded {
			return nil, fs.ErrNotExist
		}

		assets, err := m.itemAssetsByDir(itemName)
		if err != nil {
			return nil, err
		}

		for _, a := range assets {
			if a.Name == fileName {
				return &macGardenFileInfo{name: filepath.Base(a.Name), size: m.resolveAssetSize(a), mode: 0o444, modTime: time.Now().UTC()}, nil
			}
		}
		prefix := fileName + "/"
		for _, a := range assets {
			if strings.HasPrefix(a.Name, prefix) {
				return &macGardenFileInfo{name: filepath.Base(fileName), mode: fs.ModeDir | 0o555, isDir: true, modTime: time.Now().UTC()}, nil
			}
		}
	}

	// Asset-level file under root search-hit item dir: ItemName/Asset
	if len(parts) >= 2 && parts[0] != "Apps" && parts[0] != "Games" {
		itemName := parts[0]
		fileName := filepath.Join(parts[1:]...)
		m.mu.RLock()
		search, ok := m.searchByName[itemName]
		loaded := false
		if ok {
			_, loaded = m.itemByURL[search.URL]
		}
		m.mu.RUnlock()
		if !ok || !loaded {
			return nil, fs.ErrNotExist
		}
		assets, err := m.itemAssetsByDir(itemName)
		if err != nil {
			return nil, err
		}
		for _, a := range assets {
			if a.Name == fileName {
				return &macGardenFileInfo{name: a.Name, size: a.Size, mode: 0o444, modTime: time.Now().UTC()}, nil
			}
		}
	}

	return nil, fs.ErrNotExist
}

func (m *MacGardenFileSystem) DiskUsage(_ string) (totalBytes uint64, freeBytes uint64, err error) {
	return 0x20000000, 0x18000000, nil
}

func (m *MacGardenFileSystem) ChildCount(path string) (uint16, error) {
	rel, err := m.normalize(path)
	if err != nil {
		return 0, err
	}
	if rel == "" {
		return 3, nil // Apps + Games + search
	}

	m.loadCategories()
	parts := strings.Split(rel, "/")
	if len(parts) == 1 {
		switch parts[0] {
		case "Apps":
			return m.countCategoriesWithPrefix("/apps/"), nil
		case "Games":
			return m.countCategoriesWithPrefix("/games/"), nil
		}
	}
	if len(parts) == 2 && (parts[0] == "Apps" || parts[0] == "Games") {
		catURL := m.getCategoryURL(parts[1])
		if catURL == "" {
			return 0, nil
		}
		m.mu.RLock()
		if count, ok := m.categoryItemCount[catURL]; ok {
			m.mu.RUnlock()
			return count, nil
		}
		m.mu.RUnlock()
		// Category counts must remain fully lazy. Until a category has actually
		// been opened and its items fetched, report an unknown count as zero
		// rather than triggering remote requests during parent directory enumerate.
		return 0, nil
	}
	if len(parts) == 3 && (parts[0] == "Apps" || parts[0] == "Games") {
		itemName := parts[2]
		m.mu.RLock()
		itemURL := m.itemURLByDir[itemName]
		item := m.itemByURL[itemURL]
		m.mu.RUnlock()
		if item == nil {
			return 0, nil
		}
		assets, err := m.itemAssetsByDir(itemName)
		if err != nil {
			return 0, nil
		}
		return uint16(len(buildItemDirEntries(assets, ""))), nil
	}
	if len(parts) >= 4 && (parts[0] == "Apps" || parts[0] == "Games") {
		itemName := parts[2]
		subPath := strings.Join(parts[3:], "/")
		m.mu.RLock()
		itemURL := m.itemURLByDir[itemName]
		item := m.itemByURL[itemURL]
		m.mu.RUnlock()
		if item == nil {
			return 0, nil
		}
		assets, err := m.itemAssetsByDir(itemName)
		if err != nil {
			return 0, nil
		}
		return uint16(len(buildItemDirEntries(assets, subPath))), nil
	}
	if len(parts) >= 1 && parts[0] == "search" {
		switch len(parts) {
		case 1:
			// /search — number of cached queries.
			m.mu.RLock()
			n := uint16(len(m.catSearchCache))
			m.mu.RUnlock()
			return n, nil
		case 2:
			// /search/<query> — count distinct type dirs + untyped items.
			m.mu.RLock()
			cache, ok := m.catSearchCache[parts[1]]
			m.mu.RUnlock()
			if !ok {
				return 0, nil
			}
			typesSeen := map[string]struct{}{}
			untypedSeen := map[string]struct{}{}
			for _, page := range cache.pages {
				for _, r := range page {
					if r.Type != "" {
						typesSeen[r.Type] = struct{}{}
					} else if name := sanitizeGardenName(r.Name); name != "" {
						untypedSeen[name] = struct{}{}
					}
				}
			}
			return clampGardenCount(len(typesSeen) + len(untypedSeen)), nil
		case 3:
			// /search/<query>/<type> — count items of that type.
			if isSearchResultType(parts[2]) {
				m.mu.RLock()
				cache, ok := m.catSearchCache[parts[1]]
				m.mu.RUnlock()
				if !ok {
					return 0, nil
				}
				seen := map[string]struct{}{}
				for _, page := range cache.pages {
					for _, r := range page {
						if r.Type == parts[2] {
							if name := sanitizeGardenName(r.Name); name != "" {
								seen[name] = struct{}{}
							}
						}
					}
				}
				return clampGardenCount(len(seen)), nil
			}
			// /search/<query>/<item> — offspring count for item root.
			itemName := parts[2]
			m.mu.RLock()
			itemURL := m.itemURLByDir[itemName]
			item := m.itemByURL[itemURL]
			m.mu.RUnlock()
			if item == nil {
				return 0, nil
			}
			assets, err := m.itemAssetsByDir(itemName)
			if err != nil {
				return 0, nil
			}
			return uint16(len(buildItemDirEntries(assets, ""))), nil
		default:
			// /search/<query>/<type>/<item>[/<subdir...>] or /search/<query>/<item>/<subdir...>
			var itemName, subPath string
			if isSearchResultType(parts[2]) {
				itemName = parts[3]
				subPath = strings.Join(parts[4:], "/")
			} else {
				itemName = parts[2]
				subPath = strings.Join(parts[3:], "/")
			}
			m.mu.RLock()
			itemURL := m.itemURLByDir[itemName]
			item := m.itemByURL[itemURL]
			m.mu.RUnlock()
			if item == nil {
				return 0, nil
			}
			assets, err := m.itemAssetsByDir(itemName)
			if err != nil {
				return 0, nil
			}
			return uint16(len(buildItemDirEntries(assets, subPath))), nil
		}
	}
	if len(parts) == 1 {
		return 0, nil
	}
	return 0, &afp.NotSupportedError{Operation: "ChildCount"}
}

// DirAttributes returns AFP directory attribute bits for a path.
// /search is flagged invisible so it stays hidden from normal Finder browsing.
func (m *MacGardenFileSystem) DirAttributes(path string) (uint16, error) {
	rel, err := m.normalize(path)
	if err != nil {
		return 0, err
	}
	if rel == "search" {
		return afp.DirAttrInvisible, nil
	}
	return 0, nil
}

func (m *MacGardenFileSystem) IsReadOnly(_ string) (bool, error) {
	return true, nil
}

// SetMaxRangeSize limits each HTTP range request to at most n bytes.
// Called by the AFP service with the ASP quantum size so that reads from
// macintoshgarden.org never exceed what can fit in one ASP reply.
func (m *MacGardenFileSystem) SetMaxRangeSize(n int) {
	m.client.SetMaxRangeSize(n)
}

func (m *MacGardenFileSystem) SupportsCatSearch(_ string) (bool, error) {
	return true, nil
}

func (m *MacGardenFileSystem) Capabilities() afp.FileSystemCapabilities {
	return afp.FileSystemCapabilities{
		CatSearch:     true,
		ChildCount:    true,
		ReadDirRange:  true,
		DirAttributes: true,
		ReadOnlyState: true,
	}
}

func (m *MacGardenFileSystem) Close() error {
	m.stopOnce.Do(func() { close(m.stop) })
	m.wg.Wait()
	return nil
}

func (m *MacGardenFileSystem) CreateDir(_ string) error          { return fs.ErrPermission }
func (m *MacGardenFileSystem) CreateFile(_ string) (afp.File, error) { return nil, fs.ErrPermission }
func (m *MacGardenFileSystem) Remove(_ string) error             { return fs.ErrPermission }
func (m *MacGardenFileSystem) Rename(_, _ string) error          { return fs.ErrPermission }

// openAsset wraps an asset in a macGardenFile, populating Content from the
// in-memory screenshot cache when the image has already been downloaded.
func (m *MacGardenFileSystem) openAsset(a macGardenAsset) *macGardenFile {
	if strings.HasPrefix(a.Name, "Screenshots/") && a.URL != "" && len(a.Content) == 0 {
		m.screenshotMu.RLock()
		data, ok := m.screenshotCache[a.URL]
		m.screenshotMu.RUnlock()
		if ok {
			a.Content = data
			a.Size = int64(len(data))
		}
	}
	return &macGardenFile{asset: a, client: m.client}
}

func (m *MacGardenFileSystem) OpenFile(path string, flag int) (afp.File, error) {
	if flag&(os.O_WRONLY|os.O_RDWR|os.O_APPEND|os.O_CREATE|os.O_TRUNC) != 0 {
		return nil, fs.ErrPermission
	}
	rel, err := m.normalize(path)
	if err != nil {
		return nil, err
	}

	parts := strings.Split(rel, "/")

	// /search/<query>/[<type>/]<item>/<asset...>
	if len(parts) >= 4 && parts[0] == "search" {
		var itemName, fileName string
		if isSearchResultType(parts[2]) {
			if len(parts) < 5 {
				return nil, fs.ErrInvalid
			}
			itemName = parts[3]
			fileName = strings.Join(parts[4:], "/")
		} else {
			itemName = parts[2]
			fileName = strings.Join(parts[3:], "/")
		}
		m.mu.RLock()
		search, ok := m.searchByName[itemName]
		m.mu.RUnlock()
		if !ok {
			return nil, fs.ErrNotExist
		}
		if err := m.ensureItemForDir(itemName, search.URL); err != nil {
			return nil, fs.ErrNotExist
		}
		assets, err := m.itemAssetsByDir(itemName)
		if err != nil {
			return nil, err
		}
		for _, a := range assets {
			if a.Name == fileName {
				return m.openAsset(a), nil
			}
		}
		return nil, fs.ErrNotExist
	}

	// Must be asset level: Apps/Category/Item/Asset or deeper
	if len(parts) < 4 || (parts[0] != "Apps" && parts[0] != "Games") {
		return nil, fs.ErrInvalid
	}

	catName := parts[1]
	itemName := parts[2]
	fileName := strings.Join(parts[3:], "/")

	catURL := m.getCategoryURL(catName)
	if catURL == "" {
		return nil, fs.ErrNotExist
	}

	itemURL, err := m.getItemURLInCategory(catURL, itemName)
	if err != nil {
		return nil, fs.ErrNotExist
	}

	if err := m.ensureItemForDir(itemName, itemURL); err != nil {
		return nil, fs.ErrNotExist
	}

	assets, err := m.itemAssetsByDir(itemName)
	if err != nil {
		return nil, err
	}

	for _, a := range assets {
		if a.Name == fileName {
			return m.openAsset(a), nil
		}
	}
	return nil, fs.ErrNotExist
}

func (m *MacGardenFileSystem) CatSearch(_ string, query string, reqMatches int32, cursor [16]byte) ([]string, [16]byte, int32) {
	rawQuery := strings.TrimSpace(query)
	if rawQuery == "" {
		return nil, cursor, afp.ErrParamErr
	}
	normalizedQuery := normalizeMacGardenSearchQuery(rawQuery)
	if normalizedQuery == "" {
		return nil, cursor, afp.ErrParamErr
	}

	limit := int(reqMatches)
	if limit <= 0 {
		limit = 25
	}

	isContinuation := cursor[0] == 0x01
	cursorQueryHash := uint32(cursor[1])<<16 | uint32(cursor[2])<<8 | uint32(cursor[3])
	cursorOffset := uint32(cursor[4])<<24 | uint32(cursor[5])<<16 | uint32(cursor[6])<<8 | uint32(cursor[7])

	queryHash := uint32(0)
	if len(normalizedQuery) >= 3 {
		queryHash = uint32(normalizedQuery[0])<<16 | uint32(normalizedQuery[1])<<8 | uint32(normalizedQuery[2])
	} else if len(normalizedQuery) > 0 {
		for i := 0; i < len(normalizedQuery); i++ {
			queryHash = (queryHash << 8) | uint32(normalizedQuery[i])
		}
	}

	startIdx := 0
	if isContinuation && cursorQueryHash == queryHash {
		startIdx = int(cursorOffset)
	} else {
		netlog.Debug("[MacGarden][CatSearch] starting new search for %q", normalizedQuery)
	}

	// Determine which page startIdx falls on and skip to the right entry within it.
	firstPage := startIdx / macGardenSearchPageSize
	skipInFirst := startIdx % macGardenSearchPageSize

	type hit struct {
		result garden.SearchResult
		name   string
	}
	hits := make([]hit, 0, limit)
	exhausted := false

	for pageNum := firstPage; len(hits) < limit; pageNum++ {
		m.ensureSearchPage(normalizedQuery, pageNum)

		m.mu.RLock()
		cache := m.catSearchCache[normalizedQuery]
		var page []garden.SearchResult
		if cache != nil {
			page = cache.pages[pageNum]
			exhausted = cache.exhausted
		}
		m.mu.RUnlock()

		if len(page) == 0 {
			break
		}

		skip := 0
		if pageNum == firstPage {
			skip = skipInFirst
		}
		for i := skip; i < len(page) && len(hits) < limit; i++ {
			name := sanitizeGardenName(page[i].Name)
			if name != "" {
				hits = append(hits, hit{result: page[i], name: name})
			}
		}

		if len(page) < macGardenSearchPageSize || exhausted {
			break
		}
	}

	netlog.Debug("[MacGarden][CatSearch] query=%q startIdx=%d firstPage=%d skip=%d returned=%d exhausted=%v",
		normalizedQuery, startIdx, firstPage, skipInFirst, len(hits), exhausted)

	paths := make([]string, 0, len(hits))
	m.mu.Lock()
	for _, h := range hits {
		dir := h.name
		if h.result.Type != "" {
			dir = filepath.Join(h.result.Type, h.name)
		}
		paths = append(paths, filepath.Join(m.root, "search", normalizedQuery, dir))
		m.searchByName[h.name] = macGardenCachedResult{Name: h.result.Name, URL: h.result.URL}
		m.itemURLByDir[h.name] = h.result.URL
	}
	m.mu.Unlock()

	moreAvailable := len(hits) == limit || !exhausted

	nextCursor := [16]byte{}
	nextCursor[1] = byte((queryHash >> 16) & 0xFF)
	nextCursor[2] = byte((queryHash >> 8) & 0xFF)
	nextCursor[3] = byte(queryHash & 0xFF)
	if moreAvailable {
		nextCursor[0] = 0x01
		nextOffset := uint32(startIdx + len(hits))
		nextCursor[4] = byte((nextOffset >> 24) & 0xFF)
		nextCursor[5] = byte((nextOffset >> 16) & 0xFF)
		nextCursor[6] = byte((nextOffset >> 8) & 0xFF)
		nextCursor[7] = byte(nextOffset & 0xFF)
	}

	return paths, nextCursor, afp.NoErr
}

// ensureSearchPage fetches a single MacGarden search page into the cache if it
// is not already there. Marks the cache exhausted when the page is partial
// (fewer than macGardenSearchPageSize items) or returns an error.
func (m *MacGardenFileSystem) ensureSearchPage(normalizedQuery string, pageNum int) {
	m.mu.RLock()
	cache, ok := m.catSearchCache[normalizedQuery]
	if ok {
		if _, cached := cache.pages[pageNum]; cached {
			m.mu.RUnlock()
			return
		}
		if cache.exhausted {
			m.mu.RUnlock()
			return
		}
	}
	m.mu.RUnlock()

	netlog.Debug("[MacGarden][CatSearch] fetching search page %d for %q", pageNum, normalizedQuery)
	pageResults, err := m.client.GetSearchPage(normalizedQuery, pageNum)

	m.mu.Lock()
	cache, ok = m.catSearchCache[normalizedQuery]
	if !ok {
		cache = &macGardenSearchCache{pages: make(map[int][]garden.SearchResult)}
	}
	if _, alreadyCached := cache.pages[pageNum]; !alreadyCached {
		if err != nil {
			netlog.Warn("[MacGarden][CatSearch] page %d fetch failed for %q: %v", pageNum, normalizedQuery, err)
			cache.exhausted = true
		} else {
			cache.pages[pageNum] = pageResults
			if len(pageResults) < macGardenSearchPageSize {
				netlog.Debug("[MacGarden][CatSearch] page %d: %d results for %q (last page)", pageNum, len(pageResults), normalizedQuery)
				cache.exhausted = true
			} else {
				netlog.Debug("[MacGarden][CatSearch] page %d: %d results for %q", pageNum, len(pageResults), normalizedQuery)
			}
		}
		m.catSearchCache[normalizedQuery] = cache
	}
	m.mu.Unlock()
}

func normalizeMacGardenSearchQuery(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	lower := strings.ToLower(s)
	for _, marker := range []string{" type:app,game", " type:app", " type:game", "type:app,game", "type:app", "type:game"} {
		if idx := strings.Index(lower, marker); idx >= 0 {
			s = s[:idx]
			lower = strings.ToLower(s)
		}
	}
	quoted := extractQuotedSegments(s)
	if len(quoted) > 0 {
		best := ""
		bestScore := -1
		for _, q := range quoted {
			cand := cleanMacGardenCandidate(q)
			score := 0
			for _, r := range cand {
				if unicode.IsLetter(r) || unicode.IsDigit(r) {
					score++
				}
			}
			if score > bestScore {
				bestScore = score
				best = cand
			}
		}
		if best != "" {
			return best
		}
	}
	return cleanMacGardenCandidate(s)
}

func mirrorFolderForURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "mirror-unknown"
	}
	switch strings.ToLower(u.Host) {
	case "old.mac.gdn":
		return "mirror-old"
	case "download.macintoshgarden.org":
		return "mirror-download"
	default:
		return "mirror-unknown"
	}
}

func buildItemDirEntries(assets []macGardenAsset, subPath string) []fs.DirEntry {
	subPath = strings.Trim(strings.ReplaceAll(subPath, "\\", "/"), "/")
	dirSeen := make(map[string]struct{})
	fileSeen := make(map[string]struct{})
	entries := make([]fs.DirEntry, 0, len(assets))

	for _, a := range assets {
		name := strings.Trim(strings.ReplaceAll(a.Name, "\\", "/"), "/")
		if name == "" {
			continue
		}
		if subPath != "" {
			prefix := subPath + "/"
			if !strings.HasPrefix(name, prefix) {
				continue
			}
			name = strings.TrimPrefix(name, prefix)
			if name == "" {
				continue
			}
		}

		if idx := strings.Index(name, "/"); idx >= 0 {
			dirName := name[:idx]
			if dirName == "" {
				continue
			}
			if _, ok := dirSeen[dirName]; ok {
				continue
			}
			dirSeen[dirName] = struct{}{}
			entries = append(entries, macGardenDirEntry{info: &macGardenFileInfo{name: dirName, mode: fs.ModeDir | 0o555, isDir: true, modTime: time.Now().UTC()}})
			continue
		}

		if _, ok := fileSeen[name]; ok {
			continue
		}
		fileSeen[name] = struct{}{}
		entries = append(entries, macGardenDirEntry{info: &macGardenFileInfo{name: name, size: a.Size, mode: 0o444, modTime: time.Now().UTC()}})
	}

	sort.Slice(entries, func(i, j int) bool {
		return strings.ToLower(entries[i].Name()) < strings.ToLower(entries[j].Name())
	})
	return entries
}

func cleanMacGardenCandidate(s string) string {
	s = strings.NewReplacer("$", "", "@", " ", "\"", " ").Replace(s)
	s = strings.TrimSpace(s)
	s = strings.Trim(s, ".,:;()[]{}<>' ")
	s = strings.Join(strings.Fields(s), " ")
	if s == "" || s == "." {
		return ""
	}
	return s
}

func extractQuotedSegments(s string) []string {
	segments := make([]string, 0, 2)
	start := -1
	for i, r := range s {
		if r != '"' {
			continue
		}
		if start < 0 {
			start = i + 1
			continue
		}
		if start <= i {
			segments = append(segments, s[start:i])
		}
		start = -1
	}
	return segments
}

func (m *MacGardenFileSystem) ensureItemForDir(dirName string, fallbackURL string) error {
	dirName = strings.TrimSpace(pathBase(dirName))
	if dirName == "" {
		return fs.ErrNotExist
	}
	m.mu.RLock()
	itemURL := m.itemURLByDir[dirName]
	m.mu.RUnlock()
	if itemURL == "" {
		itemURL = fallbackURL
	}
	if itemURL == "" {
		return fs.ErrNotExist
	}

	m.mu.RLock()
	_, ok := m.itemByURL[itemURL]
	m.mu.RUnlock()
	if ok {
		return nil
	}

	item, err := m.client.GetSoftwareItem(itemURL)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.itemByURL[itemURL] = item
	m.itemURLByDir[dirName] = itemURL
	m.mu.Unlock()
	return nil
}

func (m *MacGardenFileSystem) itemAssetsByDir(dirName string) ([]macGardenAsset, error) {
	dirName = pathBase(dirName)
	m.mu.RLock()
	itemURL := m.itemURLByDir[dirName]
	item := m.itemByURL[itemURL]
	m.mu.RUnlock()
	if itemURL == "" || item == nil {
		return nil, fs.ErrNotExist
	}

	netlog.Info("[AFP][MacGarden] building assets for %q: %d screenshot(s), %d download group(s)", dirName, len(item.Screenshots), len(item.Downloads))
	assets := make([]macGardenAsset, 0, len(item.Downloads)+len(item.Screenshots)+2)
	txtPath := filepath.Join(dirName, "Description.txt")
	htmlPath := filepath.Join(dirName, "Description.html")
	descMac := strings.ReplaceAll(item.Description, "\n", "\r")
	txtBytes := []byte(descMac)
	htmlBytes := []byte("<html><body><pre>" + htmlEscape(item.Description) + "</pre></body></html>")
	assets = append(assets,
		macGardenAsset{Name: "Description.txt", Content: txtBytes, Size: int64(len(txtBytes))},
		macGardenAsset{Name: "Description.html", Content: htmlBytes, Size: int64(len(htmlBytes))},
	)

	m.mu.Lock()
	m.descriptionByPath[txtPath] = assets[0]
	m.descriptionByPath[htmlPath] = assets[1]
	m.mu.Unlock()

	// For each URL use the cached size if available; collect uncached URLs for
	// background probing so this function never blocks on network I/O.
	var needsProbe []string

	shotIdx := 1
	for _, shotURL := range item.Screenshots {
		if !strings.HasPrefix(shotURL, "http://") && !strings.HasPrefix(shotURL, "https://") {
			continue
		}
		name := fmt.Sprintf("Screenshots/Screenshot %02d %s", shotIdx, garden.FileNameFromURL(shotURL, "image"))
		size, cached := m.client.CachedContentLength(shotURL)
		if !cached {
			netlog.Debug("[AFP][MacGarden] screenshot %d/%d not yet cached, will probe in background", shotIdx, len(item.Screenshots))
			needsProbe = append(needsProbe, shotURL)
		} else {
			netlog.Debug("[AFP][MacGarden] screenshot %d size: %d bytes (cached)", shotIdx, size)
		}
		asset := macGardenAsset{Name: name, URL: shotURL, Size: size}
		assets = append(assets, asset)
		m.mu.Lock()
		m.screenshotByPath[filepath.Join(dirName, name)] = asset
		m.mu.Unlock()
		shotIdx++
	}

	for _, dl := range item.Downloads {
		for _, link := range dl.Links {
			if !strings.HasPrefix(link.URL, "http://") && !strings.HasPrefix(link.URL, "https://") {
				continue
			}
			// Skip MD5 checksum links — they are not downloadable files.
			if strings.Contains(link.URL, "arch_md5.php") {
				continue
			}
			base := garden.FileNameFromURL(link.URL, dl.Title)
			if base == "" {
				base = sanitizeGardenName(dl.Title)
			}
			name := mirrorFolderForURL(link.URL) + "/" + base
			size, cached := m.client.CachedContentLength(link.URL)
			if !cached {
				netlog.Debug("[AFP][MacGarden] download %q not yet cached, will probe in background", dl.Title)
				needsProbe = append(needsProbe, link.URL)
			} else {
				netlog.Debug("[AFP][MacGarden] download %q size: %d bytes (cached)", dl.Title, size)
			}
			asset := macGardenAsset{Name: name, URL: link.URL, Size: size}
			assets = append(assets, asset)
			m.mu.Lock()
			m.downloadByPath[filepath.Join(dirName, name)] = asset
			m.mu.Unlock()
		}
	}

	if len(needsProbe) > 0 && m.client.FetchHead() {
		netlog.Info("[AFP][MacGarden] probing %d uncached asset size(s) for %q in background", len(needsProbe), dirName)
		urls := needsProbe
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			for _, u := range urls {
				select {
				case <-m.stop:
					return
				default:
				}
				if _, err := m.client.HeadContentLength(u); err != nil {
					netlog.Warn("[AFP][MacGarden] background probe failed for %q: %v", u, err)
				}
			}
			netlog.Info("[AFP][MacGarden] background probe complete for %q", dirName)
		}()
	}

	netlog.Info("[AFP][MacGarden] built %d asset(s) for %q", len(assets), dirName)
	return assets, nil
}

func (m *MacGardenFileSystem) categoryByName(name string) (garden.Category, bool) {
	for _, c := range m.categories {
		if c.Name == name {
			return c, true
		}
	}
	return garden.Category{}, false
}

func (m *MacGardenFileSystem) getCategoryURL(catName string) string {
	m.loadCategories()
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, c := range m.categories {
		if c.Name == catName {
			return c.URL
		}
	}
	return ""
}

func (m *MacGardenFileSystem) getCategoryPageMeta(catURL string) (macGardenCategoryPageMeta, error) {
	m.mu.RLock()
	if meta, ok := m.categoryPageMeta[catURL]; ok {
		m.mu.RUnlock()
		return meta, nil
	}
	m.mu.RUnlock()

	info, err := m.client.GetCategoryPageInfo(catURL)
	if err != nil {
		return macGardenCategoryPageMeta{}, err
	}
	meta := macGardenCategoryPageMeta{
		TotalCount:     clampGardenCount(info.TotalCount),
		PageSize:       info.PageSize,
		LastPageNumber: info.LastPageNumber,
		LastPageCount:  info.LastPageCount,
	}
	m.mu.Lock()
	m.categoryPageMeta[catURL] = meta
	m.categoryItemCount[catURL] = meta.TotalCount
	m.cacheCategoryPageLocked(catURL, 0, info.FirstPage)
	if info.LastPageNumber > 0 {
		m.cacheCategoryPageLocked(catURL, info.LastPageNumber, info.LastPage)
	}
	m.mu.Unlock()
	return meta, nil
}

func (m *MacGardenFileSystem) getCategoryPage(catURL string, pageNumber int) ([]garden.SearchResult, error) {
	m.mu.RLock()
	if pages, ok := m.categoryPageItems[catURL]; ok {
		if items, ok := pages[pageNumber]; ok {
			cached := append([]garden.SearchResult(nil), items...)
			m.mu.RUnlock()
			return cached, nil
		}
	}
	m.mu.RUnlock()

	items, err := m.client.GetCategoryPage(catURL, pageNumber)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.cacheCategoryPageLocked(catURL, pageNumber, items)
	m.mu.Unlock()
	return append([]garden.SearchResult(nil), items...), nil
}

func (m *MacGardenFileSystem) cacheCategoryPageLocked(catURL string, pageNumber int, items []garden.SearchResult) {
	if _, ok := m.categoryPageItems[catURL]; !ok {
		m.categoryPageItems[catURL] = make(map[int][]garden.SearchResult)
	}
	cloned := append([]garden.SearchResult(nil), items...)
	m.categoryPageItems[catURL][pageNumber] = cloned
	for _, item := range cloned {
		name := sanitizeGardenName(item.Name)
		if name == "" {
			continue
		}
		m.itemURLByDir[name] = item.URL
	}
}

func (m *MacGardenFileSystem) readCategoryDirRange(catURL string, startIndex uint16, reqCount uint16) ([]fs.DirEntry, uint16, error) {
	if reqCount > macGardenEnumerateWindow {
		reqCount = macGardenEnumerateWindow
	}
	meta, err := m.getCategoryPageMeta(catURL)
	if err != nil {
		return nil, 0, err
	}
	total := meta.TotalCount
	if total == 0 {
		return nil, 0, nil
	}
	if startIndex < 1 {
		startIndex = 1
	}
	if startIndex > total {
		return nil, total, nil
	}
	if reqCount == 0 {
		return nil, total, nil
	}
	pageSize := meta.PageSize
	if pageSize <= 0 {
		return nil, total, nil
	}
	startOffset := int(startIndex) - 1
	endOffset := startOffset + int(reqCount)
	if endOffset > int(total) {
		endOffset = int(total)
	}
	firstPage := startOffset / pageSize
	lastPage := (endOffset - 1) / pageSize
	results := make([]garden.SearchResult, 0, endOffset-startOffset)
	for pageNumber := firstPage; pageNumber <= lastPage; pageNumber++ {
		items, err := m.getCategoryPage(catURL, pageNumber)
		if err != nil {
			return nil, total, err
		}
		pageStart := 0
		if pageNumber == firstPage {
			pageStart = startOffset - pageNumber*pageSize
		}
		pageEnd := len(items)
		if pageNumber == lastPage {
			pageLimit := endOffset - pageNumber*pageSize
			if pageLimit < pageEnd {
				pageEnd = pageLimit
			}
		}
		if pageStart < 0 {
			pageStart = 0
		}
		if pageStart > len(items) {
			pageStart = len(items)
		}
		if pageEnd < pageStart {
			pageEnd = pageStart
		}
		results = append(results, items[pageStart:pageEnd]...)
	}
	entries := make([]fs.DirEntry, 0, len(results))
	for _, item := range results {
		entries = append(entries, macGardenDirEntry{info: &macGardenFileInfo{name: sanitizeGardenName(item.Name), mode: fs.ModeDir | 0o555, isDir: true, modTime: time.Now().UTC()}})
	}
	return entries, total, nil
}

func (m *MacGardenFileSystem) getCategoryItems(catURL string) ([]garden.SearchResult, error) {
	netlog.Debug("[AFP][MacGarden] getCategoryItems for URL: %s", catURL)
	m.mu.RLock()
	if items, ok := m.itemsInCategory[catURL]; ok {
		m.mu.RUnlock()
		netlog.Debug("[AFP][MacGarden] getCategoryItems found %d cached items for %s", len(items), catURL)
		return items, nil
	}
	m.mu.RUnlock()

	meta, err := m.getCategoryPageMeta(catURL)
	if err != nil {
		netlog.Warn("[AFP][MacGarden] failed to fetch category page metadata: %v", err)
		return nil, err
	}

	netlog.Debug("[AFP][MacGarden] fetching all pages for category URL: %s", catURL)
	items := make([]garden.SearchResult, 0, int(meta.TotalCount))
	for pageNumber := 0; pageNumber <= meta.LastPageNumber; pageNumber++ {
		pageItems, err := m.getCategoryPage(catURL, pageNumber)
		if err != nil {
			netlog.Warn("[AFP][MacGarden] failed to fetch category page %d: %v", pageNumber, err)
			return nil, err
		}
		items = append(items, pageItems...)
	}

	netlog.Info("[AFP][MacGarden] got %d items from category %s", len(items), catURL)
	m.mu.Lock()
	m.itemsInCategory[catURL] = items
	m.categoryItemCount[catURL] = clampGardenCount(len(items))
	m.mu.Unlock()
	return items, nil
}

func clampGardenCount(count int) uint16 {
	if count <= 0 {
		return 0
	}
	if count > 0xffff {
		return 0xffff
	}
	return uint16(count)
}

func (m *MacGardenFileSystem) countCategoriesWithPrefix(prefix string) uint16 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	count := uint16(0)
	for _, cat := range m.categories {
		if strings.HasPrefix(strings.ToLower(urlPathFromAbsolute(cat.URL)), prefix) {
			count++
		}
	}
	return count
}

func (m *MacGardenFileSystem) getItemURLInCategory(catURL string, itemName string) (string, error) {
	// Fast path: if the item URL is already cached from prior ranged enumeration,
	// avoid forcing a full category crawl.
	m.mu.RLock()
	if cachedURL := m.itemURLByDir[itemName]; cachedURL != "" {
		m.mu.RUnlock()
		return cachedURL, nil
	}
	if cachedItems, ok := m.itemsInCategory[catURL]; ok {
		for _, item := range cachedItems {
			if sanitizeGardenName(item.Name) == itemName {
				m.mu.RUnlock()
				return item.URL, nil
			}
		}
	}
	if cachedPages, ok := m.categoryPageItems[catURL]; ok {
		for _, pageItems := range cachedPages {
			for _, item := range pageItems {
				if sanitizeGardenName(item.Name) == itemName {
					m.mu.RUnlock()
					return item.URL, nil
				}
			}
		}
	}
	m.mu.RUnlock()

	meta, err := m.getCategoryPageMeta(catURL)
	if err != nil {
		return "", err
	}

	for pageNumber := 0; pageNumber <= meta.LastPageNumber; pageNumber++ {
		pageItems, err := m.getCategoryPage(catURL, pageNumber)
		if err != nil {
			return "", err
		}
		for _, item := range pageItems {
			if sanitizeGardenName(item.Name) == itemName {
				return item.URL, nil
			}
		}
	}
	return "", fs.ErrNotExist
}

func isSearchResultType(s string) bool { return s == "App" || s == "Game" }

func sanitizeGardenName(s string) string {
	s = strings.TrimSpace(s)
	replacer := strings.NewReplacer(
		"\\", "_",
		"/", "_",
		":", "-",
		"*", "_",
		"?", "",
		"\"", "",
		"<", "(",
		">", ")",
		"|", "_",
	)
	s = replacer.Replace(s)
	if s == "" {
		return "Item"
	}
	return s
}

func htmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

func pathBase(s string) string {
	s = filepath.ToSlash(s)
	parts := strings.Split(s, "/")
	return parts[len(parts)-1]
}

func pathDir(s string) string {
	s = filepath.ToSlash(s)
	idx := strings.LastIndex(s, "/")
	if idx < 0 {
		return ""
	}
	return s[:idx]
}

func urlPathFromAbsolute(absURL string) string {
	u, err := url.Parse(absURL)
	if err != nil {
		return ""
	}
	return u.Path
}

var _ afp.FileSystem = (*MacGardenFileSystem)(nil)

var errMacGardenNotFound = errors.New("macgarden: not found")
