//go:build !windows && !linux && !darwin && !tinygo

package hostinfo

import (
	"net"
	"runtime"
)

func getCPULoad() float64             { return 0 }
func getMemoryInfo() (uint64, uint64) { return 0, 0 }

func detectHostIPAndMAC() (string, string) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", ""
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil || len(addrs) == 0 {
			continue
		}
		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String(), iface.HardwareAddr.String()
			}
		}
	}
	return "", ""
}

func getHostIPAndMAC() (string, string) {
	if hostIP != "" {
		return hostIP, hostMACAddress
	}
	return detectHostIPAndMAC()
}

func getOSName() string {
	return runtime.GOOS
}

func getGoVersion() string {
	return runtime.Version()
}

func getTinyGoVersion() string {
	return ""
}
