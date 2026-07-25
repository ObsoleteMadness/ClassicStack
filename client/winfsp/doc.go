// Package winfsp mounts a remote ClassicStack share (any scheme the client SDK speaks —
// AFP, SMB, NCP, EtherDFS) as a Windows filesystem via github.com/winfsp/go-winfsp.
//
// The adapter targets ONE interface — core/fs.ForkFS, the unified VFS the client SDK's
// client.Connect returns for every protocol — so a single implementation serves all four.
// It is written against go-winfsp's low-level Behaviour* delegate layer (not the high-
// level gofs wrapper), because only the raw layer lets us populate the full
// FSP_FSCTL_FILE_INFO from a ForkFS's DOS attributes and dates rather than a bare
// os.FileInfo.
//
// Resource forks are NOT surfaced through a mount-specific stream convention: the fork
// backend is chosen once at client.Connect (the -fork flag → appledouble / applesingle /
// derez / passthrough / …), and the mount reflects whatever namespace that backend
// produces (e.g. AppleDouble "._name" sidecars appear as ordinary files). go-winfsp does
// not expose NTFS alternate-data-stream enumeration or EA get/set, so those are not mapped.
//
// Ring: CLIENT. Windows-only; the non-Windows build is a stub returning ErrUnsupported.
package winfsp
