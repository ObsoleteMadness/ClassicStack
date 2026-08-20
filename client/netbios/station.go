package netbios

import (
	"crypto/rand"
	"fmt"
	"strings"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/client/link"
	nb "github.com/ObsoleteMadness/ClassicStack/core/protocol/netbios"
)

// station.go holds the station identity helpers shared by the datagram carriers:
// the Ethernet source MAC (host NIC by default, RandomMAC when that cannot be
// resolved) and the default NetBIOS name a datagram client presents.

// OpenerFor builds a raw-Ethernet link.Opener for a datagram carrier from an interface
// type (pcap | tap) and device name. A non-zero mac pins the Ethernet source; a zero
// mac keeps NewOpener's host-NIC MAC (WiFi APs drop any other source). If the host
// MAC cannot be resolved, a synthetic RandomMAC is used so the carrier still has a
// source address.
func OpenerFor(ifaceType, device string, mac [6]byte) (*link.Opener, error) {
	kind := ifaceType
	if kind == "" {
		kind = link.KindPcap
	}
	if !link.IsRawEtherKind(kind) {
		return nil, fmt.Errorf("netbios: -ifacetype %q carries no NetBIOS datagrams (want %v)", ifaceType, link.RawEtherKinds)
	}
	opener := link.NewOpener(link.Spec{Kind: strings.ToLower(kind), Name: device})
	if mac != ([6]byte{}) {
		opener.MAC = mac
	} else if opener.MAC == ([6]byte{}) {
		// Host NIC MAC was not resolvable (unknown device / tests); fall back to a
		// synthetic station so the carrier still has a source address.
		opener.MAC = RandomMAC()
	}
	return opener, nil
}

// RandomMAC generates a locally-administered, unicast MAC for the client's virtual
// station. A datagram client is a distinct station ON the segment the pcap device
// bridges, NOT the host, so it presents its own node address rather than borrowing the
// host NIC's MAC (which would collide with the host's own networking). The first octet
// has the locally-administered bit set and the group bit clear (the IEEE convention for a
// synthetic unicast address); the rest is random. Mirrors client/smb.RandomMAC.
func RandomMAC() [6]byte {
	var mac [6]byte
	_, _ = rand.Read(mac[:])
	mac[0] = (mac[0] | 0x02) &^ 0x01 // locally-administered, unicast
	return mac
}

// DefaultStationName derives a stable-ish NetBIOS workstation name from a MAC, so two
// client stations on one segment present distinct source names. "CS-" + the last three
// MAC octets in hex (e.g. "CS-A1B2C3", within the 15-char limit). Mirrors client/smb's
// nbipxCallingName. typ is the name-type suffix to stamp (workstation for a sender).
func DefaultStationName(mac [6]byte, typ uint8) nb.Name {
	const hex = "0123456789ABCDEF"
	b := []byte{'C', 'S', '-'}
	for _, o := range mac[3:] {
		b = append(b, hex[o>>4], hex[o&0x0F])
	}
	return nb.NewName(string(b), typ)
}

// BrowseAll opens each carrier in Protocols over the opener, actively browses it for
// window, and returns the union of hosts grouped by the protocol they were heard on — the
// full "net view" sweep. Each carrier is opened, browsed, and closed in turn (a pcap
// device serves one FrameLink at a time). A per-carrier open failure is returned in errs
// keyed by protocol rather than aborting the sweep, so a segment reachable over only one
// carrier still enumerates. station is the source NetBIOS name to present; a zero Name
// derives one from the opener's MAC. workgroup is the domain to fan the solicit out to
// ("" uses the blind default) — load-bearing on the IPX carriers, whose browser datagrams
// are addressed to <workgroup><00>.
func BrowseAll(opener *link.Opener, station nb.Name, workgroup string, window time.Duration) (map[Protocol][]Host, map[Protocol]error) {
	hosts := map[Protocol][]Host{}
	errs := map[Protocol]error{}
	for _, p := range Protocols {
		name := station
		if name == (nb.Name{}) {
			mac := opener.MAC
			if mac == ([6]byte{}) {
				mac = RandomMAC()
			}
			name = DefaultStationName(mac, nb.NameTypeWorkstation)
		}
		c, err := Open(opener, p, name)
		if err != nil {
			errs[p] = err
			continue
		}
		found, err := c.Browse(workgroup, window)
		_ = c.Close()
		if err != nil {
			errs[p] = err
			continue
		}
		hosts[p] = found
	}
	return hosts, errs
}
