//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package fs

import "syscall"

// diskUsage queries the host volume backing path via statfs(2), returning the
// total and free byte counts. It uses stdlib syscall only (no x/sys) so core/fs
// stays dependency-light; an OS not covered by a build-tagged file falls back to
// diskusage_other.go (0/0, unknown). The free figure is the unprivileged free
// space (blocks available to a non-root caller, Bavail), which is what a file
// server should advertise — it is what a client can actually write into.
func diskUsage(path string) (total, free uint64, err error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0, err
	}
	// Bsize is the fundamental block size; multiply by the block counts. Cast to
	// uint64 first: on some platforms Bsize/Blocks are signed (int64) or 32-bit.
	bsize := uint64(st.Bsize)
	total = uint64(st.Blocks) * bsize
	free = uint64(st.Bavail) * bsize
	return total, free, nil
}
