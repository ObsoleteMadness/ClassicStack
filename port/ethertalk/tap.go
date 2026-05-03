package ethertalk

import "github.com/ObsoleteMadness/ClassicStack/port/rawlink"

// NewTapPort creates an EtherTalk port over a TAP-style raw link backend.
// TAP support depends on rawlink.OpenTAP for the current platform.
func NewTapPort(opts Options) (*PcapPort, error) {
	p, err := NewPcapPort(opts)
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
