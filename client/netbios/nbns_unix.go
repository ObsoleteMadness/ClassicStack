//go:build unix && !tinygo

package netbios

import "golang.org/x/sys/unix"

func setBroadcastFD(fd uintptr) error {
	return unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_BROADCAST, 1)
}
