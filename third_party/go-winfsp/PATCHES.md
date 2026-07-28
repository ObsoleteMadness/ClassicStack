# ClassicStack patches to go-winfsp

Based on [github.com/winfsp/go-winfsp](https://github.com/winfsp/go-winfsp) v1.0.3
(root package + `filetime` subpackage only).

## FileInfoTimeout option

Upstream `Mount` zero-initializes `FSP_FSCTL_VOLUME_PARAMS` and never sets
`FileInfoTimeout`, so the WinFsp FSD performs no metadata caching (every
`GetFileInfo` / attribute probe round-trips to usermode).

Added `FileInfoTimeout(ms uint32) Option` and assign it in `Mount`.
