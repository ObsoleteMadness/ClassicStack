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
// Resource forks are surfaced through the fork backend chosen at client.Connect
// (-fork). On a native-fork protocol (AFP) a sidecar backend (derez / appledouble)
// PROJECTS those forks into the mount namespace as ordinary sidecar files
// (.rdump/.idump, ._name, …) so Windows tools can read them — the inverse of the
// server-hosting case, where the same adapters consume sidecars from a local disk.
// go-winfsp does not expose NTFS alternate-data-stream enumeration or EA get/set, so
// those are not mapped as streams.
//
// Ring: CLIENT. Windows-only; the non-Windows build is a stub returning ErrUnsupported.
package winfsp
