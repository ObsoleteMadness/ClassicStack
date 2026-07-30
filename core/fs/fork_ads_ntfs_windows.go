//go:build windows

package fs

import (
	"strings"
	"syscall"
	"unsafe"
)

// This file installs the NTFS volume-type probe the "ads" fork backend needs
// (fork_ads.go's volumeIsNTFS seam), so core/fs stays syscall-free on other
// platforms — the same injected-seam pattern as hostNativeDOSAttr
// (dosattr_native_windows.go).
//
// We call kernel32!GetVolumePathNameW + GetVolumeInformationW through
// syscall.NewLazyDLL rather than golang.org/x/sys/windows: x/sys/windows
// transitively pulls encoding/binary → reflect, which the core ring forbids
// (§1 / archtest). stdlib syscall does not export these two, so we bind the
// procs directly; syscall is already a permitted core dependency (os pulls it).

var (
	kernel32DLL              = syscall.NewLazyDLL("kernel32.dll")
	procGetVolumePathNameW   = kernel32DLL.NewProc("GetVolumePathNameW")
	procGetVolumeInformation = kernel32DLL.NewProc("GetVolumeInformationW")
)

func init() {
	volumeIsNTFS = windowsVolumeIsNTFS
}

// windowsVolumeIsNTFS reports whether the volume backing hostPath is NTFS. ok is false
// when the volume type cannot be determined (a bad path, a syscall failure), so the
// caller can fail closed.
func windowsVolumeIsNTFS(hostPath string) (isNTFS bool, ok bool) {
	fsName, ok := volumeFilesystemName(hostPath)
	if !ok {
		return false, false
	}
	return strings.EqualFold(fsName, "NTFS"), true
}

// volumeFilesystemName resolves the volume mount root for hostPath and returns its
// filesystem name (e.g. "NTFS", "FAT32", "exFAT"). ok is false on any failure.
func volumeFilesystemName(hostPath string) (string, bool) {
	p, err := syscall.UTF16PtrFromString(hostPath)
	if err != nil {
		return "", false
	}
	// Resolve the volume mount point for the path first (GetVolumeInformation wants a
	// root path, not an arbitrary file path).
	var mount [260]uint16
	r1, _, _ := procGetVolumePathNameW.Call(
		uintptr(unsafe.Pointer(p)),
		uintptr(unsafe.Pointer(&mount[0])),
		uintptr(len(mount)),
	)
	if r1 == 0 {
		return "", false
	}
	var volName, fsName [261]uint16
	var serial, maxComponentLen, flags uint32
	r2, _, _ := procGetVolumeInformation.Call(
		uintptr(unsafe.Pointer(&mount[0])),
		uintptr(unsafe.Pointer(&volName[0])), uintptr(len(volName)),
		uintptr(unsafe.Pointer(&serial)),
		uintptr(unsafe.Pointer(&maxComponentLen)),
		uintptr(unsafe.Pointer(&flags)),
		uintptr(unsafe.Pointer(&fsName[0])), uintptr(len(fsName)),
	)
	if r2 == 0 {
		return "", false
	}
	return syscall.UTF16ToString(fsName[:]), true
}
