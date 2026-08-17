// Package fuse mounts a remote ClassicStack share (any scheme the client SDK speaks —
// AFP, SMB, NCP, EtherDFS) as a FUSE filesystem via github.com/winfsp/cgofuse.
//
// The adapter targets ONE interface — core/fs.ForkFS — so a single implementation
// serves every protocol. Resource forks and Finder metadata are presented as
// host-native extended attributes when Options.NativeForks is set:
//
//   - Darwin (macFUSE): com.apple.FinderInfo + com.apple.ResourceFork, plus the
//     virtual path file/..namedfork/rsrc.
//   - Linux (libfuse): user.org.netatalk.Metadata + user.org.netatalk.ResourceFork,
//     the Netatalk ea=sys layout (spec/16 §1c).
//
// Sidecar backends (appledouble, derez, …) PROJECT forks into the mount namespace
// as ordinary files via core/fs fork_export — the inverse of the server-hosting
// case. NativeForks is then left off so the two presentations are not doubled.
//
// The adapter itself is cgo-free and testable in-process. MountAt (the cgofuse
// host) is compiled only with `-tags fuse` and cgo, so `go test ./...` stays
// green on machines without macFUSE/libfuse headers.
//
// Ring: CLIENT.
package fuse
