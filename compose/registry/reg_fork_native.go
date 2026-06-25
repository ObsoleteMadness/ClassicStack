//go:build forknative

package registry

// Blank-import the host-native fork adapter so its init() registers the "native"
// fork_backend into the core/fs fork-adapter registry, replacing the core stub
// (core/fs/fork_native_stub.go, which is !forknative). Gated by the `forknative` build
// tag so a build without it never links the host resource-fork syscalls / x/sys — the
// same tag-gated, self-registering pattern the fs backends use (reg_zipfs.go). A build
// with `forknative` gets the real adapter; one without gets the stub's "rebuild with
// -tags forknative" error if a share asks for fork_backend="native".
import _ "github.com/ObsoleteMadness/ClassicStack/adapter/fork/native"
