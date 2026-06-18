// Command csnetview is a standalone NetBIOS browse-list listener over raw Ethernet —
// an enhanced "net view". It passively decodes the browser HostAnnouncement /
// LocalMasterAnnouncement / DomainAnnouncement frames that NetBIOS hosts broadcast
// (over NetBEUI and NetBIOS-over-IPX) and prints the discovered hosts with the
// transport they were seen on, their protocol address, the OS / browser-protocol
// versions they declare, and their comment.
//
// Unlike csnbp/csgetzones (which query a responder), this is a pure passive sniffer:
// it joins no group and sends nothing, just watches the wire for the periodic
// announcements every browser emits (and a Windows host re-announces on a timer, so a
// listen window of a few seconds usually catches the active machines). To force a
// faster sweep, run it alongside any host that sends an AnnouncementRequest, or widen
// -timeout.
//
// It drives the SAME core codecs the server uses to read these frames —
// core/protocol/netbeui (NBF), core/protocol/netbios (NBIPX/NMPI),
// core/protocol/mailslot (the SMB transaction envelope), and core/protocol/browser
// (the announcement frames) — chained from the raw Ethernet frame down to the
// announcement, mirroring the server's own ingress path. It needs the 'pcap' build
// tag (libpcap/Npcap) and privilege to open the NIC.
package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/adapter/link/pcap"
	"github.com/ObsoleteMadness/ClassicStack/core/link"
	browserproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/browser"
	mailslotproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/mailslot"
	nbf "github.com/ObsoleteMadness/ClassicStack/core/protocol/netbeui"
	nbproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/netbios"
)

const ethHdrLen = 14

// llcNetBIOS is the 802.2 LLC UI header for NBF (DSAP=SSAP=0xF0, control=0x03).
var llcNetBIOS = [3]byte{0xF0, 0xF0, 0x03}

// host is one discovered NetBIOS host: where it was seen, what it announced, and the
// last announcement's timestamp (newer announcements overwrite older fields).
type host struct {
	name      string
	transport string // "NetBEUI" or "IPX"
	addr      string // protocol address it was seen from (MAC, or IPX net.node)
	osVer     string // "major.minor" or "" if not announced
	appVer    string // browser-protocol version "major.minor"
	comment   string
	role      string // "host", "master", or "domain master"
	lastSeen  time.Time
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "csnetview:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		iface   = flag.String("iface", "", "network interface to listen on (pcap device name; required)")
		timeout = flag.Duration("timeout", 8*time.Second, "how long to listen for announcements")
	)
	flag.Parse()

	if *iface == "" {
		return fmt.Errorf("an -iface is required (a pcap device name)")
	}

	fl, err := pcap.Open(pcap.DefaultEtherTalkConfig(*iface))
	if err != nil {
		return fmt.Errorf("open %s: %w", *iface, err)
	}
	defer fl.Close()

	// Best-effort kernel filter: NetBEUI (LLC) and IPX frames carry the announcements.
	if f, ok := fl.(link.FilterableLink); ok {
		_ = f.SetFilter("ipx or (ether proto 0x8137) or (llc and ether[14:2] = 0xf0f0)")
	}

	fmt.Printf("listening for NetBIOS browse announcements on %s for %s ...\n", *iface, *timeout)
	hosts := map[string]*host{}
	deadline := time.Now().Add(*timeout)
	for time.Now().Before(deadline) {
		frame, err := fl.Read()
		if err != nil {
			if err == link.ErrTimeout {
				continue
			}
			break
		}
		if h := decodeFrame(frame); h != nil {
			merge(hosts, h)
		}
	}

	printHosts(hosts)
	return nil
}

// decodeFrame tries each transport's decode path on one Ethernet frame and returns a
// discovered host, or nil when the frame is not a browser announcement.
func decodeFrame(frame link.Frame) *host {
	if len(frame) < ethHdrLen {
		return nil
	}
	var srcMAC [6]byte
	copy(srcMAC[:], frame[6:12])
	etherType := uint16(frame[12])<<8 | uint16(frame[13])
	body := frame[ethHdrLen:]

	switch {
	case etherType <= 0x05DC && len(body) >= 3 &&
		body[0] == llcNetBIOS[0] && body[1] == llcNetBIOS[1] && body[2] == llcNetBIOS[2]:
		return decodeNetBEUI(body[3:], macString(srcMAC))
	case etherType == 0x8137:
		return decodeIPX(body)
	}
	return nil
}

// decodeNetBEUI decodes an NBF body to a browser announcement: an NBF datagram /
// datagram-broadcast frame whose payload is the SMB mailslot transaction carrying the
// browser frame.
func decodeNetBEUI(nbfBody []byte, addr string) *host {
	f, err := nbf.Decode(nbfBody)
	if err != nil {
		return nil
	}
	if f.Command != nbf.CmdDatagram && f.Command != nbf.CmdDatagramBroadcast {
		return nil
	}
	return announcementToHost(f.Payload, "NetBEUI", addr)
}

