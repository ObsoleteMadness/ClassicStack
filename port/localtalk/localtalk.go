package localtalk

import (
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/protocol/ddp"

	"github.com/ObsoleteMadness/ClassicStack/netlog"
	"github.com/ObsoleteMadness/ClassicStack/port"
)

const (
	llapAppleTalkShortHeader = LLAPTypeAppleTalkShortHeader
	llapAppleTalkLongHeader  = LLAPTypeAppleTalkLongHeader
	llapENQ                  = LLAPTypeENQ
	llapACK                  = LLAPTypeACK
)

type FrameSender interface{ SendFrame(frame []byte) error }

type LinkManager interface {
	RegisterPort(p *Port)
	InboundFrame(p *Port, frame LLAPFrame)
	TransmitUnicast(p *Port, network uint16, node uint8, d ddp.Datagram)
	TransmitBroadcast(p *Port, d ddp.Datagram)
}

type Port struct {
	seedNetwork     uint16
	seedZoneName    []byte
	respondToEnq    bool
	supportsRTSCTS  bool
	rtsctsManaged   bool
	onNodeIDChange  func(node uint8)
	ctsTimeout      time.Duration
	desiredNode     uint8
	verifyChecksums bool
	calcChecksums   bool
	router          port.RouterHooks
	network         uint16
	node            uint8
	networkMin      uint16
	networkMax      uint16
	extendedNetwork bool
	nodeAttempts    int
	desiredNodeList []uint8
	mu              sync.Mutex
	stop            chan struct{}
	sendFrameFunc   func(frame []byte) error
	linkManager     LinkManager
}

func New(seedNetwork uint16, seedZoneName []byte, respondToEnq bool, desiredNode uint8) *Port {
	p := &Port{
		seedNetwork:     seedNetwork,
		seedZoneName:    seedZoneName,
		respondToEnq:    respondToEnq,
		ctsTimeout:      2 * time.Millisecond,
		desiredNode:     desiredNode,
		verifyChecksums: true,
		calcChecksums:   true,
		network:         seedNetwork,
		networkMin:      seedNetwork,
		networkMax:      seedNetwork,
		stop:            make(chan struct{}),
	}
	for i := uint8(1); i <= 0xFE; i++ {
		if i != desiredNode {
			p.desiredNodeList = append(p.desiredNodeList, i)
		}
	}
	rand.Shuffle(len(p.desiredNodeList), func(i, j int) {
		p.desiredNodeList[i], p.desiredNodeList[j] = p.desiredNodeList[j], p.desiredNodeList[i]
	})
	return p
}

func (p *Port) ConfigureSendFrame(f func(frame []byte) error) { p.sendFrameFunc = f }

// SetFrameSender wires the LocalTalk Port to a FrameSender backend. It
// is the interface-shaped counterpart to ConfigureSendFrame and the
// preferred way to attach new backends; ConfigureSendFrame remains for
// callers that already pass closures.
func (p *Port) SetFrameSender(fs FrameSender) { p.sendFrameFunc = fs.SendFrame }

func (p *Port) ShortString() string                       { return "LocalTalk" }
func (p *Port) SetLLAPLinkManager(m LinkManager)          { p.linkManager = m }
func (p *Port) SetNodeIDChangeHook(hook func(node uint8)) { p.onNodeIDChange = hook }

func (p *Port) SetCTSResponseTimeout(timeout time.Duration) {
	p.mu.Lock()
	p.ctsTimeout = timeout
	p.mu.Unlock()
}

func (p *Port) SetSupportsRTSCTS(enabled bool) {
	p.mu.Lock()
	p.supportsRTSCTS = enabled
	p.mu.Unlock()
}

func (p *Port) SetRTSCTSManagedByTransport(enabled bool) {
	p.mu.Lock()
	p.rtsctsManaged = enabled
	p.mu.Unlock()
}

func (p *Port) SendRawLLAPFrame(frame LLAPFrame) error {
	if err := frame.Validate(); err != nil {
		return err
	}
	b := frame.Bytes()
	netlog.LogLocaltalkFrameOutbound(b, p)
	return p.sendFrameFunc(b)
}

func (p *Port) BuildDataFrame(dst uint8, d ddp.Datagram) (LLAPFrame, error) {
	p.mu.Lock()
	src := p.node
	network := p.network
	calcChecksums := p.calcChecksums
	p.mu.Unlock()
	if src == 0 {
		return LLAPFrame{}, fmt.Errorf("localtalk node not yet claimed")
	}
	if d.DestinationNetwork == d.SourceNetwork && (d.DestinationNetwork == 0 || d.DestinationNetwork == network) {
		payload, err := d.AsShortHeaderBytes()
		if err != nil {
			return LLAPFrame{}, err
		}
		return LLAPFrame{DestinationNode: dst, SourceNode: src, Type: llapAppleTalkShortHeader, Payload: payload}, nil
	}
	payload, err := d.AsLongHeaderBytes(calcChecksums)
	if err != nil {
		return LLAPFrame{}, err
	}
	return LLAPFrame{DestinationNode: dst, SourceNode: src, Type: llapAppleTalkLongHeader, Payload: payload}, nil
}

