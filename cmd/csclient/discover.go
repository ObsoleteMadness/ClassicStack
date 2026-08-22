package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	afpclient "github.com/ObsoleteMadness/ClassicStack/client/afp"
	"github.com/ObsoleteMadness/ClassicStack/client/atalk"
	"github.com/ObsoleteMadness/ClassicStack/client/browse"
	etherdfsclient "github.com/ObsoleteMadness/ClassicStack/client/etherdfs"
	clientlink "github.com/ObsoleteMadness/ClassicStack/client/link"
	ncpclient "github.com/ObsoleteMadness/ClassicStack/client/ncp"
	"github.com/ObsoleteMadness/ClassicStack/cmd/internal/csconnect"
	"github.com/ObsoleteMadness/ClassicStack/core/link"
	ipxport "github.com/ObsoleteMadness/ClassicStack/core/port/ipx"
	etherdfsproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/etherdfs"
	ipxproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/ipx"
	ncpproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/ncp"
)

// cmdDiscover runs a scheme's own discovery probe and prints each responder so it can be
// pasted into a URI. AFP uses NBP on the selected DDP link (plus LToUDP) and Bonjour
// for AFP-over-TCP; NCP broadcasts a SAP General Query for file servers.
func cmdDiscover(cfg config, args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: csfs discover <scheme>  (afp | smb | ncp | etherdfs)")
		return 2
	}
	scheme := args[0]
	switch scheme {
	case "afp":
		return discoverAFP(cfg)
	case "ncp":
		return discoverNCP(cfg)
	case "etherdfs":
		return discoverEtherDFS(cfg)
	case "smb":
		return discoverSMB(cfg)
	default:
		fmt.Fprintf(os.Stderr, "csfs: unknown scheme %q\n", scheme)
		return 2
	}
}

// discoverAFP looks up AFPServer NBP entities on the selected DDP transport (and
// LToUDP when that is not already the selection) and browses AFP-over-TCP via
// mDNS (_afpovertcp._tcp). Each line is a pasteable URI with the link kind in
// the ",transport" tail.
func discoverAFP(cfg config) int {
	kind := cfg.IfaceType
	if kind == "" {
		kind = clientlink.KindLToUDP
	}
	count := 0
	scanDDP := func(k, name string) {
		opener := clientlink.NewOpener(clientlink.Spec{Kind: k, Name: name})
		dl, err := opener.DatagramLinkDDP()
		if err != nil {
			fmt.Fprintf(os.Stderr, "  (%s unavailable: %v)\n", k, err)
			return
		}
		ep := atalk.NewEndpoint(dl, atalk.Addr{Network: opener.Net, Node: opener.Node})
		defer ep.Close()
		ents, err := ep.LookupAllZones("=", atalk.AFPServerType, 2*time.Second)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  (%s NBP: %v)\n", k, err)
			return
		}
		for _, e := range ents {
			count++
			fmt.Printf("%s:%s\tafp://%s:%s,%s/  (%d.%d socket %d)\n",
				e.Object, e.Zone, e.Object, e.Zone, k, e.Addr.Network, e.Addr.Node, e.Addr.Socket)
		}
	}
	scanDDP(kind, csconnect.ResolveIface(kind, cfg.Iface))
	if kind != clientlink.KindLToUDP {
		scanDDP(clientlink.KindLToUDP, "")
	}

	tcpDev := cfg.Iface
	if tcpDev == "" {
		if d, err := clientlink.DefaultInterface(); err == nil {
			tcpDev = d.Name
		}
	}
	servers, err := afpclient.DiscoverTCP(tcpDev, 2*time.Second)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  (tcp mDNS unavailable: %v)\n", err)
	}
	for _, s := range servers {
		count++
		host := s.Host
		if s.Port != 0 && s.Port != afpclient.DSIPort {
			host = fmt.Sprintf("%s:%d", host, s.Port)
		}
		fmt.Printf("%s\tafp://%s,tcp/  (AFP over TCP port %d)\n", s.Name, host, s.Port)
	}

	if count == 0 {
		fmt.Println("no AFP servers responded")
	}
	return 0
}

// SAP/IPX discovery framing constants (mirroring the client/ncp IPX transport and
// core/port/ipx). NCP servers advertise themselves via SAP on socket 0x0452; a General
// Query for the File Server type (0x0004) draws a response from each server naming its
// NetWare name and IPX address.
const (
	sapDiscoverWait    = 2 * time.Second
	sapDiscoverIPXType = 0x04 // IPX packet type SAP rides (PEP; the server accepts type 0/4)
)

