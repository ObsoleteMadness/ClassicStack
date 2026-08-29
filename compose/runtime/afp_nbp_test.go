//go:build (afp && router) || all

// This test drives the real registry-built stack, so it only compiles when the
// components it asserts on are actually registered: AFP registers under the `afp`
// tag (reg_afp.go) and NBP under the `router` tag (reg_nbp.go). Under a bare
// `go test` (no tags) neither init() links, so the build would find "NBP not built"
// — hence the constraint mirrors the two registration gates (satisfied together by
// the umbrella `all` tag).

package runtime

// afp_nbp_test.go guards AFP Chooser discovery: the AFP file server must register its
// serverName:AFPServer@zone name with NBP on Start, and — when the [AFP] section names no
// zone of its own — fall back to the router's configured default_zone. Resolving the zone
// from CONFIG (not the live ZIT) is what makes this independent of startup ordering: AFP
// starts before the router's member ports attach and seed their zones, so a live-ZIT lookup
// would be empty and the server would register into no zone (invisible in the Chooser).

import (
	"context"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/port"
	"github.com/ObsoleteMadness/ClassicStack/core/service/nbp"

	// Real component registrations for the registry-driven Build.
	_ "github.com/ObsoleteMadness/ClassicStack/core/port/ethertalk"
	_ "github.com/ObsoleteMadness/ClassicStack/core/router"
	_ "github.com/ObsoleteMadness/ClassicStack/core/service/afp"
)

// TestAFPRegistersNBPNameInRouterDefaultZone builds the real AFP + NBP + router stack from a
// config whose [AFP] zone is blank, and asserts AFP registered AFPServer in the router's
// default_zone at its ASP socket.
func TestAFPRegistersNBPNameInRouterDefaultZone(t *testing.T) {
	m := config.NewModel()
	m.Router = config.RouterSection{DefaultZone: "EtherTalk Network", Members: []string{"EtherTalk"}}
	// A seed EtherTalk member so the router has a zone; enabled so it is built + attached.
	m.AddInstance(&port.Section{
		SKey: "EtherTalk", IsEnabled: true,
		SeedNetwork: 3, SeedNetworkEnd: 5, SeedZone: "EtherTalk Network",
	})
	// No [AFP] section is set, so its server section decodes to defaults with a BLANK zone
	// → the registry must fall back to the router default_zone for the NBP registration.

	rt, err := Build(Options{Model: m})
	if err != nil {
		t.Fatalf("Build = %v", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start = %v", err)
	}
	t.Cleanup(func() { rt.Stop(context.Background()) })

	c := rt.Component(nbp.Name)
	if c == nil {
		t.Fatal("NBP not built")
	}
	names := c.(*nbp.Service).Names()
	var found *nbp.RegisteredName
	for i := range names {
		if string(names[i].Type) == "AFPServer" {
			found = &names[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("AFP did not register an AFPServer NBP name; names = %+v", names)
	}
	if string(found.Zone) != "EtherTalk Network" {
		t.Errorf("AFPServer zone = %q, want the router default_zone %q", found.Zone, "EtherTalk Network")
	}
}
