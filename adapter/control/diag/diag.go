// Package diag is the read-only diagnostics ADAPTER: it bridges the protocol services'
// own diagnostic state (the NBP registered-name table, the MacIP lease table, …) to the
// management front-ends, WITHOUT any protocol type crossing the neutral core/control
// contract. It is the read-only sibling of compose/runtime/transports.go — the
// composition layer whose job is to know both the services and the UI, so it is allowed
// to import the service packages (which core/control must not).
//
// It resolves the live service instances from the runtime's component set (by name,
// type-asserting to the concrete service) and decodes each service's typed getter
// (nbp.Service.Names, macip.Service.Leases) into a DTO owned HERE. A service that was not
// built is simply absent from the component set, so its probe reports ErrUnavailable —
// the same graceful-degradation the front-ends already handle. The byte→string /
// IPv4→string decode lives here (or in the service), never in core/control.
//
// Ring: ADAPTER. The web/ubus servers take a *Provider and call it for the protocol
// drill-downs; the neutral control.Plane keeps only ListZones.
package diag

import (
	"sort"
	"strconv"

	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/control"
	"github.com/ObsoleteMadness/ClassicStack/core/port/ethertalk"
	"github.com/ObsoleteMadness/ClassicStack/core/service/macip"
	"github.com/ObsoleteMadness/ClassicStack/core/service/nbp"
	"github.com/ObsoleteMadness/ClassicStack/core/service/smb"
)

// componentSource is the read-only lookup the provider needs: resolve a built component
// by name and enumerate the built names. *runtime.Runtime satisfies it (Component +
// Built); a local interface keeps this package from importing compose/runtime just for
// those two methods.
type componentSource interface {
	Component(name string) component.Component
	Built() []string
}

// Provider answers the protocol-specific diagnostic drill-downs by resolving the live
// services from the component set. Construct it at the cmd edge over the runtime and
// hand it to the web/ubus servers.
type Provider struct {
	src componentSource
}

// New builds a Provider over the runtime (or any component source). A nil source makes
// every probe report ErrUnavailable.
func New(src componentSource) *Provider { return &Provider{src: src} }

// NBPName is the management view of one NBP registered name: the NVE tuple decoded to
// display strings + the DDP socket. Owned by this adapter (not core/control), so the
// neutral plane carries no NBP type.
type NBPName struct {
	Object string `json:"object"`
	Type   string `json:"type"`
	Zone   string `json:"zone"`
	Socket uint8  `json:"socket"`
}

// MacIPLease is the management view of one MacIP lease: the assigned IPv4 (dotted-quad
// string), the AppleTalk net/node, and the lease source. Owned by this adapter.
type MacIPLease struct {
	IP        string `json:"ip"`
	ATNetwork uint16 `json:"at_network"`
	ATNode    uint8  `json:"at_node"`
	Source    string `json:"source"`
}

// AARPEntry is the management view of one AARP Address Mapping Table entry: the EtherTalk
// port instance it belongs to, the resolved AppleTalk address (network.node), the MAC it
// maps to (colon-hex), and the UnixNano of the last confirm/glean. Owned by this adapter.
type AARPEntry struct {
	Port    string `json:"port"`    // the EtherTalk port instance name
	Network uint16 `json:"network"` // AppleTalk network of the mapped address
	Node    uint8  `json:"node"`    // AppleTalk node of the mapped address
	MAC     string `json:"mac"`     // resolved hardware address (aa:bb:cc:dd:ee:ff)
	SeenNs  int64  `json:"seen_ns"` // UnixNano of the last confirm/glean
}

// SMBSession is the management view of one live SMB circuit: the transport client
// label plus the fields the web UI displays for it (MAC, calling NetBIOS name,
// authenticated user, negotiated dialect, and the client's self-reported OS/LAN
// Manager identity from SESSION_SETUP_ANDX). Owned by this adapter (not
// core/control), so the neutral plane carries no SMB type.
type SMBSession struct {
	Client        string `json:"client"`
	MAC           string `json:"mac"`
	NetBIOSName   string `json:"netbios_name"`
	User          string `json:"user"`
	Dialect       string `json:"dialect"`
	NegotiatedAt  int64  `json:"negotiated_at"` // UnixNano; 0 before NEGOTIATE
	NativeOS      string `json:"native_os"`
	NativeLanMan  string `json:"native_lanman"`
	PrimaryDomain string `json:"primary_domain"`
	OpenTrees     int    `json:"open_trees"`
	OpenFiles     int    `json:"open_files"`
}