// decodeIPX decodes an IPX frame to a browser announcement: an IPX type-20 NBIPX
// broadcast whose NMPI MailslotSend payload is the SMB mailslot transaction carrying
// the browser frame. The protocol address is the source IPX network.node.
func decodeIPX(ipxBody []byte) *host {
	// The IPX header is 30 bytes; the source net (bytes 18..22) + node (22..28) give
	// the printable address. Type (byte 5) 0x14 is the NBIPX broadcast carrying NMPI.
	if len(ipxBody) < 30 || ipxBody[5] != nbproto.IPXTypeNetBIOS {
		return nil
	}
	var srcNet [4]byte
	var srcNode [6]byte
	copy(srcNet[:], ipxBody[18:22])
	copy(srcNode[:], ipxBody[22:28])
	nmpi, err := nbproto.DecodeNMPIPacket(ipxBody[30:])
	if err != nil || nmpi.Opcode != nbproto.NMPIOpMailslotSend {
		return nil
	}
	addr := fmt.Sprintf("%s.%s", netString(srcNet), macString(srcNode))
	return announcementToHost(nmpi.Payload, "IPX", addr)
}

// announcementToHost unwraps the SMB mailslot envelope and the browser frame from a
// datagram payload and builds a host record from a host/local-master/domain
// announcement. Returns nil for any other mailslot or browser opcode.
func announcementToHost(payload []byte, transport, addr string) *host {
	w, err := mailslotproto.Unmarshal(payload)
	if err != nil || !strings.EqualFold(w.Name, mailslotproto.NameBrowse) {
		return nil
	}
	op, frame, ok := browserproto.UnwrapPayload(w.Body)
	if !ok {
		return nil
	}
	switch op {
	case browserproto.OpHostAnnouncement, browserproto.OpLocalMasterAnnounce:
		a, err := browserproto.UnmarshalAnnouncement(frame)
		if err != nil {
			return nil
		}
		role := "host"
		if op == browserproto.OpLocalMasterAnnounce {
			role = "master"
		}
		return &host{
			name:      browserproto.NormalizeName(a.ServerName),
			transport: transport,
			addr:      addr,
			osVer:     fmt.Sprintf("%d.%d", a.OSVersionMajor, a.OSVersionMinor),
			appVer:    fmt.Sprintf("%d.%d", a.VersionMajor, a.VersionMinor),
			comment:   a.Comment,
			role:      role,
			lastSeen:  time.Now(),
		}
	case browserproto.OpDomainAnnouncement:
		da, err := browserproto.UnmarshalDomainAnnouncement(frame)
		if err != nil {
			return nil
		}
		return &host{
			name:      browserproto.NormalizeName(da.LocalMaster),
			transport: transport,
			addr:      addr,
			role:      "domain master",
			comment:   "workgroup " + browserproto.NormalizeName(da.MachineGroup),
			lastSeen:  time.Now(),
		}
	}
	return nil
}

// merge inserts or updates a host by name, preferring the newest announcement and not
// losing a richer field (version/comment) to a sparser later one (e.g. a domain
// announcement that carries no version).
func merge(hosts map[string]*host, h *host) {
	if h.name == "" {
		return
	}
	existing := hosts[h.name]
	if existing == nil {
		hosts[h.name] = h
		return
	}
	existing.lastSeen = h.lastSeen
	existing.transport = h.transport
	existing.addr = h.addr
	if h.osVer != "" && h.osVer != "0.0" {
		existing.osVer = h.osVer
	}
	if h.appVer != "" && h.appVer != "0.0" {
		existing.appVer = h.appVer
	}
	if h.comment != "" {
		existing.comment = h.comment
	}
	if h.role == "master" || h.role == "domain master" {
		existing.role = h.role
	}
}

// printHosts renders the discovered hosts as an aligned table sorted by name.
func printHosts(hosts map[string]*host) {
	if len(hosts) == 0 {
		fmt.Println("no hosts discovered (try a longer -timeout, or a quieter segment may simply have none announcing)")
		return
	}
	names := make([]string, 0, len(hosts))
	for n := range hosts {
		names = append(names, n)
	}
	sort.Strings(names)

	fmt.Printf("\n%-16s %-8s %-21s %-7s %-7s %s\n", "HOST", "PROTO", "ADDRESS", "OS", "APPVER", "ROLE / COMMENT")
	for _, n := range names {
		h := hosts[n]
		rc := h.role
		if h.comment != "" {
			rc = h.role + " — " + h.comment
		}
		fmt.Printf("%-16s %-8s %-21s %-7s %-7s %s\n",
			h.name, h.transport, h.addr, dash(h.osVer), dash(h.appVer), rc)
	}
	fmt.Printf("\n%d host(s) discovered\n", len(hosts))
}

// dash renders an empty or zero version as "-".
func dash(v string) string {
	if v == "" || v == "0.0" {
		return "-"
	}
	return v
}

// macString formats a 6-byte address as a colon-separated MAC.
func macString(n [6]byte) string {
	return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", n[0], n[1], n[2], n[3], n[4], n[5])
}

// netString formats a 4-byte IPX network number as 8 hex digits.
func netString(n [4]byte) string {
	return fmt.Sprintf("%02x%02x%02x%02x", n[0], n[1], n[2], n[3])
}
