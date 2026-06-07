package control

import "context"

// ZoneInfo is one AppleTalk zone reported by ListZones.
type ZoneInfo struct {
	Name string `json:"name"`
}

// NetworkInfo is one routing-table network reported by DDPEnumerate.
type NetworkInfo struct {
	NetworkMin uint16 `json:"network_min"`
	NetworkMax uint16 `json:"network_max"`
	Distance   uint8  `json:"distance"`
	Port       string `json:"port"`
}

// RTMPEntry is one routing-table entry reported by RTMPTable. State is the
// RTMP aging state ("good" | "suspect" | "bad" | "worst") — RTMP's notion of an
// entry's age, advanced on each aging tick and reset when the route is heard
// again. Distance 0 means a directly-connected network reached via Port; for
// learned networks NextNetwork/NextNode is the next-hop router.
type RTMPEntry struct {
	NetworkMin  uint16 `json:"network_min"`
	NetworkMax  uint16 `json:"network_max"`
	Distance    uint8  `json:"distance"`
	Port        string `json:"port"`
	NextNetwork uint16 `json:"next_network"`
	NextNode    uint8  `json:"next_node"`
	State       string `json:"state"`
}

// EchoResult is the outcome of an AEP (AppleTalk Echo Protocol) probe.
type EchoResult struct {
	Network uint16 `json:"network"`
	Node    uint8  `json:"node"`
	OK      bool   `json:"ok"`
	RTTMS   int64  `json:"rtt_ms"`
	Err     string `json:"err,omitempty"`
}

// ServerInfo is one host reported by SMBBrowse.
type ServerInfo struct {
	Name    string `json:"name"`
	Comment string `json:"comment,omitempty"`
}

// LeaseInfo is one MacIP IP lease reported by MacIPLeases. Source is
// "static" (pool-assigned) or "dhcp" (relayed from the network's DHCP server).
type LeaseInfo struct {
	IP           string `json:"ip"`
	ATNetwork    uint16 `json:"at_network"`
	ATNode       uint8  `json:"at_node"`
	Source       string `json:"source"`
	LastSeenUnix int64  `json:"last_seen_unix"`
}

// MacIPState is a point-in-time summary of the MacIP gateway for the
// dashboard: its mode, options, and live counts.
type MacIPState struct {
	Mode         string `json:"mode"` // "nat" or "bridge"
	DHCPRelay    bool   `json:"dhcp_relay"`
	Zone         string `json:"zone,omitempty"`
	ActiveLeases int    `json:"active_leases"`
	Sessions     int    `json:"sessions"`
}

// Diagnostics is the set of read-only network probes the UI exposes. The
// concrete implementation is provided by the supervisor at wire time
// (some probes — e.g. SMB browse — are only available when that subsystem
// is built in); an unset probe returns ErrDiagUnavailable.
type Diagnostics interface {
	// ListZones returns the AppleTalk zones known to the router/ZIP.
	ListZones(ctx context.Context) ([]ZoneInfo, error)
	// AEPEcho sends an Echo request to net/node and reports the round trip.
	AEPEcho(ctx context.Context, network uint16, node uint8) (EchoResult, error)
	// ZIPEnumerate walks zones via ZIP GetZoneList.
	ZIPEnumerate(ctx context.Context) ([]ZoneInfo, error)
	// DDPEnumerate lists networks/nodes from the routing table.
	DDPEnumerate(ctx context.Context) ([]NetworkInfo, error)
	// RTMPTable returns the full RTMP routing table including each entry's
	// aging state.
	RTMPTable(ctx context.Context) ([]RTMPEntry, error)
	// SMBBrowse returns the SMB/NetBIOS browse list of servers. Only
	// available in SMB-enabled builds.
	SMBBrowse(ctx context.Context) ([]ServerInfo, error)
	// MacIPLeases returns the MacIP gateway's current IP leases. Only
	// available when MacIP is built in and enabled.
	MacIPLeases(ctx context.Context) ([]LeaseInfo, error)
}

// SetDiagnostics installs the diagnostics implementation.
func (p *Plane) SetDiagnostics(d Diagnostics) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.diag = d
}

// Diagnostics returns the installed diagnostics implementation, or a
// no-op that reports every probe as unavailable when none is set.
func (p *Plane) Diagnostics() Diagnostics {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.diag == nil {
		return unavailableDiagnostics{}
	}
	return p.diag
}
