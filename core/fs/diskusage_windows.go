//go:build windows

package fs

import (
	"syscall"
	"unsafe"
)

// diskUsage queries the host volume backing path via GetDiskFreeSpaceExW,
// returning the total and free byte counts. It loads the call from kernel32 the
// same way the Go stdlib does internally, so core/fs needs no x/sys dependency.
// The free figure is the space available to the calling user (lpFreeBytesAvailable,
// which honours per-user quotas) — what the file server should advertise.
func diskUsage(path string) (total, free uint64, err error) {
	pathPtr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, err
	}
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	getDiskFreeSpaceEx := kernel32.NewProc("GetDiskFreeSpaceExW")
	var freeToCaller, totalBytes, totalFree uint64
	r1, _, callErr := getDiskFreeSpaceEx.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		uintptr(unsafe.Pointer(&freeToCaller)),
		uintptr(unsafe.Pointer(&totalBytes)),
		uintptr(unsafe.Pointer(&totalFree)),
	)
	if r1 == 0 {
		return 0, 0, callErr
	}
	return totalBytes, freeToCaller, nil
}
