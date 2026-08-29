//go:build unix

package fuse

import "os"

func currentUIDGID() (uid, gid uint32) {
	return uint32(os.Getuid()), uint32(os.Getgid())
}
