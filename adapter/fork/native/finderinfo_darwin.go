//go:build (forknative || all) && darwin

package native

import (
	"golang.org/x/sys/unix"
)

// finderInfoXattr is the macOS extended-attribute name carrying the 32-byte Finder info
// (16 bytes FInfo + 16 bytes FXInfo) for a file — the same bytes AFP/SMB exchange.
const finderInfoXattr = "com.apple.FinderInfo"

// ReadFinderInfo reads the host file's com.apple.FinderInfo xattr. ok is false when the
// attribute is absent (a file with no Finder info), which is not an error.
func (e *nativeForkEngine) ReadFinderInfo(path string) (info [32]byte, ok bool, err error) {
	hp, resolved := e.hostPathOf(path)
	if !resolved {
		return [32]byte{}, false, nil
	}
	buf := make([]byte, 32)
	n, gerr := unix.Getxattr(hp, finderInfoXattr, buf)
	if gerr != nil {
		// ENOATTR / ENODATA / ENOTSUP all mean "no Finder info here" — report absent.
		return [32]byte{}, false, nil
	}
	if n < 32 {
		// A short attribute is malformed; treat as absent rather than surfacing garbage.
		return [32]byte{}, false, nil
	}
	copy(info[:], buf[:32])
	return info, true, nil
}

// WriteFinderInfo writes the 32-byte Finder info to the host file's
// com.apple.FinderInfo xattr.
func (e *nativeForkEngine) WriteFinderInfo(path string, info [32]byte) error {
	hp, resolved := e.hostPathOf(path)
	if !resolved {
		return nil
	}
	return unix.Setxattr(hp, finderInfoXattr, info[:], 0)
}
