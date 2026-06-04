//go:build webui || all

package main

import (
	"context"

	"github.com/ObsoleteMadness/ClassicStack/netlog"
	"github.com/ObsoleteMadness/ClassicStack/service/webui"
)

type webUIHookEnabled struct {
	srv *webui.Server
}

func (h *webUIHookEnabled) Start(ctx context.Context) error {
	if h.srv == nil {
		return nil
	}
	return h.srv.Start(ctx)
}

func (h *webUIHookEnabled) Stop() error {
	if h.srv == nil {
		return nil
	}
	return h.srv.Stop()
}

// wireWebUI constructs the HTTPS management server when the web UI is
// enabled. The control plane (passed via WebUIWiring.Plane) is the single
// management API the server adapts onto HTTP/SSE. When the UI is disabled
// a hook with a nil server is returned so Start/Stop are no-ops.
func wireWebUI(w WebUIWiring) (WebUIHook, error) {
	if !w.Options.Enabled {
		return &webUIHookEnabled{}, nil
	}
	plane, _ := w.Plane.(webui.ControlPlane)
	srv, err := webui.NewServer(webui.Options{
		Bind:    w.Options.Bind,
		TLS:     w.Options.TLS,
		CertPEM: w.Options.CertPEM,
		KeyPEM:  w.Options.KeyPEM,
		Plane:   plane,
	})
	if err != nil {
		return nil, err
	}
	netlog.Info("[MAIN][WebUI] enabled on %s (tls=%t)", w.Options.Bind, w.Options.TLS)
	return &webUIHookEnabled{srv: srv}, nil
}
