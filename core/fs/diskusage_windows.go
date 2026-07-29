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
	// unsafe.Pointer is mandatory to pass the UTF-16 path and the output
	// counters to the Win32 GetDiskFreeSpaceExW syscall; this is the standard
	// syscall-interop pattern (mirrors the Go stdlib) with no pointer arithmetic.
	r1, _, callErr := getDiskFreeSpaceEx.Call(
		uintptr(unsafe.Pointer(pathPtr)),       // #nosec G103 -- Win32 syscall interop
		uintptr(unsafe.Pointer(&freeToCaller)), // #nosec G103 -- Win32 syscall interop
		uintptr(unsafe.Pointer(&totalBytes)),   // #nosec G103 -- Win32 syscall interop
		uintptr(unsafe.Pointer(&totalFree)),    // #nosec G103 -- Win32 syscall interop
	)
	if r1 == 0 {
		return 0, 0, callErr
	}
	return totalBytes, freeToCaller, nil
}
