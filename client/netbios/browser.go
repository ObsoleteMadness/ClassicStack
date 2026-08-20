package netbios

import (
	"fmt"
	"sort"
	"strings"
	"time"

	corelink "github.com/ObsoleteMadness/ClassicStack/core/link"
	browserproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/browser"
	ipxproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/ipx"
	mailslotproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/mailslot"
	nbf "github.com/ObsoleteMadness/ClassicStack/core/protocol/netbeui"
	nb "github.com/ObsoleteMadness/ClassicStack/core/protocol/netbios"
)

// browser.go is the SDK's "net view" primitive: actively solicit browser announcements
// (an AnnouncementRequest datagram, so listening hosts re-announce immediately instead of
// on their periodic timer) and collect the HostAnnouncement / LocalMasterAnnouncement /
// DomainAnnouncement frames into a host list. It also holds the passive decode path
// (frame → Host), so csnetview is a thin consumer: parse flags, call Browse.
//
// This lifts the decode/merge logic that lived inline in cmd/csnetview into the SDK, so a
// third-party client enumerates a legacy segment by embedding this package rather than
// re-deriving the browser wire format.

// browseGroupName is the NetBIOS destination an NBF browser AnnouncementRequest /
// announcement targets: the workgroup group name at the browser suffix. Over NBF the
// AnnouncementRequest is broadcast to every browser on the segment, so the exact
// workgroup label is not load-bearing for soliciting a re-announce; "*" with the group
// suffix reaches all. The IPX datagram plane does NOT accept this name — see
// Conn.browseFanoutName.
var browseGroupName = nb.NewName("*", nb.NameTypeGroup)

// browseFanoutName is the destination NetBIOS name a browser datagram fans out to on
// this carrier.
//
// NBF keeps the wildcard group name above. The NWLink IPX datagram plane does not: every
// golden fan-out browser datagram on socket 0x0553 — host announcement, local-master
// announcement, AnnouncementRequest, election, GetBackupList request — is addressed to
// <workgroup><00>, the workgroup name each station registers at the workstation suffix
// (spec/captures/nbipx-win98.pcap frames 16/19/48/58, nwlink-win98.pcap frames
// 1/7/13/26-40; the "Check name WORKGROUP<00>" registrations are frames 2/9/11/14).
// Neither "*"<1E> nor <workgroup><1D> is ever seen there, and neither draws an answer
// from a live Win98/NT segment: a sweep sending them saw zero replies from four active
// NBIPX stations, which is why an NBIPX browse came back empty while NBF worked.
func (c *Conn) browseFanoutName(workgroup string) nb.Name {
	if ipxFamily(c.proto) {
		return nb.NewName(workgroupOrDefault(workgroup), nb.NameTypeWorkstation)
	}
	return browseGroupName
}

// Host is one discovered NetBIOS host: where it was seen (which carrier + protocol
// address), what it announced (name, OS/browser versions, comment), and its browser role.
// It is the SDK-facing browse-list record; csnetview renders a slice of these.
type Host struct {
	Name       string    // announced server/computer name (upper-cased)
	Protocol   Protocol  // the carrier it was heard on (NBF / NBIPX)
	Address    string    // protocol source address (MAC for NBF, IPX net.node for NBIPX)
	OSVersion  string    // "major.minor", or "" if not announced
	AppVersion string    // browser-protocol version "major.minor"
	Comment    string    // the announcement comment (or "workgroup X" for a domain announce)
	Role       string    // "host", "master", or "domain master"
	LastSeen   time.Time // timestamp of the most recent announcement seen
}

// Browse actively enumerates the hosts reachable over this carrier for window: it sends a
// broadcast AnnouncementRequest to solicit an immediate re-announce, then listens for the
// announcement datagrams and returns the collected hosts sorted by name. Because it
// solicits rather than only sniffing, a short window catches the active machines instead
// of waiting for each host's periodic (~12-minute) timer — the difference between an
// active "net view" and a passive listener.
func (c *Conn) Browse(workgroup string, window time.Duration) ([]Host, error) {
	if err := c.solicit(workgroup); err != nil {
		return nil, err
	}
	hosts := map[string]*Host{}
	deadline := time.Now().Add(window)
	for time.Now().Before(deadline) {
		frame, err := c.fl.Read()
		if err != nil {
			if err == corelink.ErrTimeout {
				continue
			}
			break
		}
		if h := c.decodeFrame(frame); h != nil {
			mergeHost(hosts, h)
		}
	}
	return sortedHosts(hosts), nil
}

