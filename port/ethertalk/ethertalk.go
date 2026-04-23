package ethertalk

import (
	"bytes"
	"encoding/binary"
	"math/rand"
	"sync"
	"time"

	"github.com/pgodw/omnitalk/protocol/ddp"

	"github.com/pgodw/omnitalk/netlog"
	"github.com/pgodw/omnitalk/port"
)

var (
	ieee8022Type1  = []byte{0xAA, 0xAA, 0x03}
	snapAARP       = []byte{0x00, 0x00, 0x00, 0x80, 0xF3}
	snapAppleTalk  = []byte{0x08, 0x00, 0x07, 0x80, 0x9B}
	aarpHeader     = append(append(append([]byte{}, ieee8022Type1...), snapAARP...), []byte{0x00, 0x01, 0x80, 0x9B, 6, 4}...)
	aarpValidation = aarpHeader[8:14] // {0x00,0x01,0x80,0x9B,0x06,0x04}
	elapBroadcast  = []byte{0x09, 0x00, 0x07, 0xFF, 0xFF, 0xFF}
	elapMCprefix   = []byte{0x09, 0x00, 0x07, 0x00, 0x00}
)

const (
	aarpFuncRequest  = 1
	aarpFuncResponse = 2
	aarpFuncProbe    = 3

	aarpProbeTimeout = 200 * time.Millisecond
	aarpProbeRetries = 10

	amtMaxAge      = 10 * time.Second
	amtAgeInterval = 1 * time.Second

	heldMaxAge      = 10 * time.Second
	heldAgeInterval = 1 * time.Second
	heldAARPRetry   = 250 * time.Millisecond
)

type FrameTx func(frame []byte) error

type amtEntry struct {
	hw   []byte
	when time.Time
}

type heldDatagram struct {
	d    ddp.Datagram
	when time.Time
}

type Port struct {
	hwAddr        []byte
	seedZoneNames [][]byte
	router        port.RouterHooks
	tx            FrameTx
	networkMin    uint16
	networkMax    uint16

	addrMu  sync.RWMutex
	network uint16
	node    uint8

	probeMu       sync.Mutex
	probeAttempts int
	probeNetwork  uint16
	probeNode     uint8
	probeNets     []uint16
	probeNodes    []uint8

	tableMu       sync.Mutex
	amt           map[[2]uint16]amtEntry
	heldDatagrams map[[2]uint16][]heldDatagram

	wg   sync.WaitGroup
	stop chan struct{}
}

func New(hwAddr []byte, seedNetworkMin, seedNetworkMax, desiredNetwork uint16, desiredNode uint8, seedZoneNames [][]byte) *Port {
	p := &Port{
		hwAddr:        append([]byte(nil), hwAddr...),
		networkMin:    seedNetworkMin,
		networkMax:    seedNetworkMax,
		seedZoneNames: seedZoneNames,
		amt:           map[[2]uint16]amtEntry{},
		heldDatagrams: map[[2]uint16][]heldDatagram{},
		stop:          make(chan struct{}),
	}
	if seedNetworkMin != 0 && seedNetworkMax != 0 {
		p.probeMu.Lock()
		if desiredNetwork >= seedNetworkMin && desiredNetwork <= seedNetworkMax {
			p.probeNets = []uint16{desiredNetwork}
		}
		if desiredNode >= 1 && desiredNode <= 0xFD {
			p.probeNodes = []uint8{desiredNode}
		}
		p.rerollProbeState()
		p.probeMu.Unlock()
	}
	return p
}

// rerollProbeState picks a new (probeNetwork, probeNode) and resets probeAttempts.
// Must be called with probeMu held.
func (p *Port) rerollProbeState() {
	if len(p.probeNodes) == 0 {
		if len(p.probeNets) == 0 {
			if p.networkMin == 0 || p.networkMax == 0 {
				return
			}
			nets := make([]uint16, 0, int(p.networkMax)-int(p.networkMin)+1)
			for n := p.networkMin; n <= p.networkMax; n++ {
				nets = append(nets, n)
			}
			rand.Shuffle(len(nets), func(i, j int) { nets[i], nets[j] = nets[j], nets[i] })
			p.probeNets = nets
		}
		p.probeNetwork = p.probeNets[len(p.probeNets)-1]
		p.probeNets = p.probeNets[:len(p.probeNets)-1]
		nodes := make([]uint8, 0xFD) // 1..253
		for i := range nodes {
			nodes[i] = uint8(i + 1)
		}
		rand.Shuffle(len(nodes), func(i, j int) { nodes[i], nodes[j] = nodes[j], nodes[i] })
		p.probeNodes = nodes
	}
	if len(p.probeNodes) == 0 {
		return
	}
	p.probeNode = p.probeNodes[len(p.probeNodes)-1]
	p.probeNodes = p.probeNodes[:len(p.probeNodes)-1]
	p.probeAttempts = 0
}

