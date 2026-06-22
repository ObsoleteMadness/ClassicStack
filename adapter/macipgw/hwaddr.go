package macipgw

import (
	"encoding/hex"
	"fmt"
	"net"
	"strings"
)

// macIPOUI is the locally administered prefix used to fabricate a stable per-Mac
// Ethernet address for DHCP relay (bit 1 of the first octet marks it locally
// administered). Mirrors the legacy pkg/hwaddr.MacIPOUI.
var macIPOUI = [3]byte{0x02, 0x00, 0x00}

// parseEthernet accepts 12 hex digits with optional ':' or '-' separators.
func parseEthernet(s string) (net.HardwareAddr, error) {
	normalized := strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(s), ":", ""), "-", "")
	if len(normalized) != 12 {
		return nil, fmt.Errorf("ethernet address: want 12 hex digits, got %d", len(normalized))
	}
	b, err := hex.DecodeString(normalized)
	if err != nil {
		return nil, fmt.Errorf("ethernet address: %w", err)
	}
	return net.HardwareAddr(b), nil
}

// fabricateMACForAT builds a stable locally administered Ethernet MAC from an
// AppleTalk address, giving each Mac a stable identity for the DHCP server:
// 02:00:00 : <atNet hi> : <atNet lo> : <atNode>.
func fabricateMACForAT(atNet uint16, atNode uint8) net.HardwareAddr {
	return net.HardwareAddr{
		macIPOUI[0], macIPOUI[1], macIPOUI[2],
		byte(atNet >> 8), byte(atNet), atNode,
	}
}
