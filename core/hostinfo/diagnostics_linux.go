//go:build linux && !tinygo

package hostinfo

import (
	"bufio"
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

var (
	lastUserStat    uint64
	lastNiceStat    uint64
	lastSystemStat  uint64
	lastIdleStat    uint64
	lastIowaitStat  uint64
	lastIrqStat     uint64
	lastSoftirqStat uint64
	linuxCPUMu      sync.Mutex
	linuxCPUInit    bool
)

func getCPULoad() float64 {
	linuxCPUMu.Lock()
	defer linuxCPUMu.Unlock()

	f, err := os.Open("/proc/stat")
	if err != nil {
		return 0
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		return 0
	}
	line := scanner.Text()
	fields := strings.Fields(line)
	if len(fields) < 8 || fields[0] != "cpu" {
		return 0
	}

	var user, nice, system, idle, iowait, irq, softirq uint64
	user, _ = strconv.ParseUint(fields[1], 10, 64)
	nice, _ = strconv.ParseUint(fields[2], 10, 64)
	system, _ = strconv.ParseUint(fields[3], 10, 64)
	idle, _ = strconv.ParseUint(fields[4], 10, 64)
	iowait, _ = strconv.ParseUint(fields[5], 10, 64)
	irq, _ = strconv.ParseUint(fields[6], 10, 64)
	softirq, _ = strconv.ParseUint(fields[7], 10, 64)

	if !linuxCPUInit {
		lastUserStat = user
		lastNiceStat = nice
		lastSystemStat = system
		lastIdleStat = idle
		lastIowaitStat = iowait
		lastIrqStat = irq
		lastSoftirqStat = softirq
		linuxCPUInit = true
		return 0
	}

	userDiff := user - lastUserStat
	niceDiff := nice - lastNiceStat
	systemDiff := system - lastSystemStat
	idleDiff := idle - lastIdleStat
	iowaitDiff := iowait - lastIowaitStat
	irqDiff := irq - lastIrqStat
	softirqDiff := softirq - lastSoftirqStat

	lastUserStat = user
	lastNiceStat = nice
	lastSystemStat = system
	lastIdleStat = idle
	lastIowaitStat = iowait
	lastIrqStat = irq
	lastSoftirqStat = softirq

	idleTicks := idleDiff + iowaitDiff
	totalTicks := userDiff + niceDiff + systemDiff + idleTicks + irqDiff + softirqDiff

	if totalTicks == 0 {
		return 0
	}

	return float64(totalTicks-idleTicks) / float64(totalTicks) * 100.0
}

func getMemoryInfo() (total uint64, free uint64) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		key := strings.TrimSuffix(fields[0], ":")
		val, _ := strconv.ParseUint(fields[1], 10, 64)
		if key == "MemTotal" {
			total = val * 1024 // kB to bytes
		} else if key == "MemAvailable" {
			free = val * 1024
		} else if key == "MemFree" && free == 0 {
			free = val * 1024
		}
	}
	return total, free
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