func (p *Port) ParseInboundDataFrame(frame LLAPFrame) (ddp.Datagram, error) {
	switch frame.Type {
	case llapAppleTalkShortHeader:
		return ddp.DatagramFromShortHeaderBytes(frame.DestinationNode, frame.SourceNode, frame.Payload)
	case llapAppleTalkLongHeader:
		p.mu.Lock()
		verifyChecksums := p.verifyChecksums
		p.mu.Unlock()
		return ddp.DatagramFromLongHeaderBytes(frame.Payload, verifyChecksums)
	default:
		return ddp.Datagram{}, fmt.Errorf("not a LocalTalk data frame: 0x%02X", frame.Type)
	}
}

func (p *Port) DesiredNode() uint8 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.desiredNode
}

func (p *Port) ClaimedNode() uint8 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.node
}

func (p *Port) ClaimNode(node uint8) {
	p.mu.Lock()
	p.node = node
	hook := p.onNodeIDChange
	p.mu.Unlock()
	if hook != nil {
		hook(node)
	}
}

func (p *Port) ClearClaimedNode() {
	p.mu.Lock()
	p.node = 0
	hook := p.onNodeIDChange
	p.mu.Unlock()
	if hook != nil {
		hook(0)
	}
}

func (p *Port) RerollDesiredNode() uint8 {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.rerollDesiredNode()
	return p.desiredNode
}

func (p *Port) RespondToENQ() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.respondToEnq
}

func (p *Port) SupportsRTSCTS() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.supportsRTSCTS
}

func (p *Port) RTSCTSManagedByTransport() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.rtsctsManaged
}

func (p *Port) CTSResponseTimeout() time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.ctsTimeout
}

// rerollDesiredNode picks a new desired node address from the fallback list.
// Must be called with p.mu held.
func (p *Port) rerollDesiredNode() {
	p.nodeAttempts = 0
	if len(p.desiredNodeList) == 0 {
		for i := uint8(1); i <= 0xFE; i++ {
			p.desiredNodeList = append(p.desiredNodeList, i)
		}
		rand.Shuffle(len(p.desiredNodeList), func(i, j int) {
			p.desiredNodeList[i], p.desiredNodeList[j] = p.desiredNodeList[j], p.desiredNodeList[i]
		})
	}
	p.desiredNode = p.desiredNodeList[len(p.desiredNodeList)-1]
	p.desiredNodeList = p.desiredNodeList[:len(p.desiredNodeList)-1]
}

func (p *Port) Network() uint16       { return p.network }
func (p *Port) Node() uint8           { return p.node }
func (p *Port) NetworkMin() uint16    { return p.networkMin }
func (p *Port) NetworkMax() uint16    { return p.networkMax }
func (p *Port) ExtendedNetwork() bool { return false }

func (p *Port) Start(router port.RouterHooks) error {
	p.router = router
	// Register seed network in the routing table so datagrams can be routed to this port.
	if p.networkMin != 0 {
		if rs, ok := router.(interface {
			RoutingSetPortRange(pt port.Port, networkMin, networkMax uint16)
		}); ok {
			rs.RoutingSetPortRange(p, p.networkMin, p.networkMax)
		}
	}
	// Register seed zone in the Zone Information Table.
	if p.seedNetwork != 0 && len(p.seedZoneName) > 0 {
		if za, ok := router.(interface {
			AddNetworksToZone(zoneName []byte, networkMin uint16, networkMax *uint16) error
		}); ok {
			nmax := p.networkMax
			_ = za.AddNetworksToZone(p.seedZoneName, p.networkMin, &nmax)
		}
	}
	if p.linkManager != nil {
		p.linkManager.RegisterPort(p)
	} else {
		go p.nodeRun()
	}
	return nil
}

func (p *Port) Stop() error { close(p.stop); return nil }

func (p *Port) nodeRun() {
	t := time.NewTicker(250 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-p.stop:
			return
		case <-t.C:
			p.mu.Lock()
			if p.nodeAttempts >= 8 {
				netlog.Info("%s claiming node address %d", p.ShortString(), p.desiredNode)
				p.node = p.desiredNode
				p.mu.Unlock()
				return
			}
			dst := p.desiredNode
			p.nodeAttempts++
			p.mu.Unlock()
			_ = p.sendFrameFunc([]byte{dst, dst, llapENQ})
		}
	}
}

