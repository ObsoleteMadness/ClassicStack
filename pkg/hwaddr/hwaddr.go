// Package hwaddr provides unified hardware-address types covering Ethernet
// (EUI-48), LocalTalk (8-bit LLAP node ID), and AppleTalk (24-bit DDP
// address), plus parsing, formatting, generation, and conversion between
// them. It replaces ad-hoc helpers previously scattered across cmd/classicstack,
// port/ethertalk, port/localtalk, and service/macip.
package hwaddr

import (
	"encoding/hex"
	"fmt"
	"math/rand"
	"net"
	"strings"
)

// Ethernet is a 48-bit EUI-48 hardware address.
type Ethernet [6]byte

// LocalTalk is an 8-bit LLAP node identifier. Values 0 and 0xFF are reserved
// (invalid / broadcast). Nodes 1–127 are the "user" range; 128–254 are the
// "server" range that servers prefer when self-assigning.
type LocalTalk uint8

// AppleTalk is a 24-bit DDP address (16-bit network + 8-bit node).
type AppleTalk struct {
	Network uint16
	Node    uint8
}

// AppleOUI is Apple's registered IEEE OUI; used as the default prefix when
// synthesising Ethernet addresses from AppleTalk addresses.
var AppleOUI = [3]byte{0x00, 0x00, 0x07}

// MacIPOUI is the locally administered prefix historically used by ClassicStack's
// MacIP gateway to fabricate per-node MACs for DHCP. Bit 1 of the first octet
// is set, marking the address as locally administered.
var MacIPOUI = [3]byte{0x02, 0x00, 0x00}

// ParseEthernet accepts 12 hex digits with optional `:` or `-` separators.
func ParseEthernet(s string) (Ethernet, error) {
	var out Ethernet
	normalized := strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(s), ":", ""), "-", "")
	if len(normalized) != 12 {
		return out, fmt.Errorf("ethernet address: want 12 hex digits, got %d", len(normalized))
	}
	b, err := hex.DecodeString(normalized)
	if err != nil {
		return out, fmt.Errorf("ethernet address: %w", err)
	}
	copy(out[:], b)
	return out, nil
}

// String renders as colon-separated lowercase hex (`de:ad:be:ef:ca:fe`).
func (e Ethernet) String() string {
	return net.HardwareAddr(e[:]).String()
}

// Bytes returns a copy of the raw 6-byte form.
func (e Ethernet) Bytes() []byte {
	out := make([]byte, 6)
	copy(out, e[:])
	return out
}

// HardwareAddr adapts to net.HardwareAddr for stdlib APIs.
func (e Ethernet) HardwareAddr() net.HardwareAddr {
	return net.HardwareAddr(e.Bytes())
}

// EthernetFromBytes constructs an Ethernet from a 6-byte slice.
func EthernetFromBytes(b []byte) (Ethernet, error) {
	var out Ethernet
	if len(b) != 6 {
		return out, fmt.Errorf("ethernet address: want 6 bytes, got %d", len(b))
	}
	copy(out[:], b)
	return out, nil
}

// ParseLocalTalk parses `0x<hex>`, `0<octal>`, or decimal forms.
func ParseLocalTalk(s string) (LocalTalk, error) {
	s = strings.TrimSpace(s)
	var n uint64
	var err error
	switch {
	case strings.HasPrefix(s, "0x"), strings.HasPrefix(s, "0X"):
		_, err = fmt.Sscanf(s[2:], "%x", &n)
	default:
		_, err = fmt.Sscanf(s, "%d", &n)
	}
	if err != nil {
		return 0, fmt.Errorf("localtalk node: %w", err)
	}
	if n > 0xFF {
		return 0, fmt.Errorf("localtalk node: %d out of range", n)
	}
	return LocalTalk(n), nil
}

// String renders as `0x<HH>`.
func (n LocalTalk) String() string { return fmt.Sprintf("0x%02X", uint8(n)) }

// Valid reports whether n is a usable unicast node id (not 0, not 0xFF).
func (n LocalTalk) Valid() bool { return n != 0 && n != 0xFF }

// IsServerRange reports whether n is in the server-preferred range (128–254).
func (n LocalTalk) IsServerRange() bool { return n >= 128 && n <= 254 }

// GenerateEthernet fabricates an Ethernet address by filling the last three
// octets with random bytes from r (using math/rand.Read if r is nil).
func GenerateEthernet(oui [3]byte, r *rand.Rand) Ethernet {
	var e Ethernet
	e[0], e[1], e[2] = oui[0], oui[1], oui[2]
	var tail [3]byte
	if r == nil {
		r = rand.New(rand.NewSource(rand.Int63()))
	}
	for i := range tail {
		tail[i] = byte(r.Intn(256))
	}
	e[3], e[4], e[5] = tail[0], tail[1], tail[2]
	return e
}

// GenerateLocalTalk returns a shuffled candidate list of LocalTalk node ids
// suitable for self-assignment. If preferred is non-empty its entries are
// tried first in the order given; the remaining valid node ids follow in
// shuffled order. If r is nil, math/rand's default source is used.
//
// Server callers should pass preferred ids in the 128–254 range so they
// claim server-range addresses before falling back to client-range ones.
func GenerateLocalTalk(preferred []LocalTalk, r *rand.Rand) []LocalTalk {
	seen := make(map[LocalTalk]bool, 254)
	out := make([]LocalTalk, 0, 254)
	for _, p := range preferred {
		if !p.Valid() || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	rest := make([]LocalTalk, 0, 254)
	for i := 1; i <= 254; i++ {
		id := LocalTalk(i)
		if seen[id] {
			continue
		}
		rest = append(rest, id)
	}
	shuffle := rand.Shuffle
	if r != nil {
		shuffle = r.Shuffle
	}
	shuffle(len(rest), func(i, j int) { rest[i], rest[j] = rest[j], rest[i] })
	return append(out, rest...)
}

// EthernetFromAppleTalk synthesises an Ethernet address encoding the given
// AppleTalk address in the low 24 bits. The conversion is deterministic and
// reversible via AppleTalkFromEthernet using the same oui.
//
// Layout: [oui[0] oui[1] oui[2] netHi netLo node].
func EthernetFromAppleTalk(oui [3]byte, a AppleTalk) Ethernet {
	var e Ethernet
	e[0], e[1], e[2] = oui[0], oui[1], oui[2]
	e[3] = byte(a.Network >> 8)
	e[4] = byte(a.Network)
	e[5] = a.Node
	return e
}

// AppleTalkFromEthernet recovers the AppleTalk address previously encoded
// by EthernetFromAppleTalk. Returns ok=false if the OUI prefix does not
// match.
func AppleTalkFromEthernet(oui [3]byte, e Ethernet) (AppleTalk, bool) {
	if e[0] != oui[0] || e[1] != oui[1] || e[2] != oui[2] {
		return AppleTalk{}, false
	}
	return AppleTalk{
		Network: uint16(e[3])<<8 | uint16(e[4]),
		Node:    e[5],
	}, true
}

// MacIPEthernetFromAppleTalk is the MacIP-gateway-specific address
// synthesis used for DHCP client identity on behalf of AppleTalk nodes.
// Layout: 0x02 (locally administered) | netHi | netLo | node | 'M' | 'I'.
// The suffix "MI" distinguishes these addresses from generic AARP-style
// syntheses and preserves wire-level compatibility with existing DHCP
// leases issued against ClassicStack MacIP.
func MacIPEthernetFromAppleTalk(a AppleTalk) Ethernet {
	return Ethernet{0x02, byte(a.Network >> 8), byte(a.Network), a.Node, 'M', 'I'}
}
