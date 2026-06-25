//go:build forknative && !darwin

package native

// On a non-Darwin host there is no native com.apple.FinderInfo attribute, so the native
// adapter reports Finder info as absent and drops writes. The resource fork is still
// served from the "..namedfork/rsrc" stream path where the host filesystem supports it
// (engine.go); a host without native forks simply has none, which is valid (data-only).
// An operator needing portable Finder info on such a host should choose the appledouble
// or xattr fork backend instead.

func (e *nativeForkEngine) ReadFinderInfo(path string) (info [32]byte, ok bool, err error) {
	_ = path
	return [32]byte{}, false, nil
}

func (e *nativeForkEngine) WriteFinderInfo(path string, info [32]byte) error {
	_ = path
	_ = info
	return nil
}
