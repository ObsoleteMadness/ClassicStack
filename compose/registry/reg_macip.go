//go:build macip || all

package registry

import (
	"context"

	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/service/macip"
)

// macipPlaceholder is an inert stand-in the registry returns for the MacIP
// service. The real macip.Service (M5) needs its AppleTalk router, NBP service,
// and IP-side egress injected — dependencies the registry factory does not have
// in hand. Wiring the real service into the supervisor is the compose cutover
// (M8/M10); until then the registry advertises the name with this no-op so the
// stack still boots. Mirrors how M4 left the router's DDP services unattached.
type macipPlaceholder struct{ running bool }

func (p *macipPlaceholder) Name() string                { return macip.Name }
func (p *macipPlaceholder) Start(context.Context) error { p.running = true; return nil }
func (p *macipPlaceholder) Stop(context.Context) error  { p.running = false; return nil }

func init() {
	Register(macip.Name, func(m *config.Model) (component.Component, error) {
		return &macipPlaceholder{}, nil
	})
}

var _ component.Component = (*macipPlaceholder)(nil)
