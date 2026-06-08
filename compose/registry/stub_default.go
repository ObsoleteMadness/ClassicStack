//go:build !registrytag

package registry

// taggedRegistered reflects whether the build-tag-gated factory is present in this build.
// Without the `registrytag` build tag, stub_tagged.go is excluded, so nothing registers
// "stub-tagged" and the conformance test expects ok=false.
const taggedRegistered = false
