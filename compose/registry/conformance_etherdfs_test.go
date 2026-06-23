//go:build etherdfs || all

package registry

import (
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/service/etherdfs"
)

// init registers the EtherDFS conformance stager: EtherDFS is configured via its
// own singleton [EtherDFS] server section (it is BOTH the wire endpoint and the
// file server), not a plain port.Section, so the conformance harness must stage an
// enabled ServerSection or the factory builds the disabled (nil) form.
func init() {
	conformanceStagers[etherdfs.Name] = func(m *config.Model) {
		m.Set(&etherdfs.ServerSection{SKey: etherdfs.ServerKey, Interface: "eth0", IsEnabled: true})
	}
}