// sapQueryFrameTypes are the Ethernet encapsulations the SAP query is broadcast in when
// the user pins none: all three legacy framings. A real NetWare server is bound to raw
// 802.3 or 802.2 rather than Ethernet II (each frame type is a distinct logical IPX net),
// so querying in every framing is what draws a reply regardless of the server's binding —
// the read path accepts all three via core/port/ipx.Strip either way.
var sapQueryFrameTypes = []ipxport.FrameType{ipxport.FrameEthernetII, ipxport.FrameRaw8023, ipxport.FrameLLC8022}

// smbBrowseWindow is how long discover smb listens per NetBIOS carrier after soliciting.
// It matches csnetview's default: long enough for solicited browsers to re-announce
// without a long wait.
const smbBrowseWindow = 4 * time.Second

// discoverSMB enumerates SMB servers the way a real "net view" does — via the master
// browser, not by trusting broadcast self-announcements. In a real workgroup an ordinary
// host (e.g. a Win98 File & Print station) announces ONLY to the local master browser and
// does not answer a broadcast AnnouncementRequest, so a solicit-and-sniff sweep almost never
// sees it; the authoritative list lives in the master and must be asked for. The whole
// three-source sweep (solicit+sniff, find-master via __MSBROWSE__/<1D>+GetBackupList, then
// RAP NetServerEnum2 over an SMB session to the master) lives in client/browse, shared with
// csnetview. Raw-Ethernet only (the browser rides NetBIOS datagrams).
func discoverSMB(cfg config) int {
	kind := cfg.IfaceType
	if kind == "" {
		kind = clientlink.KindPcap
	}
	if !clientlink.IsRawEtherKind(kind) {
		return fail(fmt.Errorf("discover smb needs a raw-Ethernet interface (the browser rides NetBIOS datagrams); got -ifacetype %q", kind))
	}

	var mac [6]byte
	if cfg.MAC != "" {
		m, err := parseMAC(cfg.MAC)
		if err != nil {
			return fail(err)
		}
		mac = m
	}

	servers, results := browse.Enumerate(browse.Options{
		Device:    csconnect.ResolveIface(kind, cfg.Iface),
		Kind:      kind,
		MAC:       mac,
		FrameType: cfg.FrameType,
		Window:    smbBrowseWindow,
		Trace:     func(line string) { fmt.Println(line) },
	})
	tcpServers, tcpRes := browse.EnumerateTCP(browse.Options{
		Device: csconnect.ResolveIface(kind, cfg.Iface),
		Kind:   kind,
		Window: smbBrowseWindow,
		Trace:  func(line string) { fmt.Println(line) },
	})
	if tcpRes.Err != nil {
		fmt.Fprintf(os.Stderr, "  (%s carrier unavailable: %v)\n", tcpRes.Protocol, tcpRes.Err)
	}
	servers = mergeBrowseServers(servers, tcpServers)
	// Surface per-carrier open failures so a segment reachable over only one carrier still
	// reports usefully (e.g. no IPX on the wire).
	for _, r := range results {
		if r.Err != nil {
			fmt.Fprintf(os.Stderr, "  (%s carrier unavailable: %v)\n", r.Protocol, r.Err)
		}
	}

	if len(servers) == 0 {
		fmt.Println("no SMB servers found (no announcements, and no master browser answered)")
		return 0
	}
	for _, s := range servers {
		fmt.Printf("%s\tsmb://%s/  (%s%s)\n", s.Name, s.Name, smbVia(s), smbServerNote(s))
	}
	return 0
}

// smbVia renders how a server was discovered (its carriers + the most authoritative source).
func smbVia(s browse.Server) string {
	carriers := make([]string, 0, len(s.Carriers))
	for _, c := range s.Carriers {
		carriers = append(carriers, string(c))
	}
	via := strings.Join(carriers, "+")
	switch s.Source {
	case browse.SourceBrowseList:
		via += " browse-list"
	case browse.SourceMaster:
		via += " master"
	}
	return via
}