func (p *Port) ConfigureTx(tx FrameTx) { p.tx = tx }
func (p *Port) ShortString() string    { return "EtherTalk" }

func (p *Port) Network() uint16 { p.addrMu.RLock(); defer p.addrMu.RUnlock(); return p.network }
func (p *Port) Node() uint8     { p.addrMu.RLock(); defer p.addrMu.RUnlock(); return p.node }

func (p *Port) NetworkMin() uint16    { return p.networkMin }
func (p *Port) NetworkMax() uint16    { return p.networkMax }
func (p *Port) ExtendedNetwork() bool { return true }

func (p *Port) Start(r port.RouterHooks) error {
	p.router = r

	if p.networkMin != 0 && p.networkMax != 0 {
		if rs, ok := r.(interface {
			RoutingSetPortRange(pt port.Port, networkMin, networkMax uint16)
		}); ok {
			rs.RoutingSetPortRange(p, p.networkMin, p.networkMax)
		}
	}

	p.wg.Add(4)
	go p.acquireAddressRun()
	go p.amtAgeRun()
	go p.heldAgeRun()
	go p.aarpRetryRun()
	return nil
}

func (p *Port) Stop() error {
	close(p.stop)
	p.wg.Wait()
	return nil
}

func (p *Port) SetNetworkRange(nmin, nmax uint16) error {
	netlog.Info("%s assigned network number range %d-%d", p.ShortString(), nmin, nmax)
	p.networkMin = nmin
	p.networkMax = nmax

	if rs, ok := p.router.(interface {
		RoutingSetPortRange(pt port.Port, networkMin, networkMax uint16)
	}); ok {
		rs.RoutingSetPortRange(p, nmin, nmax)
	}

	p.addrMu.Lock()
	p.network = 0
	p.node = 0
	p.addrMu.Unlock()

	p.probeMu.Lock()
	p.probeNets = nil
	p.probeNodes = nil
	p.rerollProbeState()
	p.probeMu.Unlock()
	return nil
}

// acquireAddressRun sends AARP probes then claims an address.
func (p *Port) acquireAddressRun() {
	defer p.wg.Done()

	if p.networkMin != 0 && p.networkMax != 0 {
		p.probeMu.Lock()
		p.probeNets = nil
		p.probeNodes = nil
		p.rerollProbeState()
		p.probeMu.Unlock()
	}

	// Register seed zones once at startup.
	if p.networkMin != 0 && p.networkMax != 0 && len(p.seedZoneNames) > 0 {
		if za, ok := p.router.(interface {
			AddNetworksToZone(zoneName []byte, networkMin uint16, networkMax *uint16) error
		}); ok {
			nmax := p.networkMax
			for _, name := range p.seedZoneNames {
				_ = za.AddNetworksToZone(name, p.networkMin, &nmax)
			}
		}
	}

	ticker := time.NewTicker(aarpProbeTimeout)
	defer ticker.Stop()
	for {
		select {
		case <-p.stop:
			return
		case <-ticker.C:
			p.addrMu.RLock()
			hasAddr := p.network != 0
			p.addrMu.RUnlock()
			if hasAddr {
				continue
			}

			p.probeMu.Lock()
			if p.probeNetwork == 0 || p.probeNode == 0 {
				p.probeMu.Unlock()
				continue
			}
			if p.probeAttempts >= aarpProbeRetries {
				claimNet := p.probeNetwork
				claimNd := p.probeNode
				p.probeMu.Unlock()
				p.addrMu.Lock()
				p.network = claimNet
				p.node = claimNd
				p.addrMu.Unlock()
				netlog.Info("%s claiming address %d.%d", p.ShortString(), claimNet, claimNd)
				continue
			}
			probeNet := p.probeNetwork
			probeNd := p.probeNode
			p.probeAttempts++
			p.probeMu.Unlock()
			p.sendAARPProbe(probeNet, probeNd)
		}
	}
}

