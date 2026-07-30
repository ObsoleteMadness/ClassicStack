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
// (-fork). There are two modes:
//
//   - Sidecar backends (derez / appledouble) PROJECT forks into the mount namespace
//     as ordinary sidecar files (.rdump/.idump, ._name, …) so Windows tools can read
//     them — the inverse of the server-hosting case, where the same adapters consume
//     sidecars from a local disk.
//   - Native forks (-fork native → Options.NativeForks) surface a file's resource fork
//     and Apple metadata as NTFS named streams, using the stream names NT Services for
//     Macintosh defines — :AFP_Resource, :AFP_AfpInfo, :Comments (streams_windows.go).
//     This is the SFM/AFP-server layout, so the Windows shell and SMB redirector see the
//     same streams a real server exposes. When native forks are off the mount has no
//     streams and a ':stream' path is rejected as invalid.
//
// Ring: CLIENT. Windows-only; the non-Windows build is a stub returning ErrUnsupported.
package winfsp
