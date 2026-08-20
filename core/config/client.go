package config

import (
	"fmt"
	"strings"
	"time"
)

// ClientKey is the well-known section key for the in-process file client.
const ClientKey = "Client"

// Client file-sharing schemes the operator client may probe and connect to.
const (
	ClientServiceAFP      = "afp"
	ClientServiceSMB      = "smb"
	ClientServiceNCP      = "ncp"
	ClientServiceEtherDFS = "etherdfs"
)

// DefaultClientIdleMinutes is how long an unused remote session is kept when
// [Client] max_idle_minutes is unset or zero.
const DefaultClientIdleMinutes = 10

// ClientSection is the in-process file-client config: LAN discovery, remote
// sessions, and optional FUSE/WinFsp host mounts. It is a well-known Model field
// (like HTTP/Logging), not a registered component. Omitted from a config file, it
// defaults to disabled so a server does not open outbound client sockets unless
// the operator opts in.
type ClientSection struct {
	// Enabled turns the in-process file client on. Default false when [Client]
	// is omitted or the key is absent from a present [Client] table.
	Enabled bool `toml:"enabled" display:"Enabled" desc:"Run the in-process file client (LAN scan, remote sessions, optional host mounts). Default false."`
	// Iface is the [[interface]] NAME the outbound client binds (e.g. br-lan).
	// Empty = the model's default interface.
	Iface string `toml:"iface,omitempty" display:"Interface" desc:"[[interface]] name the outbound client binds (e.g. br-lan). Empty = the default interface." example:"br-lan" widget:"iface"`
	// Name is the NetBIOS/SMB name the outbound client presents when it browses and
	// connects to servers — the calling name on session carriers (SMB-over-NBIPX/NBF)
	// and the station name on browse/discovery datagrams. Empty = the server's own
	// Identity.Hostname (§4-bis), so the client and the server share one identity by
	// default, matching how a real Windows/DOS station's redirector and file-sharing
	// server present one NetBIOS name. Set only when the client should present a name
	// distinct from the server.
	Name string `toml:"name,omitempty" display:"Client name" desc:"NetBIOS/SMB name the outbound client presents when browsing/connecting. Empty = the server's own Identity.Hostname." example:"CLASSICSTACK"`
	// MAC pins the outbound client's Ethernet source address on a pcap/tap link,
	// distinct from the server's own NIC-bound ports on the same interface. Empty =
	// the bound [[interface]]'s hw_address, or (failing that) the host NIC's own MAC —
	// the same "be the host" default the server uses.
	MAC string `toml:"mac,omitempty" display:"Station MAC" desc:"Ethernet source address the outbound client presents. Empty = the interface's hw_address, or the host NIC's own MAC." example:"02:00:00:00:00:01" widget:"mac"`
	// Services lists which file-sharing schemes to probe and connect: afp, smb,
	// ncp, etherdfs. Empty = all four when Enabled.
	Services []string `toml:"services,omitempty" display:"Services" desc:"File-sharing schemes the client probes and connects: afp, smb, ncp, etherdfs. Empty = all four."`
	// MaxIdleMinutes is unused-session idle time before disconnect. 0 = 10.
	MaxIdleMinutes int `toml:"max_idle_minutes,omitempty" display:"Max idle (minutes)" desc:"Idle minutes before an unused remote session is disconnected. 0 = 10." example:"10"`
	// Mount allows FUSE (macFUSE/libfuse) or WinFsp host mounts of remote volumes.
	Mount bool `toml:"mount" display:"Enable mounting" desc:"Allow FUSE/WinFsp host mounts of remote volumes the client opens."`
	// LogFile is an optional extra log path for client/Finder traffic. Empty = none
	// (client lines still go to the process logger).
	LogFile string `toml:"log_file,omitempty" display:"Log file" desc:"Optional extra log file for client/Finder traffic. Empty = process logger only." example:"client.log"`
	// Capture is an optional pcap file path for outbound client wire traffic (pcap/tap
	// transports). Empty = no capture. Shares the process-wide capture sink keyed by path.
	Capture string `toml:"capture,omitempty" display:"Capture file" desc:"Optional pcap path for outbound client wire traffic on pcap/tap links." example:"client-afp.pcap" widget:"capture"`
	// CaptureSnaplen truncates each captured frame (0 = 65535).
	CaptureSnaplen int `toml:"capture_snaplen,omitempty" display:"Capture snaplen" desc:"Max bytes per captured client frame (0 = 65535)." example:"65535" capability:"capture"`
}

// Key returns the well-known section key.
func (ClientSection) Key() string { return ClientKey }

// Clone returns a deep copy (Services is the only reference-typed field).
func (s ClientSection) Clone() ClientSection {
	cp := s
	if s.Services != nil {
		cp.Services = append([]string(nil), s.Services...)
	}
	return cp
}

// DefaultClient is the product default: client off, 10-minute idle.
func DefaultClient() ClientSection {
	return ClientSection{MaxIdleMinutes: DefaultClientIdleMinutes}
}

// IdleDuration is the unused-session timeout, applying DefaultClientIdleMinutes
// when MaxIdleMinutes is unset or negative.
func (s ClientSection) IdleDuration() time.Duration {
	n := s.MaxIdleMinutes
	if n <= 0 {
		n = DefaultClientIdleMinutes
	}
	return time.Duration(n) * time.Minute
}

// AllowsService reports whether scheme is among the enabled client services.
// An empty Services list means every known scheme.
func (s ClientSection) AllowsService(scheme string) bool {
	scheme = strings.ToLower(strings.TrimSpace(scheme))
	if scheme == "" {
		return false
	}
	if len(s.Services) == 0 {
		return isClientService(scheme)
	}
	for _, svc := range s.Services {
		if strings.ToLower(strings.TrimSpace(svc)) == scheme {
			return true
		}
	}
	return false
}

// EnabledServices is the scheme list the client should scan, in stable order.
// An empty Services list expands to all known schemes.
func (s ClientSection) EnabledServices() []string {
	if len(s.Services) == 0 {
		return []string{ClientServiceAFP, ClientServiceSMB, ClientServiceNCP, ClientServiceEtherDFS}
	}
	out := make([]string, 0, len(s.Services))
	seen := map[string]bool{}
	for _, svc := range s.Services {
		name := strings.ToLower(strings.TrimSpace(svc))
		if !isClientService(name) || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

// Validate checks service names and idle minutes.
func (s ClientSection) Validate() error {
	if s.MaxIdleMinutes < 0 {
		return fmt.Errorf("client: max_idle_minutes must be >= 0")
	}
	for _, svc := range s.Services {
		name := strings.ToLower(strings.TrimSpace(svc))
		if name == "" {
			continue
		}
		if !isClientService(name) {
			return fmt.Errorf("client: unknown service %q (want afp, smb, ncp, etherdfs)", svc)
		}
	}
	return nil
}

func isClientService(name string) bool {
	switch name {
	case ClientServiceAFP, ClientServiceSMB, ClientServiceNCP, ClientServiceEtherDFS:
		return true
	}
	return false
}
