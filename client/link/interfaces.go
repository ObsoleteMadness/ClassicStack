package link

import (
	"fmt"
	"io"
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/core/hostinfo"
)

// interfaces.go exposes the host's capturable NICs to the client command line so a
// user who does not know the exact pcap device name (Npcap's "\Device\NPF_{GUID}" on
// Windows is not guessable) can discover it. Every client tool that takes a -iface for a
// pcap transport shares this one listing + printer, so the device names it prints are
// exactly the strings pcap.Open accepts. Enumeration itself is build-tag split
// (pcap.go / pcap_stub.go provide listPcapDevices) so a no-pcap build reports the honest
// "built without the 'pcap' tag" rather than a bogus empty list.

// Interface describes a capturable NIC as the client sees it: the raw pcap device Name
// (what -iface must be given), a human Description, and any bound IP addresses.
type Interface struct {
	Name        string   // raw pcap device name — the exact string -iface expects
	Description string   // friendly adapter description (may be empty)
	Addresses   []string // bound IP addresses (may be empty)
}

// ListInterfaces enumerates the host NICs available to the pcap raw-Ethernet transports
// (the carrier for EtherTalk / IPX / NetBEUI / EtherDFS and their SMB/NCP clients). It
// returns the pcap device names verbatim so a caller can copy one straight into -iface.
// In a build without the 'pcap' tag it returns the tag's ErrUnavailable-equivalent error.
func ListInterfaces() ([]Interface, error) {
	return listPcapDevices()
}

// DefaultInterface returns the capturable NIC bound to the host's primary (default-route)
// interface — the one a pcap client should use when the user omits -iface. It enumerates
// the pcap devices, then asks core/hostinfo to pick the one bound to the routing-table
// primary interface (pcap-free, cross-platform, no privileges). The returned
// Interface.Name is the exact "\Device\NPF_{GUID}"-style string -iface expects (Npcap
// device names are not derivable from the OS interface name — only an IP match bridges
// the two). It errors if the pcap backend is missing (no 'pcap' build tag), if there is
// no default route, or if the primary interface has no matching pcap device.
func DefaultInterface() (Interface, error) {
	devs, err := ListInterfaces()
	if err != nil {
		return Interface{}, fmt.Errorf("list pcap interfaces: %w", err)
	}
	hd := make([]hostinfo.Device, len(devs))
	for i, d := range devs {
		hd[i] = hostinfo.Device{Name: d.Name, Addresses: d.Addresses}
	}
	pick, err := hostinfo.PrimaryDevice(hd)
	if err != nil {
		return Interface{}, fmt.Errorf("detect primary interface: %w", err)
	}
	// Return the full Interface (with its Description) for the matched device name.
	for _, d := range devs {
		if d.Name == pick.Name {
			return d, nil
		}
	}
	return Interface{Name: pick.Name, Addresses: pick.Addresses}, nil
}

// PrintInterfaces writes the host's capturable NICs to w as an aligned table, or a clear
// diagnostic if enumeration failed (no pcap backend, or no permission). It is the shared
// implementation behind every client command's -list-ifaces flag, so the output is
// identical whichever tool a user runs it from. It never returns an error — a listing
// failure is reported in-band as a single explanatory line — so callers can simply print
// and exit 0.
func PrintInterfaces(w io.Writer) {
	ifaces, err := ListInterfaces()
	if err != nil {
		fmt.Fprintf(w, "cannot list interfaces: %v\n", err)
		fmt.Fprintln(w, "(raw-Ethernet transports need a build with the 'pcap' tag and Npcap/libpcap installed)")
		return
	}
	if len(ifaces) == 0 {
		fmt.Fprintln(w, "no capturable interfaces found")
		return
	}
	fmt.Fprintln(w, "Interfaces (pass the DEVICE to -iface):")
	for _, ifi := range ifaces {
		desc := ifi.Description
		if desc == "" {
			desc = "(no description)"
		}
		fmt.Fprintf(w, "  %s\n      %s", ifi.Name, desc)
		if len(ifi.Addresses) > 0 {
			fmt.Fprintf(w, " [%s]", strings.Join(ifi.Addresses, ", "))
		}
		fmt.Fprintln(w)
	}
}