// solicit broadcasts a browser AnnouncementRequest so every listening browser re-announces
// itself now. workgroup names the domain to fan out to ("" uses the blind default); it is
// load-bearing on the IPX carriers, whose fan-out name is <workgroup><00>.
func (c *Conn) solicit(workgroup string) error {
	dst := c.browseFanoutName(workgroup)
	dtracef("%s browser AnnouncementRequest → %s (solicit re-announce)", c.proto, dst.String())
	return c.SendMailslot(mailslotproto.NameBrowse, dst, c.announcementRequestBody(), true)
}

// announcementRequestBody builds the browser AnnouncementRequest with our station's computer
// name as the ResponseName. The ResponseName is NOT optional on the wire: a real Win98/NT
// browser rejects an AnnouncementRequest that carries no NUL-terminated response computer
// name (Wireshark flags it "Malformed Packet: BROWSER") and never re-announces — the exact
// reason a solicit saw nothing. The responding host unicasts its HostAnnouncement to this
// name, and since we're a NetBIOS station on the segment it reaches us.
func (c *Conn) announcementRequestBody() []byte {
	return browserproto.AnnouncementRequest{ResponseName: c.srcName.String()}.Marshal()
}

// decodeFrame strips this carrier's L2 encapsulation from one inbound frame and, if it
// carries a browser announcement, returns the Host it describes (else nil). It is the
// receive mirror of sendNBF / sendNBIPX and reuses the same core codecs the server
// ingests announcements with.
func (c *Conn) decodeFrame(frame []byte) *Host {
	payload, addr := c.browserDatagram(frame)
	if payload == nil {
		return nil
	}
	return announcementToHost(payload, c.proto, addr)
}

// browserPayload strips this carrier's framing from one inbound frame and returns the
// mailslot datagram payload (the SMB_COM_TRANSACTION mailslot write), or nil if the frame
// is not a NetBIOS datagram for this carrier. It is the shared unwrap used by both the
// announcement decode (decodeFrame) and the GetBackupList-response decode (masterbrowse.go).
func (c *Conn) browserPayload(frame []byte) []byte {
	payload, _ := c.browserDatagram(frame)
	return payload
}

// browserDatagram strips this carrier's L2 encapsulation and returns the mailslot payload
// plus the printable source address (MAC for NBF, IPX net.node for NBIPX), or nil.
func (c *Conn) browserDatagram(frame []byte) ([]byte, string) {
	if len(frame) < ethHdrLen {
		return nil, ""
	}
	switch {
	case c.proto == NBF:
		return c.decodeNBFDatagram(frame)
	case ipxFamily(c.proto):
		return c.decodeNBIPXDatagram(frame)
	}
	return nil, ""
}

// decodeNBFDatagram decodes an NBF UI datagram frame to its mailslot payload. The 802.3
// body must carry the NetBIOS LLC header (0xF0 0xF0 0x03); the NBF frame must be a
// DATAGRAM / DATAGRAM_BROADCAST.
func (c *Conn) decodeNBFDatagram(frame []byte) ([]byte, string) {
	body := frame[ethHdrLen:]
	if len(body) < 3 || body[0] != llcNetBIOS[0] || body[1] != llcNetBIOS[1] || body[2] != llcNetBIOS[2] {
		return nil, ""
	}
	f, err := nbf.Decode(body[3:])
	if err != nil || (f.Command != nbf.CmdDatagram && f.Command != nbf.CmdDatagramBroadcast) {
		return nil, ""
	}
	var srcMAC [6]byte
	copy(srcMAC[:], frame[6:12])
	return f.Payload, macString(srcMAC)
}

