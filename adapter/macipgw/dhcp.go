package macipgw

import (
	"context"
	"encoding/binary"
	"log/slog"
	"math/rand"
	"net"
	"sync"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/adapter/macipgw/nat"
)

const (
	dhcpServerPort = 67
	dhcpClientPort = 68
	dhcpTimeout    = 10 * time.Second

	dhcpBootRequest = 1
	dhcpBootReply   = 2

	dhcpMsgDiscover = 1
	dhcpMsgOffer    = 2
	dhcpMsgRequest  = 3
	dhcpMsgAck      = 5
	dhcpMsgNak      = 6

	dhcpOptPad         = 0
	dhcpOptSubnetMask  = 1
	dhcpOptRouter      = 3
	dhcpOptDNS         = 6
	dhcpOptBroadcast   = 28
	dhcpOptLeaseTime   = 51
	dhcpOptMsgType     = 53
	dhcpOptServerID    = 54
	dhcpOptRequestedIP = 50
	dhcpOptParamReq    = 55
	dhcpOptClientID    = 61
	dhcpOptEnd         = 255

	dhcpMagic = 0x63825363
)

// dhcpResult holds the configuration received from a DHCP Ack. Fields mirror common
// DHCP options returned by the server.
type dhcpResult struct {
	assignedIP net.IP
	mask       net.IPMask
	router     net.IP
	nameserver net.IP
	broadcast  net.IP
	leaseTime  uint32
}

// pendingDHCP tracks an in-progress DHCP transaction for a single fabricated
// AppleTalk client, keyed by the DHCP transaction id (xid).
type pendingDHCP struct {
	xid      uint32
	fabMAC   net.HardwareAddr
	atNet    uint16
	atNode   uint8
	ch       chan *dhcpResult
	offered  net.IP
	serverID net.IP
}

// dhcpClient performs DHCP on behalf of Mac clients, using the IP-side link to
// send and receive DHCP frames. Ported from the legacy service/macip/dhcp_client.go.
type dhcpClient struct {
	link *etherIPLink
	log  *slog.Logger
	stop <-chan struct{}

	mu      sync.Mutex
	pending map[uint32]*pendingDHCP
}

// newDHCPClient constructs a dhcpClient over the provided IP link. stop is the
// egress lifecycle channel; once closed, in-flight transactions return early.
func newDHCPClient(link *etherIPLink, log *slog.Logger, stop <-chan struct{}) *dhcpClient {
	if log == nil {
		log = slog.Default()
	}
	return &dhcpClient{
		link:    link,
		log:     log,
		stop:    stop,
		pending: make(map[uint32]*pendingDHCP),
	}
}

// handleReply processes a raw DHCP reply payload received from the IP link (the
// etherlink's onDHCP callback), correlating it with a pending transaction by xid.
func (c *dhcpClient) handleReply(pkt []byte) {
	// Minimum: 236-byte fixed header + 4-byte magic + at least option-end.
	if len(pkt) < 241 {
		return
	}
	if pkt[0] != dhcpBootReply {
		return
	}
	if binary.BigEndian.Uint32(pkt[236:240]) != dhcpMagic {
		return
	}
	xid := binary.BigEndian.Uint32(pkt[4:8])
	yiaddr := net.IP(append([]byte(nil), pkt[16:20]...)).To4()

	c.mu.Lock()
	p := c.pending[xid]
	c.mu.Unlock()
	if p == nil {
		return
	}

	msgType, opts := parseDHCPOptions(pkt[240:])
	switch msgType {
	case dhcpMsgOffer:
		p.offered = yiaddr
		if sid, ok := opts[dhcpOptServerID]; ok && len(sid) >= 4 {
			p.serverID = net.IP(append([]byte(nil), sid[:4]...)).To4()
		}
		c.sendRequest(p)

	case dhcpMsgAck:
		res := &dhcpResult{assignedIP: yiaddr}
		if v, ok := opts[dhcpOptSubnetMask]; ok && len(v) == 4 {
			res.mask = net.IPMask(append([]byte(nil), v...))
		}
		if v, ok := opts[dhcpOptRouter]; ok && len(v) >= 4 {
			res.router = net.IP(append([]byte(nil), v[:4]...)).To4()
		}
		if v, ok := opts[dhcpOptDNS]; ok && len(v) >= 4 {
			res.nameserver = net.IP(append([]byte(nil), v[:4]...)).To4()
		}
		if v, ok := opts[dhcpOptBroadcast]; ok && len(v) >= 4 {
			res.broadcast = net.IP(append([]byte(nil), v[:4]...)).To4()
		}
		if v, ok := opts[dhcpOptLeaseTime]; ok && len(v) == 4 {
			res.leaseTime = binary.BigEndian.Uint32(v)
		}
		select {
		case p.ch <- res:
		default:
		}

	case dhcpMsgNak:
		c.log.Debug("macipgw-dhcp: NAK", "at_net", p.atNet, "at_node", p.atNode, "xid", xid)
		select {
		case p.ch <- nil:
		default:
		}
	}
}

