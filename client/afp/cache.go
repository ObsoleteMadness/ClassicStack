package afp

import (
	stdfs "io/fs"
	"sync"
	"time"
)

// cache.go is a short-TTL Stat/ReadDir cache for the AFP client. Windows (via WinFsp)
// re-probes the same paths relentlessly — the csmount-vmac capture shows ~3280
// FPGetFileDirParms vs ~108 for a classic Mac client on a comparable browse — so a
// few hundred milliseconds of caching collapses duplicate probes without serving
// stale listings across real mutations (Create/Remove/Rename/Write invalidate).

const cacheTTL = 500 * time.Millisecond

type cacheEntry[T any] struct {
	at  time.Time
	val T
	err error
}

type diskUsage struct {
	total, free uint64
}

// cache holds per-FS Stat and ReadDir results. Embedded in FS; nil-safe when unused.
type attrCache struct {
	mu    sync.Mutex
	stats map[string]cacheEntry[stdfs.FileInfo]
	dirs  map[string]cacheEntry[[]stdfs.DirEntry]
	disk  cacheEntry[diskUsage]
}

func (c *attrCache) getStat(path string) (stdfs.FileInfo, error, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.stats[path]
	if !ok || time.Since(e.at) > cacheTTL {
		return nil, nil, false
	}
	e.at = time.Now()
	c.stats[path] = e
	return e.val, e.err, true
}

func (c *attrCache) putStat(path string, fi stdfs.FileInfo, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stats == nil {
		c.stats = make(map[string]cacheEntry[stdfs.FileInfo])
	}
	c.stats[path] = cacheEntry[stdfs.FileInfo]{at: time.Now(), val: fi, err: err}
}

func (c *attrCache) getDir(path string) ([]stdfs.DirEntry, error, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.dirs[path]
	if !ok || time.Since(e.at) > cacheTTL {
		return nil, nil, false
	}
	e.at = time.Now()
	c.dirs[path] = e
	return e.val, e.err, true
}

func (c *attrCache) putDir(path string, ents []stdfs.DirEntry, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.dirs == nil {
		c.dirs = make(map[string]cacheEntry[[]stdfs.DirEntry])
	}
	c.dirs[path] = cacheEntry[[]stdfs.DirEntry]{at: time.Now(), val: ents, err: err}
}

func (c *attrCache) getDisk() (total, free uint64, err error, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.disk.at.IsZero() || time.Since(c.disk.at) > cacheTTL {
		return 0, 0, nil, false
	}
	return c.disk.val.total, c.disk.val.free, c.disk.err, true
}

func (c *attrCache) putDisk(total, free uint64, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.disk = cacheEntry[diskUsage]{at: time.Now(), val: diskUsage{total: total, free: free}, err: err}
}

// invalidate drops cached entries that may be affected by a mutation at path
// (the path itself, its parent directory listing, and — for renames — both sides).
func (c *attrCache) invalidate(paths ...string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, p := range paths {
		delete(c.stats, p)
		delete(c.dirs, p)
		dir, _ := splitPath(p)
		delete(c.dirs, dir)
		delete(c.stats, dir)
	}
	c.disk = cacheEntry[diskUsage]{}
}

func (c *attrCache) invalidateAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stats = nil
	c.dirs = nil
	c.disk = cacheEntry[diskUsage]{}
}