// decodeNBIPXDatagram decodes an NMPI MailslotSend frame on the NB-IPX datagram socket
// (0x0553) to its mailslot payload. The IPX source net.node is the printable address.
//
// BOTH IPX packet types are accepted, because a browser exchange uses both: the fan-out
// half (Host/LocalMaster announcements, AnnouncementRequest, election, GetBackupList
// request) is type 20 / IPXTypeNetBIOS broadcast, but the master's UNICAST answer comes
// back as type 4 / IPXTypePEP — golden spec/captures/nbipx-win98.pcap frame 60 and
// nwlink-win98.pcap frame 41 are both "Get Backup List Response", type 0x04, socket
// 0x0553. Accepting only type 20 dropped exactly the frame that names the master, which
// is why an NBIPX FindMaster silently returned nothing while NBF (whose reply rides the
// same UI datagram as the request) worked. The socket check replaces the type as the
// discriminator that keeps session/name-service IPX traffic out of the browser decode.
func (c *Conn) decodeNBIPXDatagram(frame []byte) ([]byte, string) {
	etherType := uint16(frame[12])<<8 | uint16(frame[13])
	if etherType != etherTypeIPX {
		return nil, ""
	}
	d, err := ipxproto.Decode(frame[ethHdrLen:])
	if err != nil || (d.Type != ipxNetBIOSTyp && d.Type != ipxPEPTyp) || d.SrcSock != nbDatagramSocket {
		return nil, ""
	}
	nmpi, err := nb.DecodeNMPIPacket(d.Payload)
	if err != nil || nmpi.Opcode != nb.NMPIOpMailslotSend {
		return nil, ""
	}
	addr := fmt.Sprintf("%s.%s", netString(d.SrcNet), macString(d.SrcNode))
	return nmpi.Payload, addr
}

// announcementToHost unwraps the mailslot envelope and the browser frame from a datagram
// payload and builds a Host from a host / local-master / domain announcement. Returns nil
// for any other mailslot or browser opcode (a GetBackupList, an election, a net-send).
func announcementToHost(payload []byte, proto Protocol, addr string) *Host {
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
		return &Host{
			Name:       browserproto.NormalizeName(a.ServerName),
			Protocol:   proto,
			Address:    addr,
			OSVersion:  fmt.Sprintf("%d.%d", a.OSVersionMajor, a.OSVersionMinor),
			AppVersion: fmt.Sprintf("%d.%d", a.VersionMajor, a.VersionMinor),
			Comment:    a.Comment,
			Role:       role,
			LastSeen:   time.Now(),
		}
	case browserproto.OpDomainAnnouncement:
		da, err := browserproto.UnmarshalDomainAnnouncement(frame)
		if err != nil {
			return nil
		}
		return &Host{
			Name:     browserproto.NormalizeName(da.LocalMaster),
			Protocol: proto,
			Address:  addr,
			Role:     "domain master",
			Comment:  "workgroup " + browserproto.NormalizeName(da.MachineGroup),
			LastSeen: time.Now(),
		}
	}
	return nil
}

// mergeHost inserts or updates a host by name, preferring the newest announcement and not
// losing a richer field (version/comment) to a sparser later one (e.g. a domain
// announcement that carries no version).
func mergeHost(hosts map[string]*Host, h *Host) {
	if h.Name == "" {
		return
	}
	existing := hosts[h.Name]
	if existing == nil {
		hosts[h.Name] = h
		return
	}
	existing.LastSeen = h.LastSeen
	existing.Protocol = h.Protocol
	existing.Address = h.Address
	if h.OSVersion != "" && h.OSVersion != "0.0" {
		existing.OSVersion = h.OSVersion
	}
	if h.AppVersion != "" && h.AppVersion != "0.0" {
		existing.AppVersion = h.AppVersion
	}
	if h.Comment != "" {
		existing.Comment = h.Comment
	}
	if h.Role == "master" || h.Role == "domain master" {
		existing.Role = h.Role
	}
}

// sortedHosts flattens the host map into a slice sorted by name.
func sortedHosts(hosts map[string]*Host) []Host {
	out := make([]Host, 0, len(hosts))
	for _, h := range hosts {
		out = append(out, *h)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// netString formats a 4-byte IPX network number as 8 hex digits.
func netString(n [4]byte) string {
	const hex = "0123456789abcdef"
	out := make([]byte, 0, 8)
	for _, b := range n {
		out = append(out, hex[b>>4], hex[b&0x0F])
	}
	return string(out)
}
