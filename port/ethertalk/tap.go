package ethertalk

import "github.com/pgodw/omnitalk/port/rawlink"

// NewTapPort creates an EtherTalk port over a TAP-style raw link backend.
// TAP support depends on rawlink.OpenTAP for the current platform.
func NewTapPort(interfaceName string, hwAddr []byte, seedNetworkMin, seedNetworkMax, desiredNetwork uint16, desiredNode uint8, seedZoneNames [][]byte) (*PcapPort, error) {
	p, err := NewPcapPort(interfaceName, hwAddr, seedNetworkMin, seedNetworkMax, desiredNetwork, desiredNode, seedZoneNames)
	if err != nil {
		return nil, err
	}
	p.backendLabel = "tap"
	p.openLink = func(name string) (rawlink.RawLink, error) {
		return rawlink.OpenTAP(name)
	}
	p.applyBPFFilter = false
	return p, nil
}

type TapPort = PcapPort
type MacvtapPort = PcapPort
