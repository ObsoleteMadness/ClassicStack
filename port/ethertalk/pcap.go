package ethertalk

import (
	"log"
	"net"

	"github.com/pgodw/omnitalk/port"
	"github.com/pgodw/omnitalk/port/rawlink"
)

// etherTalkBPFFilter selects EtherTalk Phase 2 frames carried as
// 802.3 length + LLC/SNAP payloads:
// - AppleTalk DDP: DSAP/SSAP/CTL=AA AA 03, OUI+PID=08 00 07 80 9B
// - AARP:          DSAP/SSAP/CTL=AA AA 03, OUI+PID=00 00 00 80 F3
//
// The prior Ethernet II filter ("ether proto 0x809b or ether proto 0x80f3")
// does not match this framing and can drop discovery/routing traffic.
const etherTalkBPFFilter = "(ether[12:2] <= 1500) and (ether[14:2] = 0xaaaa) and (ether[16] = 0x03) and ((ether[17:4] = 0x08000780 and ether[21] = 0x9b) or (ether[17:4] = 0x00000080 and ether[21] = 0xf3))"

type PcapPort struct {
	*Port
	interfaceName  string
	backendLabel   string
	openLink       func(interfaceName string) (rawlink.RawLink, error)
	applyBPFFilter bool
	link           rawlink.RawLink
	medium         rawlink.PhysicalMedium
	hostMAC        []byte
	bridgeMode     bridgeMode
	adapter        bridgeFrameAdapter
	readerStop     chan struct{}
	readerDone     chan struct{}
	writerQueue    chan []byte
	writerStop     chan struct{}
	writerDone     chan struct{}
}

func NewPcapPort(interfaceName string, hwAddr []byte, seedNetworkMin, seedNetworkMax, desiredNetwork uint16, desiredNode uint8, seedZoneNames [][]byte) (*PcapPort, error) {
	if len(hwAddr) != 6 {
		return nil, net.InvalidAddrError("hw_addr must be exactly 6 bytes")
	}
	base := New(hwAddr, seedNetworkMin, seedNetworkMax, desiredNetwork, desiredNode, seedZoneNames)
	p := &PcapPort{
		Port:          base,
		interfaceName: interfaceName,
		backendLabel:  "pcap",
		openLink: func(name string) (rawlink.RawLink, error) {
			return rawlink.OpenPcap(rawlink.DefaultEtherTalkConfig(name))
		},
		applyBPFFilter: true,
		medium:         rawlink.MediumEthernet,
		hostMAC:        append([]byte(nil), hwAddr...),
		bridgeMode:     bridgeModeAuto,
		adapter:        newEthertalkBridgeAdapterWithWiFiEncap(hwAddr, hwAddr, bridgeModeEthernet, false),
		readerStop:     make(chan struct{}),
		readerDone:     make(chan struct{}),
		writerQueue:    make(chan []byte, 1024),
		writerStop:     make(chan struct{}),
		writerDone:     make(chan struct{}),
	}
	p.ConfigureTx(func(frame []byte) error {
		p.sendFrame(frame)
		return nil
	})
	return p, nil
}

func (p *PcapPort) ShortString() string { return p.interfaceName }

func (p *PcapPort) SetBridgeMode(mode bridgeMode) {
	p.setResolvedBridgeMode(mode)
}

func (p *PcapPort) SetBridgeModeString(mode string) error {
	parsed, err := parseBridgeModeString(mode)
	if err != nil {
		return err
	}
	p.SetBridgeMode(parsed)
	return nil
}

func (p *PcapPort) SetFrameAdapter(adapter bridgeFrameAdapter) {
	if adapter == nil {
		adapter = newEthertalkBridgeAdapterWithWiFiEncap(p.hostMAC, p.hwAddr, p.bridgeMode, bridgeModeRequiresWiFiEncapsulation(p.medium))
	}
	p.adapter = adapter
}

