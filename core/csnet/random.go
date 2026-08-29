package csnet

import "crypto/rand"

// RandomMAC returns a synthetic locally-administered unicast Ethernet address: the
// first octet has the locally-administered bit set and the group/multicast bit
// clear, the rest are random. Used to give a virtual station (a probe tool, a
// client-side transport) its own identity distinct from the host NIC's real MAC,
// so it doesn't collide with the host on the wire.
//
// crypto/rand transitively imports reflect, but reflect itself builds and links
// fine under TinyGo (verified with the real toolchain) — the core ring only bans
// the specific packages that do generic reflection-based *serialization*
// (encoding/json, encoding/binary, database/sql; see core/internal/archtest),
// which crypto/rand is not. So, unlike an earlier version of this package,
// RandomMAC lives here rather than in client/link.
func RandomMAC() [6]byte {
	var mac [6]byte
	_, _ = rand.Read(mac[:])
	mac[0] = (mac[0] | 0x02) &^ 0x01 // locally-administered, unicast
	return mac
}
