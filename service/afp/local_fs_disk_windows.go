//go:build windows

package afp

import "golang.org/x/sys/windows"

func diskUsage(path string) (totalBytes uint64, freeBytes uint64, err error) {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, err
	}

	var freeAvailable uint64
	var total uint64
	var totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(pathPtr, &freeAvailable, &total, &totalFree); err != nil {
		return 0, 0, err
	}

	return total, totalFree, nil
}