func (p *PcapPort) SetBridgeHostMAC(hostMAC []byte) error {
	if len(hostMAC) != 6 {
		return net.InvalidAddrError("bridge host mac must be exactly 6 bytes")
	}
	p.hostMAC = append([]byte(nil), hostMAC...)
	p.adapter = newEthertalkBridgeAdapterWithWiFiEncap(p.hostMAC, p.hwAddr, p.bridgeMode, bridgeModeRequiresWiFiEncapsulation(p.medium))
	return nil
}

func (p *PcapPort) setResolvedBridgeMode(mode bridgeMode) {
	if mode == bridgeModeAuto {
		mode = bridgeModeEthernet
	}
	p.bridgeMode = mode
	p.adapter = newEthertalkBridgeAdapterWithWiFiEncap(p.hostMAC, p.hwAddr, mode, bridgeModeRequiresWiFiEncapsulation(p.medium))
}

func (p *PcapPort) Start(r port.RouterHooks) error {
	link, err := p.openLink(p.interfaceName)
	if err != nil {
		return err
	}
	p.link = link

	// Detect physical medium and resolve bridge mode.
	if mr, ok := link.(rawlink.MediumReporter); ok {
		p.medium = mr.Medium()
	}
	mode := p.bridgeMode
	if mode == bridgeModeAuto {
		mode = detectEthertalkBridgeModeFromMedium(p.medium)
	}
	p.setResolvedBridgeMode(mode)
	if p.bridgeMode == bridgeModeWiFi && !bridgeModeRequiresWiFiEncapsulation(p.medium) {
		log.Printf("pcap wifi bridge on %s using Ethernet TX framing (medium: ethernet)", p.interfaceName)
	}
	log.Printf("%s bridge mode on %s: %s (medium: %v)", p.backendLabel, p.interfaceName, p.bridgeMode.String(), p.medium)

	// Apply BPF filter when the backend supports it.
	if p.applyBPFFilter {
		if fl, ok := link.(rawlink.FilterableLink); ok {
			if err := fl.SetFilter(etherTalkBPFFilter); err != nil {
				log.Printf("warning: could not set BPF filter on %s: %v", p.interfaceName, err)
			}
		}
	}

	if err := p.Port.Start(r); err != nil {
		return err
	}
	go p.readRun()
	go p.writeRun()
	return nil
}

func (p *PcapPort) Stop() error {
	close(p.readerStop)
	close(p.writerStop)
	<-p.readerDone
	<-p.writerDone
	if p.link != nil {
		p.link.Close()
	}
	return p.Port.Stop()
}

func (p *PcapPort) readRun() {
	defer close(p.readerDone)
	for {
		select {
		case <-p.readerStop:
			return
		default:
			data, err := p.link.ReadFrame()
			if err != nil {
				if err != rawlink.ErrTimeout {
					log.Printf("pcap read error on %s: %v", p.interfaceName, err)
				}
				continue
			}
			normalized, err := p.adapter.inboundFrame(data)
			if err != nil {
				log.Printf("warning: failed to normalize inbound frame on %s: %v", p.interfaceName, err)
				continue
			}
			p.InboundFrame(normalized)
		}
	}
}

func (p *PcapPort) sendFrame(frameData []byte) {
	select {
	case p.writerQueue <- frameData:
	default:
		log.Printf("warning: pcap writer queue full, dropping outbound packet")
	}
}

func (p *PcapPort) writeRun() {
	defer close(p.writerDone)
	for {
		select {
		case <-p.writerStop:
			return
		case frameData := <-p.writerQueue:
			prepared, err := p.adapter.outboundFrame(frameData)
			if err != nil {
				log.Printf("warning: failed to prepare outbound frame on %s: %v", p.interfaceName, err)
				continue
			}
			if err := p.link.WriteFrame(prepared); err != nil {
				log.Printf("warning: couldn't send packet: %v", err)
			}
		}
	}
}
