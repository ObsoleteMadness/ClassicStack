//go:build !windows

package ltoudp

import "syscall"

// setSockOptReuseAddr enables SO_REUSEADDR and SO_REUSEPORT so multiple
// LToUDP speakers on one host can bind UDP 1954 at once (spec/ltoudp.md:
// "SO_REUSEADDR and SO_REUSEPORT are a good idea"). Darwin in particular
// rejects a second bind of 0.0.0.0:1954 unless SO_REUSEPORT is set on the
// socket before bind; SO_REUSEADDR alone is not enough.
func setSockOptReuseAddr(fd uintptr) error {
	if err := syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1); err != nil {
		return err
	}
	// Best-effort: a kernel without SO_REUSEPORT still works for a single
	// speaker; the bind is what surfaces EADDRINUSE.
	_ = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEPORT, 1)
	return nil
}
