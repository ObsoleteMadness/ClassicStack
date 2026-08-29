//go:build darwin && !tinygo

package hostinfo

import (
	"bufio"
	"net"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

func getCPULoad() float64 {
	out, err := exec.Command("top", "-l", "1", "-n", "0").Output()
	if err != nil {
		return 0
	}
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		if strings.Contains(line, "CPU usage:") {
			fields := strings.Fields(line)
			for i, field := range fields {
				if field == "idle" && i > 0 {
					idleStr := strings.TrimSuffix(fields[i-1], "%")
					if idlePct, err := strconv.ParseFloat(idleStr, 64); err == nil {
						return 100.0 - idlePct
					}
				}
			}
		}
	}
	return 0
}

func getMemoryInfo() (total uint64, free uint64) {
	out, err := exec.Command("sysctl", "-n", "hw.memsize").Output()
	if err == nil {
		t, err := strconv.ParseUint(strings.TrimSpace(string(out)), 10, 64)
		if err == nil {
			total = t
		}
	}

	out, err = exec.Command("vm_stat").Output()
	if err == nil {
		var pageSize uint64 = 4096
		scanner := bufio.NewScanner(strings.NewReader(string(out)))
		for scanner.Scan() {
			line := scanner.Text()
			if strings.Contains(line, "page size of") {
				fields := strings.Fields(line)
				if len(fields) >= 8 {
					if pSize, err := strconv.ParseUint(fields[7], 10, 64); err == nil {
						pageSize = pSize
					}
				}
			}
			if strings.HasPrefix(line, "Pages free:") {
				fields := strings.Fields(line)
				if len(fields) >= 3 {
					valStr := strings.TrimSuffix(fields[2], ".")
					if pages, err := strconv.ParseUint(valStr, 10, 64); err == nil {
						free = pages * pageSize
					}
				}
			}
		}
	}
	if free == 0 && total > 0 {
		free = total / 2
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
