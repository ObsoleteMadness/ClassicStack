package app

import (
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/config"
)

// TestAppConfigFromModel_RouterAttach verifies that [Router].ports drives the
// per-transport AttachRouter flags: an empty list binds every transport, while
// a non-empty list binds only the named ones (others run standalone).
func TestAppConfigFromModel_RouterAttach(t *testing.T) {
	t.Run("empty list binds all", func(t *testing.T) {
		m := config.Defaults()
		cfg, err := appConfigFromModel(m)
		if err != nil {
			t.Fatalf("appConfigFromModel: %v", err)
		}
		if !cfg.LToUDPAttachRouter || !cfg.TashTalkAttachRouter || !cfg.EtherTalkAttachRouter {
			t.Fatalf("empty Ports should attach all, got LToUDP=%v TashTalk=%v EtherTalk=%v",
				cfg.LToUDPAttachRouter, cfg.TashTalkAttachRouter, cfg.EtherTalkAttachRouter)
		}
	})

	t.Run("explicit list detaches the unlisted", func(t *testing.T) {
		m := config.Defaults()
		m.Router.Ports = []string{config.RouterPortLToUDP, config.RouterPortEtherTalk}
		cfg, err := appConfigFromModel(m)
		if err != nil {
			t.Fatalf("appConfigFromModel: %v", err)
		}
		if !cfg.LToUDPAttachRouter || !cfg.EtherTalkAttachRouter {
			t.Errorf("listed transports should attach; got LToUDP=%v EtherTalk=%v",
				cfg.LToUDPAttachRouter, cfg.EtherTalkAttachRouter)
		}
		if cfg.TashTalkAttachRouter {
			t.Errorf("unlisted TashTalk should be standalone, got attached")
		}
	})
}

// TestRouterPortsModel_Projection verifies modelFromAppConfig projects the
// resolved attach flags back into [Router].ports: nil when every configured
// transport is attached, and an explicit allow-list when one is detached.
func TestRouterPortsModel_Projection(t *testing.T) {
	base := defaultAppConfig()
	// Configure all three transports so they count as present.
	base.LToUDP.Enabled = true
	base.TashTalk.Port = "COM1"
	base.EtherTalk.Device = "eth0"

	t.Run("all attached projects no [Router] section", func(t *testing.T) {
		cfg := base
		if ports := routerPortsModel(cfg); ports != nil {
			t.Errorf("all attached should project nil Ports, got %v", ports)
		}
	})

	t.Run("one detached projects the attached allow-list", func(t *testing.T) {
		cfg := base
		cfg.TashTalkAttachRouter = false
		ports := routerPortsModel(cfg)
		want := []string{config.RouterPortLToUDP, config.RouterPortEtherTalk}
		if len(ports) != len(want) {
			t.Fatalf("Ports = %v, want %v", ports, want)
		}
		for i := range want {
			if ports[i] != want[i] {
				t.Fatalf("Ports = %v, want %v", ports, want)
			}
		}
	})

	t.Run("detached-but-unconfigured transport has no effect", func(t *testing.T) {
		cfg := defaultAppConfig()
		cfg.LToUDP.Enabled = true // only LToUDP configured
		cfg.LToUDPAttachRouter = true
		// TashTalk is "detached" but has no serial port -> not configured, so it
		// must not force an explicit allow-list. Every *configured* transport is
		// attached, so the projection stays nil (no [Router] section).
		cfg.TashTalkAttachRouter = false
		if ports := routerPortsModel(cfg); ports != nil {
			t.Fatalf("unconfigured detached transport should project nil, got %v", ports)
		}
	})

	t.Run("real detached transport among configured forces the list", func(t *testing.T) {
		cfg := base
		cfg.EtherTalkAttachRouter = false
		ports := routerPortsModel(cfg)
		want := []string{config.RouterPortLToUDP, config.RouterPortTashTalk}
		if len(ports) != len(want) || ports[0] != want[0] || ports[1] != want[1] {
			t.Fatalf("Ports = %v, want %v", ports, want)
		}
	})
}
