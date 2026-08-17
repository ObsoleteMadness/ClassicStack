package fuse

import (
	"os"

	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
)

func (a *Adapter) getattrNamedFork(base string, kind namedKind) (Stat, error) {
	if !a.nativeForks {
		return Stat{}, os.ErrNotExist
	}
	fi, err := a.fsys.Stat(base)
	if err != nil {
		return Stat{}, err
	}
	st := a.fillStat(base, fi)
	if kind == namedForkDir {
		st.IsDir = true
		st.Mode = 0o755
		st.Size = 0
		st.Flags = 0
		return st, nil
	}
	n, err := a.fsys.ForkLen(base, fs.ResourceFork)
	if err != nil {
		return Stat{}, err
	}
	st.IsDir = false
	st.Mode = 0o644
	st.Size = n
	st.Flags = 0
	return st, nil
}

func (a *Adapter) openNamedFork(base string, kind namedKind, flags int) (uint64, error) {
	if !a.nativeForks {
		return 0, os.ErrNotExist
	}
	if kind == namedForkDir {
		h := &openFile{path: joinStore(base, namedForkDirName), isDir: true, flag: os.O_RDONLY}
		fh := a.handles.add(h)
		return fh, nil
	}
	flag := a.flagFor(flags)
	if flag != os.O_RDONLY {
		flag |= os.O_CREATE
	}
	f, err := a.fsys.OpenFork(base, fs.ResourceFork, flag)
	if err != nil {
		trace("Open namedfork %q → err=%v", base, err)
		return 0, err
	}
	h := &openFile{path: base, f: f, flag: flag, rsrc: true}
	fh := a.handles.add(h)
	trace("Open namedfork %q → fh=%d", base, fh)
	a.log.Log2(log.Debug, "fuse open namedfork", log.Str("path", base), log.Int("fh", int64(fh)))
	return fh, nil
}
