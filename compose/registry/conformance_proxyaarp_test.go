//go:build ethertalk || all

package registry

import (
	"github.com/ObsoleteMadness/ClassicStack/adapter/bridge"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
)

// init registers the ProxyAARP conformance stager: the proxy-AARP Wi-Fi/tunnel bridge is
// configured via its own singleton [ProxyAARP] section (tunnel/egress interfaces), not a
// plain port.Section, so the conformance harness must stage an enabled Section or the
// factory builds the disabled (nil) form. With no ctx.Opener it comes up inert, which is
// exactly the lifecycle contract the harness exercises.
func init() {
	conformanceStagers[bridge.Name] = func(m *config.Model) {
		m.Set(&bridge.Section{
			SKey:            bridge.SectionKey,
			Enabled:         true,
			TunnelInterface: "eth0",
			EgressInterface: "wlan0",
		})
	}
}
