package config

import "runtime"

// Defaults returns a Model seeded with ClassicStack's built-in defaults.
// These mirror the flag/DefaultConfig defaults in cmd/classicstack so a
// Model built from an empty source matches a default flag-driven run.
func Defaults() *Model {
	return &Model{
		Logging: LoggingModel{Level: "info"},
		Bridge:  BridgeModel{Mode: "pcap", BridgeMode: "auto", HWAddress: "DE:AD:BE:EF:CA:FE"},
		LToUDP: LToUDPModel{
			Enabled:     true,
			Interface:   "0.0.0.0",
			SeedNetwork: 1,
			SeedZone:    "LToUDP Network",
		},
		TashTalk: TashTalkModel{
			SeedNetwork: 2,
			SeedZone:    "TashTalk Network",
		},
		EtherTalk: EtherTalkModel{
			SeedNetworkMin: 3,
			SeedNetworkMax: 5,
			SeedZone:       "EtherTalk Network",
			DesiredNetwork: 3,
			DesiredNode:    253,
		},
		Capture: CaptureModel{Snaplen: 65535},
		MacIP:   MacIPModel{NATSubnet: "192.168.100.0/24"},
		IPX:     IPXModel{Framing: "ethernet_ii"},
		NetBIOS: NetBIOSModel{Transports: []string{"tcp"}},
		SMB: SMBModel{
			NBTBinding: ":139",
			ServerName: "CLASSICSTACK",
			Workgroup:  "WORKGROUP",
		},
		AFP: AFPModel{
			Enabled:            true,
			Name:               "Go File Server",
			Protocols:          "tcp,ddp",
			Binding:            ":548",
			CNIDBackend:        "sqlite",
			UseDecomposedNames: true,
			AppleDoubleMode:    "modern",
		},
		Shortname: ShortnameModel{
			Backend:           "memory",
			WindowsShortnames: runtime.GOOS == "windows",
		},
		WebUI: WebUIModel{
			Bind: "127.0.0.1:8080",
			TLS:  true,
		},
	}
}
