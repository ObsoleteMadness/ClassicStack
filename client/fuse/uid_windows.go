//go:build windows

package fuse

func currentUIDGID() (uid, gid uint32) { return 0, 0 }
