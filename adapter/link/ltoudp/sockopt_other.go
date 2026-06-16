//go:build !windows

package ltoudp

import "syscall"

// setSockOptReuseAddr enables SO_REUSEADDR so multiple participants on one host
// can bind the LToUDP group port simultaneously.
func setSockOptReuseAddr(fd uintptr) error {
	return syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
}
