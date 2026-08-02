package port

// IPXFrameFields is the Novell Ethernet encapsulation an IPX port embeds.
type IPXFrameFields struct {
	IPXFrameType  string   `toml:"ipx_frame_type,omitempty" display:"Frame type" desc:"Outbound Ethernet encapsulation: ethernet_ii (default, MacIPX) · 802.3 · 802.2. Inbound accepts all." example:"ethernet_ii" default:"ethernet_ii" widget:"frame_type" capability:"ipx_framing"`
	IPXFrameTypes []string `toml:"ipx_frame_types,omitempty" display:"Frame types (multi)" desc:"Optional list of encapsulations to advertise on (SAP/RIP once each). Empty = just Frame type." capability:"ipx_framing"`
}

func cloneIPXFrameTypes(in []string) []string {
	if in == nil {
		return nil
	}
	return append([]string(nil), in...)
}
