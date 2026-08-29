package port

import "github.com/ObsoleteMadness/ClassicStack/core/config"

// Base is the shared identity/binding every transport section carries: schema key,
// per-instance name, interface override, enabled flag, and optional station MAC.
// Transport-specific sections embed Base and add only the fields that apply to them.
type Base struct {
	// SKey is the section/SCHEMA key shared by every instance of a transport
	// ("EtherTalk", "LToUDP", "IPX", …). It is the registry/codec key, NOT the
	// per-instance identity — see Name.
	SKey string `toml:"-"`
	// Name is the per-INSTANCE identity (§M11).
	Name string `toml:"name,omitempty" display:"Instance name" desc:"Unique name for this instance (referenced by the router's Members list). Empty = the lone default, named after the transport." example:"et-lab" widget:""`
	// Iface is the NAME of the interface this instance binds to.
	Iface string `toml:"iface,omitempty" display:"Interface" desc:"Named interface this transport binds to. Empty inherits the default interface." example:"br-lan" widget:"iface"`
	// IsEnabled mirrors the configured-enabled flag (≠ running). Never omitempty.
	IsEnabled bool `toml:"enabled" display:"Enabled" desc:"Whether this instance is configured on (≠ currently running)." default:"true"`
	// MAC is the station hardware address used as the Ethernet source.
	MAC string `toml:"mac,omitempty" display:"Station MAC" desc:"Ethernet source address. Empty = use the interface's own MAC." example:"DE:AD:BE:EF:CA:FE"`
}

// Key returns the shared SCHEMA key (the registry/codec key).
func (b Base) Key() string { return b.SKey }

// InstanceName returns the per-instance identity (config.NamedSection).
func (b Base) InstanceName() string {
	if b.Name != "" {
		return b.Name
	}
	return b.SKey
}

// Interface makes a port Base a config.InterfaceProvider.
func (b Base) Interface() config.InterfaceSection {
	return config.InterfaceSection{Name: b.Iface}
}

// validateMAC rejects a malformed station address when one is set.
func validateMAC(mac string) error {
	if mac == "" {
		return nil
	}
	if _, err := ParseMAC(mac); err != nil {
		return err
	}
	return nil
}