func mergeBrowseServers(a, b []browse.Server) []browse.Server {
	if len(b) == 0 {
		return a
	}
	byName := make(map[string]browse.Server, len(a)+len(b))
	order := make([]string, 0, len(a)+len(b))
	for _, s := range append(a, b...) {
		if exist, ok := byName[s.Name]; ok {
			exist.Carriers = append(exist.Carriers, s.Carriers...)
			if s.Comment != "" {
				exist.Comment = s.Comment
			}
			if s.Address != "" {
				exist.Address = s.Address
			}
			if s.Source > exist.Source {
				exist.Source = s.Source
			}
			byName[s.Name] = exist
			continue
		}
		byName[s.Name] = s
		order = append(order, s.Name)
	}
	out := make([]browse.Server, 0, len(order))
	for _, name := range order {
		out = append(out, byName[name])
	}
	return out
}

// smbServerNote renders the role/comment detail of a discovered server as a " — ..." suffix.
func smbServerNote(s browse.Server) string {
	parts := make([]string, 0, 2)
	if s.Role != "" {
		parts = append(parts, s.Role)
	}
	if s.Comment != "" {
		parts = append(parts, s.Comment)
	}
	if len(parts) == 0 {
		return ""
	}
	return " — " + strings.Join(parts, ", ")
}

// discoverNCP broadcasts a SAP General Query for NetWare file servers over the selected
// raw NIC (pcap) and prints each responder's server name and IPX address so it can be
// pasted into an ncp:// URI. NCP discovery is IPX-only (SAP rides IPX), so it needs a
// pcap interface just like the ncp client transport.
func discoverNCP(cfg config) int {
	kind := cfg.IfaceType
	if kind == "" {
		kind = clientlink.KindPcap
	}
	if kind != clientlink.KindPcap {
		return fail(fmt.Errorf("discover ncp needs a pcap interface (SAP rides IPX); got -ifacetype %q", kind))
	}
	opener := clientlink.NewOpener(clientlink.Spec{Kind: kind, Name: csconnect.ResolveIface(kind, cfg.Iface)})
	if cfg.MAC != "" {
		mac, err := parseMAC(cfg.MAC)
		if err != nil {
			return fail(err)
		}
		opener.MAC = mac
	}
	fl, err := opener.FrameLink("ipx")
	if err != nil {
		return fail(fmt.Errorf("open transport: %w", err))
	}
	defer fl.Close()

	srcMAC := opener.MAC
	if srcMAC == ([6]byte{}) {
		srcMAC = ncpclient.RandomMAC()
	}

	// Broadcast the SAP General Query for the File Server type. Send it in every frame
	// type the user did not pin (a real server is often bound only on raw-802.3 / 802.2,
	// and each frame type is a distinct logical IPX net), or only the pinned one.
	query := ncpproto.MarshalQuery(ncpproto.SAPGeneralQuery, ncpproto.SAPServerTypeFileServer, nil)
	d := &ipxproto.Datagram{
		Type:    sapDiscoverIPXType,
		DstNode: [6]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF},
		DstSock: ncpproto.SAPSocket,
		SrcNode: srcMAC,
		SrcSock: ncpproto.SAPSocket,
		Payload: query,
	}
	frameTypes := sapQueryFrameTypes
	if cfg.FrameType != "" {
		ft, err := ipxport.ParseFrameType(cfg.FrameType)
		if err != nil {
			return fail(err)
		}
		frameTypes = []ipxport.FrameType{ft}
	}
	for _, ft := range frameTypes {
		if err := writeSAPFrame(fl, d, srcMAC, ft); err != nil {
			return fail(fmt.Errorf("send SAP query: %w", err))
		}
	}

	// Collect responses for a short window.
	seen := map[string]bool{}
	deadline := time.Now().Add(sapDiscoverWait)
	count := 0
	for time.Now().Before(deadline) {
		frame, err := fl.Read()
		if err != nil {
			if errors.Is(err, link.ErrTimeout) {
				continue
			}
			break
		}
		payload, ft, ok := ipxport.Strip(frame)
		if !ok {
			continue
		}
		dd, derr := ipxproto.Decode(payload)
		if derr != nil || dd.DstSock != ncpproto.SAPSocket && dd.SrcSock != ncpproto.SAPSocket {
			continue
		}
		op, entries, perr := ncpproto.ParseSAPResponse(dd.Payload)
		if perr != nil || (op != ncpproto.SAPGeneralResponse && op != ncpproto.SAPNearestResponse) {
			continue
		}
		for _, e := range entries {
			if e.Type != ncpproto.SAPServerTypeFileServer || seen[e.Name] {
				continue
			}
			seen[e.Name] = true
			count++
			// Report the frame type the advert arrived in — it is the framing to pass to
			// -frametype (or the default learned framing) to connect to this server.
			fmt.Printf("%s\tncp://%s/SYS  (net %02X%02X%02X%02X node %02X%02X%02X%02X%02X%02X hops %d frametype %s)\n",
				e.Name, e.Name,
				e.Network[0], e.Network[1], e.Network[2], e.Network[3],
				e.Node[0], e.Node[1], e.Node[2], e.Node[3], e.Node[4], e.Node[5], e.Hops, ft)
		}
	}
	if count == 0 {
		fmt.Println("no NetWare servers responded")
	}
	return 0
}

