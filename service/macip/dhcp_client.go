//go:build macip || all

// Package macip implements a minimal DHCP client used by the MacIP
// gateway. It performs DHCP discover/request sequences on behalf of
// AppleTalk clients by fabricating per-node Ethernet addresses and
// sending/receiving DHCP over a pcap-backed IP link.
package macip

import (
	"context"
	"encoding/binary"
	"math/rand"
	"net"
	"sync"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/netlog"
	"github.com/ObsoleteMadness/ClassicStack/pkg/hwaddr"
	"github.com/ObsoleteMadness/ClassicStack/port/nat"
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

// dhcpResult holds the configuration received from a DHCP Ack.
// Fields mirror common DHCP options returned by the server.
type dhcpResult struct {
	// assignedIP is the IPv4 address allocated to the client.
	assignedIP net.IP
	// mask is the subnet mask (option 1) returned by the server.
	mask net.IPMask
	// router is the default gateway (option 3) returned by the server.
	router net.IP
	// nameserver is the DNS server (option 6) returned by the server.
	nameserver net.IP
	// broadcast is the broadcast address (option 28) returned by the server.
	broadcast net.IP
	// leaseTime is the lease duration in seconds (option 51).
	leaseTime uint32
}

// pendingDHCP tracks an in-progress DHCP transaction for a single
// fabricated AppleTalk client. It is stored in the dhcpClient.pending
// map keyed by the DHCP transaction id (xid).
type pendingDHCP struct {
	// xid is the DHCP transaction identifier for this exchange.
	xid uint32
	// fabMAC is the fabricated Ethernet MAC address used as the client
	// hardware address for DHCP requests.
	fabMAC net.HardwareAddr
	// atNet and atNode identify the AppleTalk node this request is for.
	atNet  uint16
	atNode uint8
	// ch is used to deliver the final dhcpResult. A nil result indicates
	// a NAK or an error/timeout.
	ch chan *dhcpResult
	// offered is the IP address offered by the DHCP server in an Offer.
	offered net.IP
	// serverID is the server identifier (option 54) provided by the server.
	serverID net.IP
}

// dhcpClient performs DHCP on behalf of Mac clients, using the IP-side
// link to send and receive DHCP frames.
type dhcpClient struct {
	// link is the IPv4 link used to transmit/receive packets.
	link *etherIPLink

	// stop signals service shutdown; in-flight RequestIP calls abort
	// instead of blocking on dhcpTimeout.
	stop <-chan struct{}

	// mu protects the pending map.
	mu sync.Mutex
	// pending maps DHCP transaction ids to active pendingDHCP entries.
	pending map[uint32]*pendingDHCP
}

// newDHCPClient constructs a dhcpClient that will use the provided
// IP link to perform DHCP transactions. stop is the service's lifecycle
// channel; once closed, in-flight DHCP transactions return early.
func newDHCPClient(link *etherIPLink, stop <-chan struct{}) *dhcpClient {
	return &dhcpClient{
		link:    link,
		stop:    stop,
		pending: make(map[uint32]*pendingDHCP),
	}
}

// run reads DHCP responses from the pcap link and dispatches them.
// It exits when the provided stop channel is closed.
func (c *dhcpClient) run(stop <-chan struct{}) {
	for {
		select {
		case <-stop:
			return
		case pkt := <-c.link.dhcpInbound:
			c.handlePacket(pkt)
		}
	}
}

// fabricateMACForAT builds a locally administered Ethernet MAC from an
// AppleTalk address, giving each Mac a stable identity for the DHCP server.
func fabricateMACForAT(atNet uint16, atNode uint8) net.HardwareAddr {
	e := hwaddr.MacIPEthernetFromAppleTalk(hwaddr.AppleTalk{Network: atNet, Node: atNode})
	return e.HardwareAddr()
}

// RequestIP performs the full DHCP Discover→Offer→Request→Ack handshake for
// the given AppleTalk node. If preferredIP is non-nil it is sent as option 50.
// Returns nil if DHCP fails, times out, the service stops, or ctx is cancelled.
func (c *dhcpClient) RequestIP(ctx context.Context, atNet uint16, atNode uint8, preferredIP net.IP) *dhcpResult {
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
		netlog.Debug("[macip-dhcp] aborting DHCP wait for AT %d.%d xid=0x%08x: %v", atNet, atNode, xid, ctx.Err())
		return nil
	case <-c.stop:
		netlog.Debug("[macip-dhcp] aborting DHCP wait for AT %d.%d xid=0x%08x: service stopping", atNet, atNode, xid)
		return nil
	case <-timer.C:
		netlog.Debug("[macip-dhcp] timeout waiting for Ack AT %d.%d xid=0x%08x", atNet, atNode, xid)
		return nil
	}
}

// handlePacket processes a raw DHCP packet received from the pcap link,
// validates it, extracts DHCP options, and delivers the result to the
// matching pendingDHCP entry (by xid). It ignores packets that are not
// DHCP replies or that do not match any active transaction.
func (c *dhcpClient) handlePacket(pkt []byte) {
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
	netlog.Debug("[macip-dhcp] recv type=%d xid=0x%08x yiaddr=%s", msgType, xid, yiaddr)

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
		netlog.Debug("[macip-dhcp] NAK for AT %d.%d xid=0x%08x", p.atNet, p.atNode, xid)
		select {
		case p.ch <- nil:
		default:
		}
	}
}

// sendDiscover constructs and transmits a DHCP Discover packet for the
// provided pendingDHCP entry. If a preferred IP is provided it is
// included as option 50.
func (c *dhcpClient) sendDiscover(p *pendingDHCP, preferredIP net.IP) {
	payload := buildDHCPPacket(dhcpMsgDiscover, p.xid, p.fabMAC, preferredIP, nil)
	c.sendBroadcastUDP(payload)
	netlog.Debug("[macip-dhcp] Discover AT %d.%d xid=0x%08x preferredIP=%s", p.atNet, p.atNode, p.xid, preferredIP)
}

// parseDHCPOptions parses DHCP options from the options area and returns
// the DHCP message type (if present) and a map of option code -> raw value.
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

// sendRequest constructs and transmits a DHCP Request packet for the
// provided pendingDHCP entry using the offered address and server ID
// learned from the Offer.
func (c *dhcpClient) sendRequest(p *pendingDHCP) {
	payload := buildDHCPPacket(dhcpMsgRequest, p.xid, p.fabMAC, p.offered, p.serverID)
	c.sendBroadcastUDP(payload)
	netlog.Debug("[macip-dhcp] Request AT %d.%d xid=0x%08x ip=%s", p.atNet, p.atNode, p.xid, p.offered)
}

// buildDHCPPacket constructs a DHCP Discover or Request packet.
// requestedIP = the IP being requested (option 50); serverID = option 54 (Request only).
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
	binary.BigEndian.PutUint16(pkt[10:12], 0x8000) // broadcast flag: reply to 255.255.255.255
	copy(pkt[28:34], chaddr)                       // chaddr
	binary.BigEndian.PutUint32(pkt[236:240], dhcpMagic)
	copy(pkt[240:], opts)
	return pkt
}

// dhcpAppendOpt appends a DHCP option (code, length, value) to the
// provided options slice and returns the extended slice.
func dhcpAppendOpt(opts []byte, code byte, val []byte) []byte {
	return append(append(opts, code, byte(len(val))), val...)
}

// sendBroadcastUDP wraps payload in UDP/IP and sends it as an Ethernet broadcast.
// src=0.0.0.0:68, dst=255.255.255.255:67 (standard DHCP client→server).
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
		netlog.Debug("[macip-dhcp] send error: %v", err)
	}
}
