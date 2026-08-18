package config

import (
	"fmt"
	"net"
	"strings"
)

// HTTPKey is the well-known section key for the web-admin listen config.
const HTTPKey = "HTTP"

// DefaultHTTPAddr is the web-admin listen address when [http] addr is blank.
const DefaultHTTPAddr = ":1984"

// HTTPSection is the web-admin control UI listen config. It is a well-known
// Model field (like Logging/Router), not a registered component. Omitted from a
// config file, it defaults to enabled on DefaultHTTPAddr so a desktop/laptop
// server serves the UI without a flag.
type HTTPSection struct {
	// Enabled serves the web-admin UI. Default true when [http] is omitted or
	// the key is absent from a present [http] table.
	Enabled bool `toml:"enabled" display:"Enabled" desc:"Serve the web-admin control UI. Default true (listen :1984) when [http] is omitted."`
	// Addr is the TCP listen address (host:port). Empty = :1984.
	Addr string `toml:"addr,omitempty" display:"Listen address" desc:"TCP address for the web-admin UI (host:port). Empty = :1984." example:":1984"`
}

// Key returns the well-known section key.
func (HTTPSection) Key() string { return HTTPKey }

// Clone returns a copy.
func (s HTTPSection) Clone() HTTPSection { return s }

// DefaultHTTP is the product default: UI on, listen :1984.
func DefaultHTTP() HTTPSection {
	return HTTPSection{Enabled: true, Addr: DefaultHTTPAddr}
}

// ListenAddr is the address the HTTP adapter should bind, applying DefaultHTTPAddr
// when Addr is blank.
func (s HTTPSection) ListenAddr() string {
	if a := strings.TrimSpace(s.Addr); a != "" {
		return a
	}
	return DefaultHTTPAddr
}

// Validate checks the listen address is a host:port pair (empty Addr uses :1984).
func (s HTTPSection) Validate() error {
	if _, _, err := net.SplitHostPort(s.ListenAddr()); err != nil {
		return fmt.Errorf("http: invalid listen address %q", s.Addr)
	}
	return nil
}

// ApplyHTTPDefaults fills in enabled-on-:1984 when the [http] section was omitted,
// or when a present section left enabled/addr unset.
func ApplyHTTPDefaults(s HTTPSection, sectionPresent, enabledPresent bool) HTTPSection {
	if !sectionPresent {
		return DefaultHTTP()
	}
	if !enabledPresent {
		s.Enabled = true
	}
	if strings.TrimSpace(s.Addr) == "" {
		s.Addr = DefaultHTTPAddr
	}
	return s
}