func (p *Port) amtAgeRun() {
	defer p.wg.Done()
	ticker := time.NewTicker(amtAgeInterval)
	defer ticker.Stop()
	for {
		select {
		case <-p.stop:
			return
		case <-ticker.C:
			now := time.Now()
			p.tableMu.Lock()
			for k, e := range p.amt {
				if now.Sub(e.when) >= amtMaxAge {
					delete(p.amt, k)
				}
			}
			p.tableMu.Unlock()
		}
	}
}

func (p *Port) heldAgeRun() {
	defer p.wg.Done()
	ticker := time.NewTicker(heldAgeInterval)
	defer ticker.Stop()
	for {
		select {
		case <-p.stop:
			return
		case <-ticker.C:
			now := time.Now()
			p.tableMu.Lock()
			for k, ds := range p.heldDatagrams {
				var remaining []heldDatagram
				for _, hd := range ds {
					if now.Sub(hd.when) < heldMaxAge {
						remaining = append(remaining, hd)
					}
				}
				if len(remaining) == 0 {
					delete(p.heldDatagrams, k)
				} else {
					p.heldDatagrams[k] = remaining
				}
			}
			p.tableMu.Unlock()
		}
	}
}

// aarpRetryRun periodically retransmits AARP requests for destinations with held datagrams,
func (p *Port) aarpRetryRun() {
	defer p.wg.Done()
	ticker := time.NewTicker(heldAARPRetry)
	defer ticker.Stop()
	for {
		select {
		case <-p.stop:
			return
		case <-ticker.C:
			p.tableMu.Lock()
			keys := make([][2]uint16, 0, len(p.heldDatagrams))
			for k := range p.heldDatagrams {
				keys = append(keys, k)
			}
			p.tableMu.Unlock()
			for _, k := range keys {
				p.sendAARPRequest(k[0], uint8(k[1]))
			}
		}
	}
}

// addAddressMapping adds an entry to the AMT and flushes any held datagrams for that destination.
func (p *Port) addAddressMapping(network uint16, node uint8, hw []byte) {
	key := [2]uint16{network, uint16(node)}
	hwCopy := append([]byte(nil), hw...)
	p.tableMu.Lock()
	p.amt[key] = amtEntry{hw: hwCopy, when: time.Now()}
	held := p.heldDatagrams[key]
	delete(p.heldDatagrams, key)
	p.tableMu.Unlock()
	for _, hd := range held {
		p.sendDatagram(hwCopy, hd.d)
	}
}

// processAARPFrame handles an inbound AARP frame addressed to us.
func (p *Port) processAARPFrame(fn uint16, srcHW []byte, srcNetwork uint16, srcNode uint8) {
	switch fn {
	case aarpFuncRequest, aarpFuncProbe:
		p.sendAARPResponse(srcHW, srcNetwork, srcNode)
	case aarpFuncResponse:
		p.addAddressMapping(srcNetwork, srcNode, srcHW)
		// Collision detection: if we're still probing and the response matches our
		// desired address, reroll to a different address.
		p.addrMu.RLock()
		network := p.network
		node := p.node
		p.addrMu.RUnlock()
		if network == 0 && node == 0 {
			p.probeMu.Lock()
			if srcNetwork == p.probeNetwork && srcNode == p.probeNode {
				p.rerollProbeState()
			}
			p.probeMu.Unlock()
		}
	}
}

func (p *Port) sendFrame(dst, payload []byte) {
	pad := make([]byte, 0)
	if len(payload) < 46 {
		pad = make([]byte, 46-len(payload))
	}
	f := make([]byte, 0, 14+len(payload)+len(pad))
	f = append(f, dst...)
	f = append(f, p.hwAddr...)
	f = append(f, byte(len(payload)>>8), byte(len(payload)))
	f = append(f, payload...)
	f = append(f, pad...)
	netlog.LogEthernetFrameOutbound(f, p)
	_ = p.tx(f)
}

