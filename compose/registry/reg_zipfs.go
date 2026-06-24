//go:build afp || smb || all

package registry

// Blank-import the zipfs filesystem backend so its init() registers the "zipfs"
// fs_type into the core/fs factory registry. The package self-selects by build tag:
// under `zipfs`/`all` it links the real archive/zip-backed backend; in a file-service
// build WITHOUT `zipfs` it links only the tiny disabled stub, which registers an
// fs_type that errors "rebuild with -tags zipfs". Either way a config naming
// fs_type="zipfs" gets a clear answer. Kept under afp||smb||all so a build with no
// file service links neither. Mirrors reg_macgarden.go.
import _ "github.com/ObsoleteMadness/ClassicStack/adapter/zipfs"
