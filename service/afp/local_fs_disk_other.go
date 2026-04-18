//go:build !windows

package afp

import "syscall"

func diskUsage(path string) (totalBytes uint64, freeBytes uint64, err error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, 0, err
	}

	blockSize := uint64(stat.Bsize)
	totalBytes = uint64(stat.Blocks) * blockSize
	freeBytes = uint64(stat.Bavail) * blockSize
	return totalBytes, freeBytes, nil
}
