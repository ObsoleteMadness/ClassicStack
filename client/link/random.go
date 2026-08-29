package link

import "github.com/ObsoleteMadness/ClassicStack/core/csnet"

// RandomMAC returns a synthetic locally-administered unicast Ethernet address for
// a virtual station: the first octet has the locally-administered bit set and the
// group/multicast bit clear, the rest are random. A client transport (a probe
// tool, a raw-Ethernet carrier) is a distinct station on the segment the link
// bridges, NOT the host itself, so it presents its own node address rather than
// borrow the host NIC's identity (which would collide, and on Windows cannot even
// be resolved from an "\Device\NPF_{GUID}" name).
//
// Delegates to core/csnet.RandomMAC, the canonical implementation (crypto/rand is
// allowed in core — see core/csnet/random.go's doc comment). Kept as a wrapper
// here since client/etherdfs.RandomMAC, client/ncp.RandomMAC,
// client/netbios.RandomMAC, and client/smb.RandomMAC all call it by this name.
func RandomMAC() [6]byte {
	return csnet.RandomMAC()
}
