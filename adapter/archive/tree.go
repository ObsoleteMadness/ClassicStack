package archive

import "strings"

// fileRec is one pending file entry: its data fork, and (contributed
// separately — a wrapper's payload and its metadata sidecar can arrive via
// two different calls) an optional resource fork + Finder info.
type fileRec struct {
	data     []byte
	resource []byte
	finder   [32]byte
}

// treeBuilder assembles a flat set of '/'-separated, archive-relative file
// and directory paths into the nested []Node tree Expand returns. Shared by
// every format that has to reconstruct a hierarchy from a flat entry list
// (zip.go, stuffit.go) so the tree-assembly logic — parent-directory
// backfill, recursive Node nesting — exists exactly once.
type treeBuilder struct {
	files map[string]*fileRec
	dirs  map[string]struct{}
}

func newTreeBuilder() *treeBuilder {
	return &treeBuilder{files: map[string]*fileRec{}, dirs: map[string]struct{}{}}
}

// ensureParentDirs records every ancestor directory of path, so a file placed
// deep in the tree gets its full chain of parent Nodes even when the source
// archive never listed the intermediate directories explicitly.
func (b *treeBuilder) ensureParentDirs(path string) {
	parts := strings.Split(path, "/")
	for i := 1; i < len(parts); i++ {
		b.dirs[strings.Join(parts[:i], "/")] = struct{}{}
	}
}

// addDir records an explicit directory entry.
func (b *treeBuilder) addDir(path string) {
	if path == "" {
		return
	}
	b.ensureParentDirs(path)
	b.dirs[path] = struct{}{}
}

func (b *treeBuilder) rec(path string) *fileRec {
	r := b.files[path]
	if r == nil {
		r = &fileRec{}
		b.files[path] = r
	}
	return r
}

// setData records a file's data fork, creating the record if this is the
// first fork seen for path.
func (b *treeBuilder) setData(path string, data []byte) {
	b.ensureParentDirs(path)
	b.rec(path).data = data
}

// setResource records a file's resource fork + Finder info, creating the
// record if this is the first fork seen for path.
func (b *treeBuilder) setResource(path string, resource []byte, finder [32]byte) {
	b.ensureParentDirs(path)
	r := b.rec(path)
	r.resource = resource
	r.finder = finder
}

// roots assembles the accumulated files/dirs into the nested []Node tree.
func (b *treeBuilder) roots() []Node {
	var roots []Node
	seen := map[string]struct{}{}
	for path := range b.dirs {
		if _, ok := seen[path]; ok {
			continue
		}
		if n := b.buildTree(path, seen); n != nil {
			roots = append(roots, *n)
		}
	}
	for path, r := range b.files {
		if strings.Contains(path, "/") {
			continue
		}
		if _, ok := b.dirs[path]; ok {
			continue
		}
		roots = append(roots, Node{
			Name:       path,
			Data:       r.data,
			Resource:   r.resource,
			FinderInfo: r.finder,
		})
	}
	return roots
}

func (b *treeBuilder) buildTree(path string, seen map[string]struct{}) *Node {
	if _, ok := seen[path]; ok {
		return nil
	}
	seen[path] = struct{}{}
	name := path
	if i := strings.LastIndex(path, "/"); i >= 0 {
		name = path[i+1:]
	}
	prefix := path + "/"
	var kids []Node
	for p := range b.dirs {
		if !strings.HasPrefix(p, prefix) || strings.Contains(p[len(prefix):], "/") {
			continue
		}
		if child := b.buildTree(p, seen); child != nil {
			kids = append(kids, *child)
		}
	}
	for p, r := range b.files {
		if !strings.HasPrefix(p, prefix) || strings.Contains(p[len(prefix):], "/") {
			continue
		}
		base := p[len(prefix):]
		kids = append(kids, Node{
			Name:       base,
			Data:       r.data,
			Resource:   r.resource,
			FinderInfo: r.finder,
		})
	}
	return &Node{Name: name, IsDir: true, Children: kids}
}