// writeSAPFrame encapsulates an IPX datagram in an Ethernet frame of frameType and writes
// it, through the same core/port/ipx framing the client transport and server port use.
func writeSAPFrame(fl link.FrameLink, d *ipxproto.Datagram, srcMAC [6]byte, frameType ipxport.FrameType) error {
	ipxBytes, err := d.Encode(nil)
	if err != nil {
		return err
	}
	return fl.Write(frameType.Encapsulate(d.DstNode, srcMAC, ipxBytes))
}

// etherdfsDiscoverWait bounds how long the EtherDFS discovery collects replies.
const etherdfsDiscoverWait = 2 * time.Second

// discoverEtherDFS broadcasts an AL_INSTALLCHK over the selected raw NIC (pcap) and
// prints each responder's server name and MAC so a drive can be pasted into an
// etherdfs:// URI. EtherDFS discovery is raw-Ethernet-only (EtherType 0xEDF5), so it
// needs a pcap interface. The reference client learns the server MAC from an ordinary
// AL_DISKSPACE reply; AL_INSTALLCHK additionally draws the server NAME from a
// ClassicStack server, which this prints for a friendlier listing.
func discoverEtherDFS(cfg config) int {
	kind := cfg.IfaceType
	if kind == "" {
		kind = clientlink.KindPcap
	}
	if kind != clientlink.KindPcap {
		return fail(fmt.Errorf("discover etherdfs needs a pcap interface (raw Ethernet); got -ifacetype %q", kind))
	}
	opener := clientlink.NewOpener(clientlink.Spec{Kind: kind, Name: csconnect.ResolveIface(kind, cfg.Iface)})
	if cfg.MAC != "" {
		mac, err := parseMAC(cfg.MAC)
		if err != nil {
			return fail(err)
		}
		opener.MAC = mac
	}
	// The EtherDFS BPF filter (mirrors core/port/etherdfs.BPFFilter); narrows the pcap
	// handle to the custom EtherType so the read loop is not fed unrelated traffic.
	fl, err := opener.FrameLink("ether proto 0xedf5")
	if err != nil {
		return fail(fmt.Errorf("open transport: %w", err))
	}
	defer fl.Close()

	srcMAC := opener.MAC
	if srcMAC == ([6]byte{}) {
		srcMAC = etherdfsclient.RandomMAC()
	}

	// Broadcast an AL_INSTALLCHK for drive 0 (A:): the server answers with its name.
	req := etherdfsproto.Frame{
		DstMAC:   [6]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF},
		SrcMAC:   srcMAC,
		Sequence: 1,
		Drive:    0,
		Opcode:   etherdfsproto.OpInstallChk,
	}
	if err := fl.Write(req.Encode(nil)); err != nil {
		return fail(fmt.Errorf("send install check: %w", err))
	}

	seen := map[[6]byte]bool{}
	deadline := time.Now().Add(etherdfsDiscoverWait)
	count := 0
	for time.Now().Before(deadline) {
		frame, err := fl.Read()
		if err != nil {
			if errors.Is(err, link.ErrTimeout) {
				continue
			}
			break
		}
		f, perr := etherdfsproto.ParseFrame(frame)
		if perr != nil || f.SrcMAC == srcMAC || seen[f.SrcMAC] {
			continue
		}
		seen[f.SrcMAC] = true
		count++
		name := strings.TrimRight(string(f.Payload), "\x00")
		if name == "" {
			name = "(unnamed EtherDFS server)"
		}
		fmt.Printf("%s\tetherdfs://%02x:%02x:%02x:%02x:%02x:%02x/C\n",
			name, f.SrcMAC[0], f.SrcMAC[1], f.SrcMAC[2], f.SrcMAC[3], f.SrcMAC[4], f.SrcMAC[5])
	}
	if count == 0 {
		fmt.Println("no EtherDFS servers responded")
	}
	return 0
}
