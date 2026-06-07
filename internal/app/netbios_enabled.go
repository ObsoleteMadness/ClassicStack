//go:build netbios || all

package app

import (
	"context"

	"github.com/ObsoleteMadness/ClassicStack/netlog"
	netbiosproto "github.com/ObsoleteMadness/ClassicStack/protocol/netbios"
	"github.com/ObsoleteMadness/ClassicStack/service/netbios"
	"github.com/ObsoleteMadness/ClassicStack/service/netbios/over_ipx"
	"github.com/ObsoleteMadness/ClassicStack/service/netbios/over_netbeui"
	"github.com/ObsoleteMadness/ClassicStack/service/netbios/over_tcp"
)

type netbiosHookEnabled struct {
	svc      *netbios.Service
	builders []netbiosNamedBuilder
}

// Start binds every configured transport by name, then brings the service
// up (which starts the bound transports). Binding before Start means the
// service starts each transport exactly once.
func (h *netbiosHookEnabled) Start(ctx context.Context) error {
	for _, b := range h.builders {
		if err := h.svc.AddTransport(b.name, b.build()); err != nil {
			netlog.Warn("[MAIN][NetBIOS] bind transport %q: %v", b.name, err)
		}
	}
	return h.svc.Start(ctx)
}

func (h *netbiosHookEnabled) Stop() error                      { return h.svc.Stop() }
func (h *netbiosHookEnabled) NameService() netbios.NameService { return h.svc.NameService() }
func (h *netbiosHookEnabled) Service() *netbios.Service        { return h.svc }

// BuildTransport returns a freshly built transport bound under the canonical
// protocol name, or nil if that protocol is not a configured NetBIOS
// transport. The supervisor uses it to re-attach a transport when its
// underlying protocol is started again from the UI.
func (h *netbiosHookEnabled) BuildTransport(name string) netbios.Transport {
	for _, b := range h.builders {
		if b.name == name {
			return b.build()
		}
	}
	return nil
}

func wireNetBIOS(cfg NetBIOSConfig) (NetBIOSHook, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	builders := netbiosTransportBuilders(cfg)
	svc := netbios.NewService(cfg.ServerName, cfg.ScopeID, nil)
	netlog.Info("[MAIN][NetBIOS] server=%q scope=%q transports=%d",
		cfg.ServerName, cfg.ScopeID, len(builders))
	return &netbiosHookEnabled{svc: svc, builders: builders}, nil
}

// netbiosTransportBuilder constructs a fresh Transport for a single bound
// protocol. It is invoked at NetBIOS startup and again when the underlying
// protocol is restarted from the UI (so the transport re-attaches to the
// freshly started port/router).
type netbiosTransportBuilder func() netbios.Transport

// netbiosTransportBuilders maps each configured, available transport to a
// builder keyed by the canonical protocol name ("ipx", "netbeui", "tcp").
// Transports whose underlying hook is unavailable (e.g. "ipx" requested but
// the IPX router/SAP not wired) are skipped with a warning. The order of
// cfg.Transports is preserved so status reporting is stable.
func netbiosTransportBuilders(cfg NetBIOSConfig) []netbiosNamedBuilder {
	var out []netbiosNamedBuilder
	for _, name := range cfg.Transports {
		switch name {
		case "tcp":
			out = append(out, netbiosNamedBuilder{name: "tcp", build: over_tcp.NewTransport})
		case "netbeui":
			if cfg.NetBEUI != nil && cfg.NetBEUI.Port() != nil {
				nb := cfg.NetBEUI
				out = append(out, netbiosNamedBuilder{name: "netbeui", build: func() netbios.Transport {
					return over_netbeui.NewTransport(nb.Port(), nb.MAC())
				}})
			} else {
				netlog.Warn("[MAIN][NetBIOS] transport %q skipped: NetBEUI port not available", name)
			}
		case "ipx":
			if cfg.IPX != nil && cfg.IPX.Router() != nil && cfg.IPX.SAP() != nil {
				ipxHook := cfg.IPX
				server := cfg.ServerName
				out = append(out, netbiosNamedBuilder{name: "ipx", build: func() netbios.Transport {
					nbName := netbiosproto.NewName(server, netbiosproto.NameTypeFileServer)
					return over_ipx.NewTransport(ipxHook.Router(), ipxHook.SAP(), nbName)
				}})
			} else {
				netlog.Warn("[MAIN][NetBIOS] transport %q skipped: IPX router/SAP not available", name)
			}
		default:
			netlog.Warn("[MAIN][NetBIOS] unknown transport %q, ignoring", name)
		}
	}
	return out
}

// netbiosNamedBuilder pairs a transport's canonical name with its builder.
type netbiosNamedBuilder struct {
	name  string
	build netbiosTransportBuilder
}
