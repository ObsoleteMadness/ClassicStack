//go:build afp || smb || all

package registry

// Blank-import the MacGarden filesystem backend so its init() registers the
// "macgarden" fs_type into the core/fs factory registry. The package self-selects by
// build tag: under `macgarden`/`all` it links the real HTTP-scraper backend (and the
// x/net/html parser); in a file-service build WITHOUT `macgarden` it links only the
// tiny disabled stub, which registers an fs_type that errors "rebuild with -tags
// macgarden". Either way a config naming fs_type="macgarden" gets a clear answer. Kept
// under afp||smb||all so a build with no file service links neither.
import _ "github.com/ObsoleteMadness/ClassicStack/adapter/macgarden"
