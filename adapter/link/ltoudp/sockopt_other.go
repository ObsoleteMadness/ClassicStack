//go:build !windows

package ltoudp

import (
	"syscall"

	"golang.org/x/sys/unix"
)

// setSockOptReuseAddr enables SO_REUSEADDR and SO_REUSEPORT so multiple
// LToUDP speakers on one host can bind UDP 1954 at once (spec/ltoudp.md:
// "SO_REUSEADDR and SO_REUSEPORT are a good idea"). Darwin in particular
// rejects a second bind of 0.0.0.0:1954 unless SO_REUSEPORT is set on the
// socket before bind; SO_REUSEADDR alone is not enough.
//
// SO_REUSEPORT comes from x/sys/unix, not syscall: the syscall package does not
// define it on every linux arch (notably linux/amd64), and the value is not
// uniform across the ones that do — 0xf on most linux arches but 0x200 on
// mips/sparc (the OpenWrt targets) and on the BSDs. x/sys/unix carries the
// correct per-GOOS/GOARCH value.
func setSockOptReuseAddr(fd uintptr) error {
	if err := syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1); err != nil {
		return err
	}
	// Best-effort: a kernel without SO_REUSEPORT still works for a single
	// speaker; the bind is what surfaces EADDRINUSE.
	_ = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, unix.SO_REUSEPORT, 1)
	return nil
}
