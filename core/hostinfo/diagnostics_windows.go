//go:build windows && !tinygo

package hostinfo

import (
	"net"
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

var (
	kernel32                 = syscall.NewLazyDLL("kernel32.dll")
	procGetSystemTimes       = kernel32.NewProc("GetSystemTimes")
	procGlobalMemoryStatusEx = kernel32.NewProc("GlobalMemoryStatusEx")

	lastIdle    FILETIME
	lastKernel  FILETIME
	lastUser    FILETIME
	cpuMu       sync.Mutex
	initialized bool
)

type FILETIME struct {
	LowDateTime  uint32
	HighDateTime uint32
}

type MEMORYSTATUSEX struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

func getCPULoad() float64 {
	cpuMu.Lock()
	defer cpuMu.Unlock()

	var idle, kernel, user FILETIME
	// unsafe.Pointer is mandatory to pass the FILETIME output structs to the
	// Win32 GetSystemTimes syscall; standard syscall-interop, no pointer math.
	ret, _, _ := procGetSystemTimes.Call(
		uintptr(unsafe.Pointer(&idle)),   // #nosec G103 -- Win32 syscall interop
		uintptr(unsafe.Pointer(&kernel)), // #nosec G103 -- Win32 syscall interop
		uintptr(unsafe.Pointer(&user)),   // #nosec G103 -- Win32 syscall interop
	)
	if ret == 0 {
		return 0
	}

	if !initialized {
		lastIdle = idle
		lastKernel = kernel
		lastUser = user
		initialized = true
		return 0
	}

	idleDiff := filetimeDiff(idle, lastIdle)
	kernelDiff := filetimeDiff(kernel, lastKernel)
	userDiff := filetimeDiff(user, lastUser)

	lastIdle = idle
	lastKernel = kernel
	lastUser = user

	total := kernelDiff + userDiff
	if total == 0 {
		return 0
	}

	if total < idleDiff {
		return 0
	}
	return float64(total-idleDiff) / float64(total) * 100.0
}

func filetimeDiff(newVal, oldVal FILETIME) uint64 {
	n := (uint64(newVal.HighDateTime) << 32) | uint64(newVal.LowDateTime)
	o := (uint64(oldVal.HighDateTime) << 32) | uint64(oldVal.LowDateTime)
	if n < o {
		return 0
	}
	return n - o
}

func getMemoryInfo() (total uint64, free uint64) {
	var memoryStatus MEMORYSTATUSEX
	// unsafe.Sizeof/Pointer are mandatory to size and pass the MEMORYSTATUSEX
	// struct to the Win32 GlobalMemoryStatusEx syscall; standard interop.
	memoryStatus.Length = uint32(unsafe.Sizeof(memoryStatus))                          // #nosec G103 -- Win32 syscall interop
	ret, _, _ := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&memoryStatus))) // #nosec G103 -- Win32 syscall interop
	if ret == 0 {
		return 0, 0
	}
	return memoryStatus.TotalPhys, memoryStatus.AvailPhys
}

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
