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

- **`FileSystemXattrP`** (`fsop.go`) — optional `GetxattrP` / `SetxattrP`
  taking a `position uint32`. `GetxattrP` still returns the **full** attribute
  value; the host applies the offset when copying into the FUSE buffer and
  uses `len(value)` as the size-probe result. `SetxattrP` writes `value` at
  offset `position`.
- **`host.go`** — `hostGetxattr` / `hostSetxattr` dispatch to
  `FileSystemXattrP` when implemented. With it, a Get that does not fit the
  caller buffer is copied in chunks (no `ERANGE`) so a large resource fork
  does not have to be allocated in one shot by the kernel. Without it the
  original `ERANGE` size-discovery behaviour is unchanged.
- **`host_cgo.go`** — Darwin `_hostSetxattr` / `_hostGetxattr` pass `position`
  through to Go; Linux wrappers pass `0`.
- **`host_nocgo_windows.go`** — Windows has no position; callers pass `0`.
