package port

// IPXNetworkFields is the IPX network-number configuration an IPX port or
// MacIPX (IPXGW) gateway embeds. The same TOML key (ipx_network) is used on both.
type IPXNetworkFields struct {
	// Keep the tag under 255 bytes: TinyGo rejects longer struct tags (tinygo-gate).
	IPXNetwork uint32 `toml:"ipx_network,omitempty" display:"IPX network" desc:"IPX network number (decimal). IPX port: this segment (0 = local/unknown). MacIPX: announced to clients (0 = 0x10). Match them on a shared segment." default:"0" example:"16" capability:"ipx_network"`
}

// IPXNetworkProvider is the capability a section implements when it carries an
// IPX network number.
type IPXNetworkProvider interface {
	ConfiguredIPXNetwork() uint32
}

// ConfiguredIPXNetwork returns the configured network number (0 = consumer default).
func (f IPXNetworkFields) ConfiguredIPXNetwork() uint32 { return f.IPXNetwork }

// IPXNetworkBytes returns the network number as the 4-byte big-endian wire form.
func IPXNetworkBytes(n uint32) [4]byte {
	return [4]byte{byte(n >> 24), byte(n >> 16), byte(n >> 8), byte(n)}
}

var _ IPXNetworkProvider = IPXNetworkFields{}
