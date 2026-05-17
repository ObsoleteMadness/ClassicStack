//go:build !windows

package shortname

import "errors"

var errNotSupported = errors.New("native shortnames not supported on this platform")

func getWindowsShortName(path string) (string, error) {
	return "", errNotSupported
}
