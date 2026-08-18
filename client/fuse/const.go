package fuse

import "errors"

// Apple / Netatalk xattr names. Darwin uses the Apple pair; Linux uses the
// Netatalk pair (with a user. prefix advertised to getfattr). Logical names
// without the user. prefix match core/fs.NetatalkMetadataEA.
const (
	xattrAppleFinderInfo   = "com.apple.FinderInfo"
	xattrAppleResourceFork = "com.apple.ResourceFork"

	xattrNetatalkMetadata     = "org.netatalk.Metadata"
	xattrNetatalkResourceFork = "org.netatalk.ResourceFork"
	xattrUserPrefix           = "user."

	// namedForkDirName / namedForkRsrcName are the HFS+ virtual path
	// components: <file>/..namedfork/rsrc.
	namedForkDirName  = "..namedfork"
	namedForkRsrcName = "rsrc"
)

// Finder fdFlags bit (big-endian uint16 at FInfo offset 8): file is invisible.
const fdFlagsInvisible uint16 = 0x4000

// UF_HIDDEN is the Darwin/BSD st_flags bit we set so ls/Finder hide the file.
const ufHidden uint32 = 0x00008000

// errNoAttr is returned when a requested xattr is absent (mapped to ENOATTR /
// ENODATA by the cgofuse host).
var errNoAttr = errors.New("fuse: no such attribute")

// errIsDir is returned when a file operation is applied to a directory.
var errIsDir = errors.New("fuse: is a directory")

// errNotDir is returned when a directory operation is applied to a file.
var errNotDir = errors.New("fuse: not a directory")

// macFUSE iosize (bytes) is the I/O block size of the hypothetical backing
// device. A slow link (AFP over EtherTalk) stays more responsive with the
// smallest legal value: 16 KiB on Apple Silicon, 4 KiB on Intel. Must be a
// power of two; default without -oiosize is 64 KiB.
const (
	fuseIOSizeDarwinARM64 = 16384
	fuseIOSizeDarwinIntel = 4096
)
