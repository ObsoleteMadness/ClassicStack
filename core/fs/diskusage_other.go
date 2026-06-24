//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris && !windows

package fs

// diskUsage on an OS without a build-tagged statfs/GetDiskFreeSpaceEx query
// (and on TinyGo, whose syscall has no Statfs) reports 0/0 — "unknown". The
// AFP/SMB/NCP volume-info handlers treat 0/0 as a single nominal unit, so a
// mount succeeds and shows a non-empty volume rather than failing. This keeps
// core/fs compiling on every target the file services must reach (the cs-tinygo
// gate links core/service/{afp,smb}, which pull core/fs).
func diskUsage(path string) (total, free uint64, err error) {
	_ = path
	return 0, 0, nil
}
