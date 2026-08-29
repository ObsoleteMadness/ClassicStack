package link

import "crypto/rand"

// RandomMAC returns a synthetic locally-administered unicast Ethernet address for
// a virtual station: the first octet has the locally-administered bit set and the
// group/multicast bit clear, the rest are random. A client transport (a probe
// tool, a raw-Ethernet carrier) is a distinct station on the segment the link
// bridges, NOT the host itself, so it presents its own node address rather than
// borrow the host NIC's identity (which would collide, and on Windows cannot even
// be resolved from an "\Device\NPF_{GUID}" name).
//
// This lives here rather than in core/csnet because it needs crypto/rand, which
// transitively imports reflect — forbidden in the core ring (core/internal/archtest,
// §1). Every current caller (client/etherdfs, client/ncp, client/netbios,
// client/smb, cmd/internal/csconnect) already imports this package.
func RandomMAC() [6]byte {
	var mac [6]byte
	_, _ = rand.Read(mac[:])
	mac[0] = (mac[0] | 0x02) &^ 0x01 // locally-administered, unicast
	return mac
}
