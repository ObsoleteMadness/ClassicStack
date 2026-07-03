//go:build tinygo

package hostinfo

import (
	"runtime"
)

func getCPULoad() float64 {
	return 0
}

func getMemoryInfo() (total uint64, free uint64) {
	return 0, 0
}

func getHostIPAndMAC() (string, string) {
	return hostIP, hostMACAddress
}

func getOSName() string {
	return "TinyGo"
}

func getGoVersion() string {
	return "Go 1.23+ (via TinyGo)"
}

func getTinyGoVersion() string {
	return runtime.Version()
}
