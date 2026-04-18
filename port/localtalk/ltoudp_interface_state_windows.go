//go:build windows

package localtalk

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

func multicastInterfaceOperStatus() (map[uint32]bool, error) {
	size := uint32(15 * 1024)
	flags := uint32(windows.GAA_FLAG_INCLUDE_ALL_INTERFACES |
		windows.GAA_FLAG_SKIP_ANYCAST |
		windows.GAA_FLAG_SKIP_MULTICAST |
		windows.GAA_FLAG_SKIP_DNS_SERVER)

	for attempts := 0; attempts < 3; attempts++ {
		buf := make([]byte, size)
		addrs := (*windows.IpAdapterAddresses)(unsafe.Pointer(&buf[0]))
		err := windows.GetAdaptersAddresses(windows.AF_INET, flags, 0, addrs, &size)
		if err == nil {
			states := make(map[uint32]bool)
			for addr := addrs; addr != nil; addr = addr.Next {
				states[addr.IfIndex] = addr.OperStatus == windows.IfOperStatusUp
			}
			return states, nil
		}
		if err != windows.ERROR_BUFFER_OVERFLOW {
			return nil, err
		}
		if size == 0 {
			size = 15 * 1024
		}
	}

	return nil, windows.ERROR_BUFFER_OVERFLOW
}
