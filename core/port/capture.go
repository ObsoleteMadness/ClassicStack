package port

// CaptureFields is the optional wire-dump configuration a transport embeds when it
// can tee frames to a pcap file.
type CaptureFields struct {
	Capture string `toml:"capture,omitempty" display:"Capture file" desc:"Pcap path to tee this port's wire traffic (empty = off)." example:"ethertalk.pcap" capability:"capture"`
	// CaptureSnaplen caps the bytes stored per frame (0 = full frame).
	CaptureSnaplen int `toml:"capture_snaplen,omitempty" display:"Capture snaplen" desc:"Bytes stored per captured frame (0 = full frame)." default:"0" example:"256" capability:"capture"`
}

// CaptureProvider is the capability a section implements when it can tee wire
// traffic to a pcap file.
type CaptureProvider interface {
	CapturePath() string
	CaptureSnapLen() int
}

// CapturePath returns the pcap output path ("" = no capture).
func (c CaptureFields) CapturePath() string { return c.Capture }

// CaptureSnapLen returns the per-frame byte cap (0 = full frame).
func (c CaptureFields) CaptureSnapLen() int { return c.CaptureSnaplen }

var _ CaptureProvider = CaptureFields{}