func (p *Port) sendDatagram(dst []byte, d ddp.Datagram) {
	b, err := d.AsLongHeaderBytes(true)
	if err != nil {
		return
	}
	payload := append(append([]byte{}, ieee8022Type1...), snapAppleTalk...)
	payload = append(payload, b...)
	p.sendFrame(dst, payload)
}

func (p *Port) sendAARPRequest(network uint16, node uint8) {
	p.addrMu.RLock()
	srcNet := p.network
	srcNode := p.node
	p.addrMu.RUnlock()
	if srcNet == 0 || srcNode == 0 {
		return
	}
	payload := make([]byte, 0, len(aarpHeader)+22)
	payload = append(payload, aarpHeader...)
	payload = append(payload, 0, aarpFuncRequest)
	payload = append(payload, p.hwAddr...)
	payload = append(payload, 0, byte(srcNet>>8), byte(srcNet), srcNode)
	payload = append(payload, 0, 0, 0, 0, 0, 0) // target hw: zero (6 bytes)
	payload = append(payload, 0, byte(network>>8), byte(network), node)
	p.sendFrame(elapBroadcast, payload)
}

func (p *Port) sendAARPResponse(dstHW []byte, dstNetwork uint16, dstNode uint8) {
	p.addrMu.RLock()
	srcNet := p.network
	srcNode := p.node
	p.addrMu.RUnlock()
	if srcNet == 0 || srcNode == 0 {
		return
	}
	payload := make([]byte, 0, len(aarpHeader)+22)
	payload = append(payload, aarpHeader...)
	payload = append(payload, 0, aarpFuncResponse)
	payload = append(payload, p.hwAddr...)
	payload = append(payload, 0, byte(srcNet>>8), byte(srcNet), srcNode)
	payload = append(payload, dstHW...)
	payload = append(payload, 0, byte(dstNetwork>>8), byte(dstNetwork), dstNode)
	p.sendFrame(dstHW, payload)
}

func (p *Port) sendAARPProbe(network uint16, node uint8) {
	payload := make([]byte, 0, len(aarpHeader)+22)
	payload = append(payload, aarpHeader...)
	payload = append(payload, 0, aarpFuncProbe)
	payload = append(payload, p.hwAddr...)
	payload = append(payload, 0, byte(network>>8), byte(network), node) // sender proto = desired addr
	payload = append(payload, 0, 0, 0, 0, 0, 0)                         // target hw: zero (6 bytes)
	payload = append(payload, 0, byte(network>>8), byte(network), node) // target proto = desired addr
	p.sendFrame(elapBroadcast, payload)
}

func (p *Port) InboundFrame(frame []byte) {
	if len(frame) < 22 || !bytes.Equal(frame[14:17], ieee8022Type1) {
		return
	}
	length := int(binary.BigEndian.Uint16(frame[12:14]))
	if length > len(frame)-14 {
		return
	}

	dstMAC := frame[0:6]

	if bytes.Equal(frame[17:22], snapAARP) {
		// AARP packet: must be exactly 36 bytes payload and have valid header.
		if length != 36 || len(frame) < 50 || !bytes.Equal(frame[22:28], aarpValidation) {
			return
		}
		netlog.LogEthernetFrameInbound(frame, p)
		fn := binary.BigEndian.Uint16(frame[28:30])
		srcHW := frame[30:36]
		srcNetwork := binary.BigEndian.Uint16(frame[37:39])
		srcNode := frame[39]
		targetNetwork := binary.BigEndian.Uint16(frame[47:49])
		targetNode := frame[49]

		if bytes.Equal(dstMAC, p.hwAddr) {
			// Unicast to our MAC: process unconditionally (handles unicast Responses).
			p.processAARPFrame(fn, srcHW, srcNetwork, srcNode)
		} else if (fn == aarpFuncRequest || fn == aarpFuncProbe) && bytes.Equal(dstMAC, elapBroadcast) {
			// Broadcast Request or Probe: respond only when the target is our claimed address.
			// This protects our address from being stolen by a node that probes for it.
			p.addrMu.RLock()
			ownNet, ownNode := p.network, p.node
			p.addrMu.RUnlock()
			if ownNet != 0 && targetNetwork == ownNet && targetNode == ownNode {
				p.processAARPFrame(fn, srcHW, srcNetwork, srcNode)
			}
		} else if fn == aarpFuncResponse {
			// Promiscuous AARP response: update AMT silently.
			p.addAddressMapping(srcNetwork, srcNode, srcHW)
		}
		return
	}

	if bytes.Equal(frame[17:22], snapAppleTalk) {
		netlog.LogEthernetFrameInbound(frame, p)
		d, err := ddp.DatagramFromLongHeaderBytes(frame[22:14+length], false)
		if err != nil {
			netlog.Debug("%s failed to parse AppleTalk datagram from EtherTalk frame: %v", p.ShortString(), err)
			return
		}
		// Populate AMT from zero-hop frames.
		if d.HopCount == 0 {
			p.addAddressMapping(d.SourceNetwork, d.SourceNode, frame[6:12])
		}
		// Destination filtering: only deliver to router if addressed to our MAC,
		// the EtherTalk broadcast, or a valid multicast address.
		if bytes.Equal(dstMAC, p.hwAddr) ||
			bytes.Equal(dstMAC, elapBroadcast) ||
			(bytes.Equal(dstMAC[0:5], elapMCprefix) && dstMAC[5] <= 0xFC) {
			netlog.LogDatagramInbound(p.Network(), p.Node(), d, p)
			p.router.Inbound(d, p)
		}
	}
}

