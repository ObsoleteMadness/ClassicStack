package config

// CaptureSection configures per-interface wire capture: a pcap file path per named
// interface, plus a shared snap length. It is a cross-cutting singleton like Logging
// (not tied to one component), so it is a typed field on Model rather than a registered
// component section. When an interface name has a non-empty path, the cmd edge wraps
// that interface's opened FrameLink with the link.Capture decorator teeing frames to a
// pure-Go pcapfile.Sink (logging-and-wire-capture-design: capture is pcap-only).
//
// Paths is keyed by INTERFACE name (the same namespace ports reference — "eth0",
// "br-lan", …), not by transport, so one capture file covers whatever rides that
// interface. A nil/empty map captures nothing — the zero value is "no capture".
type CaptureSection struct {
	// Paths maps an interface name to its pcap output file. Empty path / absent key =
	// no capture for that interface.
	Paths map[string]string `toml:"paths"`
	// Snaplen is the per-frame capture cap in bytes (0 → the writer's default, a full
	// frame). Shared across all capture files.
	Snaplen int `toml:"snaplen"`
}

// Clone returns a deep copy (Paths is the only reference field).
func (s CaptureSection) Clone() CaptureSection {
	cp := s
	if s.Paths != nil {
		cp.Paths = make(map[string]string, len(s.Paths))
		for k, v := range s.Paths {
			cp.Paths[k] = v
		}
	}
	return cp
}

// PathFor returns the configured capture file for an interface name, or "" when none
// is set (so the caller skips wrapping that interface's link).
func (s CaptureSection) PathFor(iface string) string {
	if s.Paths == nil {
		return ""
	}
	return s.Paths[iface]
}

// Any reports whether any interface has a capture path configured.
func (s CaptureSection) Any() bool {
	for _, p := range s.Paths {
		if p != "" {
			return true
		}
	}
	return false
}
