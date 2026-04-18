//go:build !linux

package rawlink

import "fmt"

// OpenTAP opens a TAP-backed raw link.
//
// This is a portable stub; platform-specific TAP support can replace this
// implementation in future files with build tags.
func OpenTAP(devName string) (RawLink, error) {
	return nil, fmt.Errorf("rawlink: tap backend is not implemented on this platform/device (%s)", devName)
}
