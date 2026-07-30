# ClassicStack patches to go-winfsp

Based on [github.com/winfsp/go-winfsp](https://github.com/winfsp/go-winfsp) v1.0.3
(root package + `filetime` subpackage only).

## FileInfoTimeout option

Upstream `Mount` zero-initializes `FSP_FSCTL_VOLUME_PARAMS` and never sets
`FileInfoTimeout`, so the WinFsp FSD performs no metadata caching (every
`GetFileInfo` / attribute probe round-trips to usermode).

Added `FileInfoTimeout(ms uint32) Option` and assign it in `Mount`.

## Named streams + extended attributes (EA)

Ported the named-stream (`GetStreamInfo`) and extended-attribute
(`GetEa` / `SetEa`) support from a newer upstream revision, which had split
these out into `filesystem_windows.go` / `gohelper_windows.go`. This fork keeps
its v1.0.3-based single-file `host_windows.go` layout, so the port lives in a
new `ea_stream_windows.go` and is wired into the existing `FileSystemRef` /
`Mount` seam. ClassicStack uses these to expose native fork storage through NTFS
named streams and SMB EAs from the `csmount` mount tool.

Added:

- **`ea_stream_windows.go`** — helpers `FileSystemAddStreamInfo`,
  `FileSystemAddEa`, `FileSystemGetEaPackedSize`, `EnumerateEa`; behaviour
  interfaces `BehaviourGetStreamInfo(Raw)`, `BehaviourGetEa(Raw)`,
  `BehaviourSetEa(Raw)`; and the `GetStreamInfo` / `GetEa` / `SetEa` cgo
  delegates.
- **`host_windows.go`** — three fields on `FileSystemRef`
  (`getStreamInfoRaw` / `getEaRaw` / `setEaRaw`) and their `Mount` wiring, which
  sets `FspFSAttributeNamedStreams` / `FspFSAttributeExtendedAttributes` when the
  respective behaviour is implemented. When `FileInfoTimeout` is non-zero, the
  `StreamInfoTimeout` / `EaTimeout` params (+ their `FileSystemAttribute2` valid
  bits) are also set so stream/EA metadata is cached instead of round-tripping.

Struct fixes required by the ported helpers:

- **`FSP_FSCTL_STREAM_INFO`** / **`FSP_FSCTL_NOTIFY_INFO`** — dropped the trailing
  `*uint16` flexible-array-member placeholder fields. They inflated
  `unsafe.Sizeof` and broke `FileSystemAddStreamInfo`'s offset math
  (`FSP_FSCTL_STREAM_INFO` must be exactly 24 bytes). The variable-length
  name buffer is written past the struct by hand.
- **`FILE_FULL_EA_INFORMATION.EaValueLength`** — was `int16`; corrected to
  `uint16` to match the Windows `USHORT` and the EA helpers.