// SMBSessions returns the live SMB circuit table, sorted by client label.
// control.ErrUnavailable when no SMB service was built.
func (p *Provider) SMBSessions() ([]SMBSession, error) {
	svc := p.smb()
	if svc == nil {
		return nil, control.ErrUnavailable
	}
	raw := svc.Sessions()
	out := make([]SMBSession, 0, len(raw))
	for _, s := range raw {
		var negotiatedAt int64
		if !s.NegotiatedAt.IsZero() {
			negotiatedAt = s.NegotiatedAt.UnixNano()
		}
		out = append(out, SMBSession{
			Client:        s.Client,
			MAC:           s.MAC,
			NetBIOSName:   s.NetBIOSName,
			User:          s.User,
			Dialect:       s.Dialect,
			NegotiatedAt:  negotiatedAt,
			NativeOS:      s.NativeOS,
			NativeLanMan:  s.NativeLanMan,
			PrimaryDomain: s.PrimaryDomain,
			OpenTrees:     s.OpenTrees,
			OpenFiles:     s.OpenFiles,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Client < out[j].Client })
	return out, nil
}

// smb resolves the live SMB service, or nil when none was built.
func (p *Provider) smb() *smb.Service {
	if p.src == nil {
		return nil
	}
	if c := p.src.Component(smb.Name); c != nil {
		if s, ok := c.(*smb.Service); ok {
			return s
		}
	}
	return nil
}

// RegisteredNames returns the NBP name table, decoding the NVE byte fields to display
// strings, sorted by object then type. control.ErrUnavailable when no NBP service was
// built.
func (p *Provider) RegisteredNames() ([]NBPName, error) {
	svc := p.nbp()
	if svc == nil {
		return nil, control.ErrUnavailable
	}
	raw := svc.Names()
	out := make([]NBPName, 0, len(raw))
	for _, n := range raw {
		out = append(out, NBPName{
			Object: string(n.Object),
			Type:   string(n.Type),
			Zone:   string(n.Zone),
			Socket: n.Socket,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Object != out[j].Object {
			return out[i].Object < out[j].Object
		}
		return out[i].Type < out[j].Type
	})
	return out, nil
}

// MacIPLeases returns the MacIP gateway's active leases, decoding the IPv4 to a
// dotted-quad string, sorted by IP. control.ErrUnavailable when no MacIP gateway was
// built.
func (p *Provider) MacIPLeases() ([]MacIPLease, error) {
	svc := p.macip()
	if svc == nil {
		return nil, control.ErrUnavailable
	}
	raw := svc.Leases()
	out := make([]MacIPLease, 0, len(raw))
	for _, l := range raw {
		out = append(out, MacIPLease{
			IP:        ipv4String(l.IP),
			ATNetwork: l.ATNetwork,
			ATNode:    l.ATNode,
			Source:    l.Source,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].IP < out[j].IP })
	return out, nil
}

// AARPTable returns the AARP Address Mapping Table across every built EtherTalk port
// instance (each instance is a distinct EtherTalk segment with its own AMT, §M11),
// decoding each mapping's MAC to colon-hex and tagging it with the owning port. Entries
// are sorted by port, then network, then node. control.ErrUnavailable when no EtherTalk
// port was built; an empty (non-nil) slice when the ports exist but have resolved nothing
// yet (no station MAC → plain broadcast framer → no AMT, also empty).
func (p *Provider) AARPTable() ([]AARPEntry, error) {
	ports := p.etherTalkPorts()
	if len(ports) == 0 {
		return nil, control.ErrUnavailable
	}
	out := []AARPEntry{}
	for name, port := range ports {
		for _, e := range port.AARPTable() {
			out = append(out, AARPEntry{
				Port:    name,
				Network: e.Addr.Network,
				Node:    e.Addr.Node,
				MAC:     macString(e.HW),
				SeenNs:  e.Seen,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Port != out[j].Port {
			return out[i].Port < out[j].Port
		}
		if out[i].Network != out[j].Network {
			return out[i].Network < out[j].Network
		}
		return out[i].Node < out[j].Node
	})
	return out, nil
}

// etherTalkPorts resolves every built EtherTalk port instance, keyed by its component
// (instance) name. Multiple named instances are possible (§M11), each its own segment;
// the singleton case is one entry under ethertalk.Name. Empty when none was built.
func (p *Provider) etherTalkPorts() map[string]*ethertalk.Port {
	if p.src == nil {
		return nil
	}
	out := map[string]*ethertalk.Port{}
	for _, name := range p.src.Built() {
		if et, ok := p.src.Component(name).(*ethertalk.Port); ok {
			out[name] = et
		}
	}
	return out
}

// nbp resolves the live NBP service, or nil when none was built.
func (p *Provider) nbp() *nbp.Service {
	if p.src == nil {
		return nil
	}
	if c := p.src.Component(nbp.Name); c != nil {
		if s, ok := c.(*nbp.Service); ok {
			return s
		}
	}
	return nil
}

// macip resolves the live MacIP gateway, or nil when none was built.
func (p *Provider) macip() *macip.Service {
	if p.src == nil {
		return nil
	}
	if c := p.src.Component(macip.Name); c != nil {
		if s, ok := c.(*macip.Service); ok {
			return s
		}
	}
	return nil
}

// ipv4String renders a macip.IPv4 ([4]byte) as a dotted-quad string.
func ipv4String(ip macip.IPv4) string {
	return strconv.Itoa(int(ip[0])) + "." + strconv.Itoa(int(ip[1])) + "." +
		strconv.Itoa(int(ip[2])) + "." + strconv.Itoa(int(ip[3]))
}

// macString renders a 6-byte hardware address as lower-case colon-hex (aa:bb:cc:dd:ee:ff).
func macString(hw [6]byte) string {
	const hexDigits = "0123456789abcdef"
	b := make([]byte, 0, 17)
	for i, v := range hw {
		if i > 0 {
			b = append(b, ':')
		}
		b = append(b, hexDigits[v>>4], hexDigits[v&0x0f])
	}
	return string(b)
}
