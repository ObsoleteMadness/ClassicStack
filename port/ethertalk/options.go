package ethertalk

// Options bundles immutable construction inputs for an EtherTalk PcapPort
// (or its Tap variant). Keeping bridge configuration here means callers set
// it up-front rather than mutating the port after Start.
type Options struct {
	InterfaceName  string
	HWAddr         []byte
	SeedNetworkMin uint16
	SeedNetworkMax uint16
	DesiredNetwork uint16
	DesiredNode    uint8
	SeedZoneNames  [][]byte

	// BridgeMode is the textual bridge mode ("", "auto", "ethernet", "wifi").
	// Empty is treated as "auto".
	BridgeMode string
	// BridgeHostMAC is the host adapter's MAC for the Wi-Fi bridge shim.
	// When nil, falls back to HWAddr.
	BridgeHostMAC []byte
}
