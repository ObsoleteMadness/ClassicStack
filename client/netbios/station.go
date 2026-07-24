package netbios

import (
	"crypto/rand"
	"fmt"
	"strings"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/client/link"
	nb "github.com/ObsoleteMadness/ClassicStack/core/protocol/netbios"
)

// station.go holds the virtual-station identity helpers shared by the datagram carriers:
// the synthetic MAC and the default NetBIOS name a datagram client presents. They mirror
// client/smb's RandomMAC / calling-name derivation so a datagram station on the segment
// looks like any other client station, never the host itself.

// OpenerFor builds a raw-Ethernet link.Opener for a datagram carrier from an interface
// type (pcap | tap) and device name, with the virtual-station MAC pinned (or a synthetic
// RandomMAC when mac is zero). It is the shared opener-construction a datagram tool needs,
// so csnetsend/csnetview — and any client embedding this package — select the carrier the
// same way the SMB file client does (a raw-Ethernet FrameLink over pcap or the
// libpcap-free TUN/TAP), rather than each re-deriving it. ifaceType must be a raw-Ethernet
// kind (link.RawEtherKinds); ltoudp/tashtalk/tcp carry no NetBIOS datagrams and are
// rejected with a clear message. An empty ifaceType defaults to pcap.
func OpenerFor(ifaceType, device string, mac [6]byte) (*link.Opener, error) {
	kind := ifaceType
	if kind == "" {
		kind = link.KindPcap
	}
	if !link.IsRawEtherKind(kind) {
		return nil, fmt.Errorf("netbios: -ifacetype %q carries no NetBIOS datagrams (want %v)", ifaceType, link.RawEtherKinds)
	}
	opener := link.NewOpener(link.Spec{Kind: strings.ToLower(kind), Name: device})
	if mac == ([6]byte{}) {
		mac = RandomMAC()
	}
	opener.MAC = mac // synthetic virtual-station node (never the host NIC's)
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
// derives one from the opener's MAC.
func BrowseAll(opener *link.Opener, station nb.Name, window time.Duration) (map[Protocol][]Host, map[Protocol]error) {
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
		found, err := c.Browse(window)
		_ = c.Close()
		if err != nil {
			errs[p] = err
			continue
		}
		hosts[p] = found
	}
	return hosts, errs
}