func (p *Port) InboundFrame(frame []byte) {
	parsed, err := LLAPFrameFromBytes(frame)
	if err != nil {
		return
	}
	netlog.LogLocaltalkFrameInbound(parsed.Bytes(), p)
	if p.linkManager != nil {
		p.linkManager.InboundFrame(p, parsed)
		return
	}
	dst, src, typ := parsed.DestinationNode, parsed.SourceNode, parsed.Type
	switch typ {
	case llapAppleTalkShortHeader:
		d, err := ddp.DatagramFromShortHeaderBytes(dst, src, parsed.Payload)
		if err != nil {
			netlog.Debug("%s failed to parse short-header AppleTalk datagram from LocalTalk frame: %v", p.ShortString(), err)
		} else {
			netlog.LogDatagramInbound(p.Network(), p.Node(), d, p)
			p.router.Inbound(d, p)
		}
	case llapAppleTalkLongHeader:
		d, err := ddp.DatagramFromLongHeaderBytes(parsed.Payload, p.verifyChecksums)
		if err != nil {
			netlog.Debug("%s failed to parse long-header AppleTalk datagram from LocalTalk frame: %v", p.ShortString(), err)
		} else {
			netlog.LogDatagramInbound(p.Network(), p.Node(), d, p)
			p.router.Inbound(d, p)
		}
	case llapENQ:
		if p.respondToEnq && p.node != 0 && p.node == dst {
			_ = p.sendFrameFunc([]byte{p.node, p.node, llapACK})
		} else {
			// Collision avoidance: if another node is probing or has our desired address
			// and we haven't claimed a node yet, pick a different one.
			p.mu.Lock()
			if p.node == 0 && dst == p.desiredNode {
				p.rerollDesiredNode()
			}
			p.mu.Unlock()
		}
	case llapACK:
		// Another node responded to an ENQ for our desired address — collision.
		p.mu.Lock()
		if p.node == 0 && dst == p.desiredNode {
			p.rerollDesiredNode()
		}
		p.mu.Unlock()
	}
}

func (p *Port) Unicast(network uint16, node uint8, d ddp.Datagram) {
	if p.linkManager != nil {
		p.linkManager.TransmitUnicast(p, network, node, d)
		return
	}
	if network != 0 && network != p.network || p.node == 0 {
		netlog.Debug("%s Unicast: dropping (network=%d p.network=%d p.node=%d)", p.ShortString(), network, p.network, p.node)
		return
	}
	netlog.LogDatagramUnicast(network, node, d, p)
	if d.DestinationNetwork == d.SourceNetwork && (d.DestinationNetwork == 0 || d.DestinationNetwork == p.network) {
		b, err := d.AsShortHeaderBytes()
		if err != nil {
			return
		}
		_ = p.sendFrameFunc(append([]byte{node, p.node, llapAppleTalkShortHeader}, b...))
		return
	}
	b, err := d.AsLongHeaderBytes(p.calcChecksums)
	if err != nil {
		return
	}
	_ = p.sendFrameFunc(append([]byte{node, p.node, llapAppleTalkLongHeader}, b...))
}

func (p *Port) Broadcast(d ddp.Datagram) {
	if p.linkManager != nil {
		p.linkManager.TransmitBroadcast(p, d)
		return
	}
	if p.node == 0 {
		netlog.Debug("%s Broadcast: dropping (node not yet claimed)", p.ShortString())
		return
	}
	netlog.LogDatagramBroadcast(d, p)
	b, err := d.AsShortHeaderBytes()
	if err != nil {
		return
	}
	_ = p.sendFrameFunc(append([]byte{0xFF, p.node, llapAppleTalkShortHeader}, b...))
}

func (p *Port) Multicast(zoneName []byte, d ddp.Datagram) {
	netlog.LogDatagramMulticast(zoneName, d, p)
	p.Broadcast(d)
}

func (p *Port) SetNetworkRange(networkMin, networkMax uint16) error {
	if networkMin != networkMax {
		return nil
	}
	if p.network != 0 {
		return nil
	}
	netlog.Info("%s assigned network number %d", p.ShortString(), networkMin)
	p.network = networkMin
	p.networkMin = networkMin
	p.networkMax = networkMax
	// Register in the routing table so the router can forward datagrams to this port
	if rs, ok := p.router.(interface {
		RoutingSetPortRange(pt port.Port, networkMin, networkMax uint16)
	}); ok {
		rs.RoutingSetPortRange(p, networkMin, networkMax)
	}
	return nil
}