func (p *Port) Unicast(network uint16, node uint8, d ddp.Datagram) {
	netlog.LogDatagramUnicast(network, node, d, p)
	key := [2]uint16{network, uint16(node)}
	p.tableMu.Lock()
	if entry, ok := p.amt[key]; ok {
		hw := append([]byte(nil), entry.hw...)
		p.tableMu.Unlock()
		p.sendDatagram(hw, d)
		return
	}
	// Hold the datagram while we wait for AARP resolution.
	_, alreadyHeld := p.heldDatagrams[key]
	p.heldDatagrams[key] = append(p.heldDatagrams[key], heldDatagram{d: d, when: time.Now()})
	p.tableMu.Unlock()
	if !alreadyHeld {
		// First datagram to this destination: send an AARP request immediately.
		netlog.Debug("%s Unicast: no AMT entry for %d.%d, sending AARP request", p.ShortString(), network, node)
		p.sendAARPRequest(network, node)
	}
}

func (p *Port) Broadcast(d ddp.Datagram) {
	if d.DestinationNetwork != 0 || d.DestinationNode != 0xFF {
		d.DestinationNetwork = 0
		d.DestinationNode = 0xFF
	}
	netlog.LogDatagramBroadcast(d, p)
	p.sendDatagram(elapBroadcast, d)
}

func (p *Port) Multicast(zoneName []byte, d ddp.Datagram) {
	netlog.LogDatagramMulticast(zoneName, d, p)
	// Use the EtherTalk-wide broadcast (09:00:07:FF:FF:FF) rather than the
	// zone-specific multicast.  All Phase 2 nodes must accept this address, whereas
	// zone-specific multicasts require the receiving NIC to have joined the group —
	// something many VM AppleTalk stacks do not do.
	p.sendDatagram(elapBroadcast, d)
}

func (p *Port) MulticastAddress(zoneName []byte) []byte {
	sum := ddp.Checksum(ucase(zoneName))
	return []byte{elapMCprefix[0], elapMCprefix[1], elapMCprefix[2], elapMCprefix[3], elapMCprefix[4], byte(sum % 0xFD)}
}

var atalkLower = []byte("abcdefghijklmnopqrstuvwxyz\x88\x8A\x8B\x8C\x8D\x8E\x96\x9A\x9B\x9F\xBE\xBF\xCF")
var atalkUpper = []byte("ABCDEFGHIJKLMNOPQRSTUVWXYZ\xCB\x80\xCC\x81\x82\x83\x84\x85\xCD\x86\xAE\xAF\xCE")

func ucase(input []byte) []byte {
	out := make([]byte, len(input))
	for i, b := range input {
		out[i] = b
		for j := range atalkLower {
			if atalkLower[j] == b {
				out[i] = atalkUpper[j]
				break
			}
		}
	}
	return out
}
