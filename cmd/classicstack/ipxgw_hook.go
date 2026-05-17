package main

import (
	routeripx "github.com/ObsoleteMadness/ClassicStack/router/ipx"
	"github.com/ObsoleteMadness/ClassicStack/service"
	"github.com/ObsoleteMadness/ClassicStack/service/zip"
)

// IPXGWHook is the cmd-layer abstraction over the optional AppleTalk-to-IPX
// gateway service. The real implementation lives behind //go:build ipxgw;
// the stub returns nil so router-only builds skip it.
//
// AttachIPXRouter is called after the IPX subsystem has been constructed
// so the gateway can claim assigned client nodes and forward encapsulated
// IPX into the native IPX router. Safe to call with nil (no-op) when
// IPX is disabled or built out.
type IPXGWHook interface {
	Service() service.Service
	AttachIPXRouter(r routeripx.Router)
}

// IPXGWZoneBinding is one NBP name the gateway should publish in a specific
// AppleTalk zone (object name + zone name). Mirrors service/ipxgw.ZoneBinding
// at the cmd layer so callers don't have to import the ipxgw package when
// the ipxgw build tag is off.
type IPXGWZoneBinding struct {
	Object string
	Zone   string
}

// IPXGWConfig collects everything wireIPXGW needs. If Bindings is empty the
// service falls back to one registration per zone the router knows about.
type IPXGWConfig struct {
	Enabled  bool
	Bindings []IPXGWZoneBinding
	NBP      *zip.NameInformationService

	// IPXNetwork is the IPX network the gateway considers itself
	// attached to. Used today only for logging. 0 ⇒ default
	// (0x00000010, the network observed in the source captures).
	IPXNetwork uint32
}
