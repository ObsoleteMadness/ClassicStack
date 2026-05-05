//go:build netbios || all

package main

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
	svc *netbios.Service
}

func (h *netbiosHookEnabled) Start(ctx context.Context) error      { return h.svc.Start(ctx) }
func (h *netbiosHookEnabled) Stop() error                          { return h.svc.Stop() }
func (h *netbiosHookEnabled) NameService() netbios.NameService     { return h.svc.NameService() }
func (h *netbiosHookEnabled) Service() *netbios.Service            { return h.svc }

func wireNetBIOS(cfg NetBIOSConfig) (NetBIOSHook, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	transports := selectNetBIOSTransports(cfg)
	svc := netbios.NewService(cfg.ServerName, cfg.ScopeID, transports)
	netlog.Info("[MAIN][NetBIOS] server=%q scope=%q transports=%d (stub)",
		cfg.ServerName, cfg.ScopeID, len(transports))
	return &netbiosHookEnabled{svc: svc}, nil
}

// selectNetBIOSTransports turns the config's transport name list into
// concrete Transport instances, skipping any whose underlying hook is
// not available (e.g. "ipx" requested but binary built without -tags ipx).
func selectNetBIOSTransports(cfg NetBIOSConfig) []netbios.Transport {
	var out []netbios.Transport
	for _, name := range cfg.Transports {
		switch name {
		case "tcp":
			out = append(out, over_tcp.NewTransport())
		case "netbeui":
			if cfg.NetBEUI != nil && cfg.NetBEUI.Port() != nil {
				out = append(out, over_netbeui.NewTransport(cfg.NetBEUI.Port()))
			} else {
				netlog.Warn("[MAIN][NetBIOS] transport %q skipped: NetBEUI port not available", name)
			}
		case "ipx":
			if cfg.IPX != nil && cfg.IPX.Router() != nil && cfg.IPX.SAP() != nil {
				nbName := netbiosproto.NewName(cfg.ServerName, netbiosproto.NameTypeFileServer)
				out = append(out, over_ipx.NewTransport(cfg.IPX.Router(), cfg.IPX.SAP(), nbName))
			} else {
				netlog.Warn("[MAIN][NetBIOS] transport %q skipped: IPX router/SAP not available", name)
			}
		default:
			netlog.Warn("[MAIN][NetBIOS] unknown transport %q, ignoring", name)
		}
	}
	return out
}
