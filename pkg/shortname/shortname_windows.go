//go:build windows

package shortname

import (
	"path/filepath"
	"syscall"
)

// getWindowsShortName invokes the native GetShortPathName API.
func getWindowsShortName(path string) (string, error) {
	pathP, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}
	n, err := syscall.GetShortPathName(pathP, nil, 0)
	if n == 0 {
		return "", err
	}
	buf := make([]uint16, n)
	n, err = syscall.GetShortPathName(pathP, &buf[0], uint32(len(buf)))
	if n == 0 {
		return "", err
	}
	return filepath.Base(syscall.UTF16ToString(buf)), nil
}
