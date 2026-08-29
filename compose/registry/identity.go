package registry

import (
	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
)

// IdentityStamper restamps shared Identity onto one live component. Tagged
// registry files register one stamper per service so an unlinked service is
// simply absent — the supervisor stays free of service types.
type IdentityStamper func(c component.Component, m *config.Model) bool

var identityStampers []IdentityStamper

func registerIdentityStamper(fn IdentityStamper) {
	identityStampers = append(identityStampers, fn)
}

// StampIdentity restamps Identity (and AFP's router default zone) onto c when
// a stamper recognises the component. Unknown types are ignored.
func StampIdentity(c component.Component, m *config.Model) {
	if c == nil || m == nil {
		return
	}
	for _, fn := range identityStampers {
		if fn(c, m) {
			return
		}
	}
}
