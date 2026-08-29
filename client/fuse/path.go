package fuse

import (
	"errors"
	"path"
	"strings"
)

// errInvalidName is returned for a path that escapes the store root.
var errInvalidName = errors.New("fuse: invalid name")

// toStorePath converts a FUSE path ("/foo/bar", root "/") to the '/'-separated,
// root-is-"" store path core/fs uses.
func toStorePath(fusePath string) (string, error) {
	p := strings.TrimPrefix(fusePath, "/")
	if p == "" || p == "." {
		return "", nil
	}
	clean := path.Clean("/" + p)
	if clean == "/.." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
		return "", errInvalidName
	}
	return strings.TrimPrefix(clean, "/"), nil
}

func joinStore(dir, leaf string) string {
	if dir == "" {
		return leaf
	}
	return dir + "/" + leaf
}

// namedKind identifies a virtual HFS+ named-fork path.
type namedKind uint8

const (
	namedNone     namedKind = iota
	namedForkDir            // <file>/..namedfork
	namedForkRsrc           // <file>/..namedfork/rsrc
)

// splitNamedFork peels a trailing /..namedfork[/rsrc] off a store path.
func splitNamedFork(storePath string) (base string, kind namedKind) {
	if storePath == namedForkDirName {
		return "", namedForkDir
	}
	if storePath == namedForkDirName+"/"+namedForkRsrcName {
		return "", namedForkRsrc
	}
	if strings.HasSuffix(storePath, "/"+namedForkDirName+"/"+namedForkRsrcName) {
		return strings.TrimSuffix(storePath, "/"+namedForkDirName+"/"+namedForkRsrcName), namedForkRsrc
	}
	if strings.HasSuffix(storePath, "/"+namedForkDirName) {
		return strings.TrimSuffix(storePath, "/"+namedForkDirName), namedForkDir
	}
	return storePath, namedNone
}
