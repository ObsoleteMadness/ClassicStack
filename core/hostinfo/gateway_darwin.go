package hostinfo

import (
	"net"
	"os/exec"
	"strings"
)

// gateway_darwin.go resolves the default gateway via `route -n get default`, whose
// "gateway:" line carries the next hop. macOS exposes the routing table through the
// PF_ROUTE socket, but the route(8) tool is a stable, dependency-free way to read the
// default route without wiring the sysctl/route-message ABI into core.

func defaultGateway() (net.IP, error) {
	out, err := exec.Command("route", "-n", "get", "default").Output()
	if err != nil {
		return nil, ErrNoDefaultGateway
	}
	for line := range strings.SplitSeq(string(out), "\n") {
		line = strings.TrimSpace(line)
		rest, ok := strings.CutPrefix(line, "gateway:")
		if !ok {
			continue
		}
		ip := net.ParseIP(strings.TrimSpace(rest)).To4()
		if ip == nil || ip.IsUnspecified() {
			return nil, ErrNoDefaultGateway
		}
		return ip, nil
	}
	return nil, ErrNoDefaultGateway
}
