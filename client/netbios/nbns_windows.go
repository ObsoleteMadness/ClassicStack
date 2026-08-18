//go:build windows

package netbios

import "golang.org/x/sys/windows"

func setBroadcastFD(fd uintptr) error {
	return windows.SetsockoptInt(windows.Handle(fd), windows.SOL_SOCKET, windows.SO_BROADCAST, 1)
}
