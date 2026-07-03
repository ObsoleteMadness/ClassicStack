package hostinfo

import (
	"runtime"
)

type HostInfo struct {
	BoardName           string `json:"boardName"`
	EthernetAdapterType string `json:"ethernetAdapterType"`
	Architecture        string `json:"architecture"`

	// Basic diagnostics
	CPULoad        float64 `json:"cpuLoad"`
	TotalMemory    uint64  `json:"totalMemory"`
	FreeMemory     uint64  `json:"freeMemory"`
	HostIP         string  `json:"hostIp"`
	HostMACAddress string  `json:"hostMacAddress"`
	OSName         string  `json:"osName"`

	// Build data
	GoVersion     string `json:"goVersion"`
	TinyGoVersion string `json:"tinygoVersion,omitempty"`
	GitSHA        string `json:"gitSha"`
	Version       string `json:"version"`
	BuildDate     string `json:"buildDate"`
}

var (
	boardName           = "N/A"
	ethernetAdapterType = "N/A"
	architecture        = runtime.GOARCH

	hostIP         = ""
	hostMACAddress = ""

	version   = "0.0.0-dev"
	gitSHA    = "unknown"
	buildDate = "unknown"
)

// SetBoardInfo specifies the board name, ethernet adapter type, and CPU architecture.
func SetBoardInfo(board, eth, arch string) {
	if board != "" {
		boardName = board
	}
	if eth != "" {
		ethernetAdapterType = eth
	}
	if arch != "" {
		architecture = arch
	}
}

// SetHostNetworkInfo registers the host IP and MAC address (useful on microcontrollers).
func SetHostNetworkInfo(ip, mac string) {
	hostIP = ip
	hostMACAddress = mac
}

// SetBuildInfo embeds the link-time build version, commit, and build date.
func SetBuildInfo(ver, commit, date string) {
	if ver != "" {
		version = ver
	}
	if commit != "" {
		gitSHA = commit
	}
	if date != "" {
		buildDate = date
	}
}

// Get gathers all static and dynamic system details and returns a HostInfo snapshot.
func Get() HostInfo {
	totalMem, freeMem := getMemoryInfo()
	ip, mac := getHostIPAndMAC()
	return HostInfo{
		BoardName:           boardName,
		EthernetAdapterType: ethernetAdapterType,
		Architecture:        architecture,
		CPULoad:             getCPULoad(),
		TotalMemory:         totalMem,
		FreeMemory:          freeMem,
		HostIP:              ip,
		HostMACAddress:      mac,
		OSName:              getOSName(),
		GoVersion:           getGoVersion(),
		TinyGoVersion:       getTinyGoVersion(),
		GitSHA:              gitSHA,
		Version:             version,
		BuildDate:           buildDate,
	}
}