// RequestIP performs the full DHCP Discover→Offer→Request→Ack handshake for the
// given AppleTalk node. Returns nil on failure, timeout, shutdown, or ctx cancel.
func (c *dhcpClient) RequestIP(ctx context.Context, atNet uint16, atNode uint8, preferredIP net.IP) *dhcpResult {
	// #nosec G404 -- the DHCP xid just needs to be unpredictable enough to correlate
	// replies on a trusted LAN, not cryptographically random.
	xid := rand.Uint32()
	fabMAC := fabricateMACForAT(atNet, atNode)
	p := &pendingDHCP{
		xid:    xid,
		fabMAC: fabMAC,
		atNet:  atNet,
		atNode: atNode,
		ch:     make(chan *dhcpResult, 1),
	}
	c.mu.Lock()
	c.pending[xid] = p
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, xid)
		c.mu.Unlock()
	}()

	c.sendDiscover(p, preferredIP)

	timer := time.NewTimer(dhcpTimeout)
	defer timer.Stop()
	select {
	case res := <-p.ch:
		return res // nil on NAK
	case <-ctx.Done():
		return nil
	case <-c.stop:
		return nil
	case <-timer.C:
		c.log.Debug("macipgw-dhcp: timeout waiting for Ack", "at_net", atNet, "at_node", atNode, "xid", xid)
		return nil
	}
}

// sendDiscover constructs and transmits a DHCP Discover for a pending transaction.
func (c *dhcpClient) sendDiscover(p *pendingDHCP, preferredIP net.IP) {
	payload := buildDHCPPacket(dhcpMsgDiscover, p.xid, p.fabMAC, preferredIP, nil)
	c.sendBroadcastUDP(payload)
}

// sendRequest constructs and transmits a DHCP Request using the offered address.
func (c *dhcpClient) sendRequest(p *pendingDHCP) {
	payload := buildDHCPPacket(dhcpMsgRequest, p.xid, p.fabMAC, p.offered, p.serverID)
	c.sendBroadcastUDP(payload)
}

// parseDHCPOptions parses the options area, returning the message type and a map of
// option code → raw value.
func parseDHCPOptions(data []byte) (msgType byte, opts map[byte][]byte) {
	opts = make(map[byte][]byte)
	for i := 0; i < len(data); {
		code := data[i]
		if code == dhcpOptEnd {
			break
		}
		if code == dhcpOptPad {
			i++
			continue
		}
		if i+1 >= len(data) {
			break
		}
		l := int(data[i+1])
		if i+2+l > len(data) {
			break
		}
		val := data[i+2 : i+2+l]
		if code == dhcpOptMsgType && l >= 1 {
			msgType = val[0]
		}
		opts[code] = append([]byte(nil), val...)
		i += 2 + l
	}
	return
}

// buildDHCPPacket constructs a DHCP Discover or Request packet.
func buildDHCPPacket(msgType byte, xid uint32, chaddr net.HardwareAddr, requestedIP, serverID net.IP) []byte {
	var opts []byte
	opts = dhcpAppendOpt(opts, dhcpOptMsgType, []byte{msgType})
	if requestedIP != nil && !requestedIP.Equal(net.IPv4zero) {
		opts = dhcpAppendOpt(opts, dhcpOptRequestedIP, requestedIP.To4())
	}
	if serverID != nil {
		opts = dhcpAppendOpt(opts, dhcpOptServerID, serverID.To4())
	}
	// Ask for subnet mask, router, DNS, broadcast address, lease time.
	opts = dhcpAppendOpt(opts, dhcpOptParamReq, []byte{dhcpOptSubnetMask, 3, dhcpOptDNS, dhcpOptBroadcast, dhcpOptLeaseTime})
	// Client identifier: type 1 (Ethernet) + fabricated MAC.
	opts = dhcpAppendOpt(opts, dhcpOptClientID, append([]byte{1}, chaddr...))
	opts = append(opts, dhcpOptEnd)

	// Fixed 236-byte DHCP header + 4-byte magic cookie + options.
	pkt := make([]byte, 240+len(opts))
	pkt[0] = dhcpBootRequest
	pkt[1] = 1 // htype: Ethernet
	pkt[2] = 6 // hlen: 6 bytes
	binary.BigEndian.PutUint32(pkt[4:8], xid)
	binary.BigEndian.PutUint16(pkt[10:12], 0x8000) // broadcast flag
	copy(pkt[28:34], chaddr)                       // chaddr
	binary.BigEndian.PutUint32(pkt[236:240], dhcpMagic)
	copy(pkt[240:], opts)
	return pkt
}

// dhcpAppendOpt appends a DHCP option (code, length, value).
func dhcpAppendOpt(opts []byte, code byte, val []byte) []byte {
	return append(append(opts, code, byte(len(val))), val...)
}

// sendBroadcastUDP wraps payload in UDP/IP and sends it as an Ethernet broadcast
// (src 0.0.0.0:68, dst 255.255.255.255:67).
func (c *dhcpClient) sendBroadcastUDP(payload []byte) {
	udp := make([]byte, 8+len(payload))
	binary.BigEndian.PutUint16(udp[0:2], dhcpClientPort)
	binary.BigEndian.PutUint16(udp[2:4], dhcpServerPort)
	binary.BigEndian.PutUint16(udp[4:6], uint16(8+len(payload)))
	// udp[6:8] = checksum = 0 (optional for IPv4 UDP)
	copy(udp[8:], payload)

	ip := nat.BuildIPv4Packet([]byte{0, 0, 0, 0}, []byte{255, 255, 255, 255}, 17, udp)

	frame := make([]byte, 14+len(ip))
	for i := 0; i < 6; i++ {
		frame[i] = 0xff // Ethernet broadcast
	}
	copy(frame[6:12], c.link.ourMAC)
	binary.BigEndian.PutUint16(frame[12:14], etherTypeIPv4)
	copy(frame[14:], ip)

	if err := c.link.sendFrame(frame); err != nil {
		c.log.Debug("macipgw-dhcp: send error", "err", err)
	}
}
