// Package archive expands classic Mac transfer wrappers (ZIP, MacBinary, BinHex)
// into a neutral file tree suitable for writing through fs.ForkFS.
package archive

import (
	"errors"
	"strings"
)

var (
	// ErrUnsupportedFormat is returned when Expand cannot decode a wrapper.
	ErrUnsupportedFormat = errors.New("archive: unsupported format")
	// ErrCorrupt is returned when a wrapper fails validation.
	ErrCorrupt = errors.New("archive: corrupt or truncated")
)

// Node is one file or directory produced by Expand.
type Node struct {
	Name       string
	IsDir      bool
	Data       []byte
	Resource   []byte
	FinderInfo [32]byte
	Children   []Node
}

// Sniff reports whether name, Finder type/creator, or magic bytes look expandable.
func Sniff(name string, finderInfo [32]byte, data []byte) bool {
	ext := strings.ToLower(name)
	if strings.HasSuffix(ext, ".zip") || strings.HasSuffix(ext, ".sit") ||
		strings.HasSuffix(ext, ".hqx") || strings.HasSuffix(ext, ".bin") {
		return true
	}
	if len(finderInfo) >= 8 {
		t := string(finderInfo[0:4])
		if t == "SIT!" || t == "SIT5" || t == "SITD" {
			return true
		}
	}
	return isZip(data) || isMacBinary(data) || looksBinHex(data) || isStuffIt(data)
}

// Expand decodes one archive file into a flat or nested tree (single top-level nodes).
func Expand(name string, data, resource []byte, finderInfo [32]byte) ([]Node, error) {
	if n, err := expandZip(data); err == nil && n != nil {
		return n, nil
	} else if err != nil && !errors.Is(err, ErrUnsupportedFormat) {
		return nil, err
	}
	if n, err := expandMacBinary(data); err == nil && n != nil {
		return []Node{*n}, nil
	} else if err != nil && !errors.Is(err, ErrUnsupportedFormat) {
		return nil, err
	}
	if n, err := expandBinHex(data); err == nil && n != nil {
		return []Node{*n}, nil
	} else if err != nil && !errors.Is(err, ErrUnsupportedFormat) {
		return nil, err
	}
	if n, err := expandStuffIt(name, data, finderInfo); err == nil && n != nil {
		return n, nil
	} else if err != nil {
		return nil, err
	}
	_ = resource
	_ = name
	return nil, ErrUnsupportedFormat
}
