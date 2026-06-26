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
	"github.com/ObsoleteMadness/ClassicStack/core/service/macip"
	"github.com/ObsoleteMadness/ClassicStack/core/service/nbp"
)

// componentSource is the read-only lookup the provider needs: resolve a built component
// by name. *runtime.Runtime satisfies it (Component); a local interface keeps this
// package from importing compose/runtime just for one method.
type componentSource interface {
	Component(name string) component.Component
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
