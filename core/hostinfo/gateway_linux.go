package hostinfo

import (
	"bufio"
	"net"
	"os"
	"strconv"
	"strings"
)

// gateway_linux.go resolves the default gateway by reading /proc/net/route: the row
// whose Destination is 00000000 (0.0.0.0) with the RTF_GATEWAY flag is the default
// route, and its Gateway column holds the next hop as a little-endian hex DWORD.

func defaultGateway() (net.IP, error) {
	f, err := os.Open("/proc/net/route")
	if err != nil {
		return nil, ErrNoDefaultGateway
	}
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	first := true
	for sc.Scan() {
		if first { // header: Iface Destination Gateway Flags ...
			first = false
			continue
		}
		fields := strings.Fields(sc.Text())
		if len(fields) < 4 {
			continue
		}
		// Default route: destination 0.0.0.0.
		if fields[1] != "00000000" {
			continue
		}
		flags, err := strconv.ParseInt(fields[3], 16, 32)
		if err != nil || flags&0x2 == 0 { // RTF_GATEWAY = 0x2
			continue
		}
		gwHex, err := strconv.ParseUint(fields[2], 16, 32)
		if err != nil {
			continue
		}
		// The Gateway column is a little-endian hex DWORD (a[0] is the low byte).
		// Hand-roll the decode: core/ bans encoding/binary (it pulls reflect) — see
		// core/internal/archtest.
		v := uint32(gwHex)
		ip := net.IP([]byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24)}).To4()
		if ip == nil || ip.IsUnspecified() {
			continue
		}
		return ip, nil
	}
	return nil, ErrNoDefaultGateway
}
