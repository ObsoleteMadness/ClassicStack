package main

import (
	"context"

	"github.com/ObsoleteMadness/ClassicStack/port/rawlink"
	"github.com/ObsoleteMadness/ClassicStack/router/ipx"
	ipxsvc "github.com/ObsoleteMadness/ClassicStack/service/ipx"
)

// IPXHook is the cmd-layer abstraction over the optional IPX subsystem.
// IPX runs on its own router (router/ipx) and is not a member of the
// AppleTalk service set, so the hook surface is a Start/Stop pair plus
// access to the IPX router and SAP agent for higher layers (NetBIOS
// over IPX) that need to register sockets and advertise services.
type IPXHook interface {
	Start(ctx context.Context) error
	Stop() error
	Router() ipx.Router
	SAP() *ipxsvc.SAPService
}

// IPXConfig collects the values wireIPX needs. Rawlink may be nil when
// IPX is enabled without a transport (e.g. an integration test that
// drives the router directly).
type IPXConfig struct {
	Enabled         bool
	Rawlink         rawlink.RawLink
	BridgeMode      string
	BridgeFrameMode string
	Interface       string
	BridgeHWAddress string
	Framing         string
	InternalNetwork string
	Filter          string
	CapturePath     string
	CaptureSnaplen  uint32
}
