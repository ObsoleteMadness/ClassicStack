# ClassicStack patches to cgofuse

Based on [github.com/winfsp/cgofuse](https://github.com/winfsp/cgofuse) v1.6.0
(the `fuse` package only; examples and CI scaffolding omitted).

## Darwin xattr `position` (resource forks)

Upstream `host_cgo.go` receives the macOS `getxattr`/`setxattr` `position`
argument and drops it (`"OSX uses position only for the resource fork; we do
not support it!"`). Finder, `cp`, and `ditto` chunk `com.apple.ResourceFork`
through that offset; ignoring it corrupts or truncates large resource forks
on a MacFUSE mount.

Added:

- **`FileSystemXattrP`** (`fsop.go`) — optional `GetxattrSize` / `GetxattrP` /
  `SetxattrP`. `GetxattrSize` answers the size=0 probe without reading the
  value. `GetxattrP(path, name, position, size)` returns at most `size` bytes
  at `position` (ranged AFP FPRead). `SetxattrP` writes `value` at `position`.
- **`host.go`** — `hostGetxattr` / `hostSetxattr` dispatch to
  `FileSystemXattrP` when implemented. A size=0 Get calls `GetxattrSize` (so
  `ls` does not download resource forks). A sized Get copies the ranged
  result (no `ERANGE`) so a large resource fork is read in FUSE chunks.
  Without the interface the original `ERANGE` size-discovery behaviour is
  unchanged.
- **`host_cgo.go`** — Darwin `_hostSetxattr` / `_hostGetxattr` pass `position`
  through to Go; Linux wrappers pass `0`.
- **`host_nocgo_windows.go`** — Windows has no position; callers pass `0`.
