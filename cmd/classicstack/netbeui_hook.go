package main

import (
	"context"

	"github.com/ObsoleteMadness/ClassicStack/port/netbeui"
	"github.com/ObsoleteMadness/ClassicStack/port/rawlink"
)

// NetBEUIHook is the cmd-layer abstraction over the optional NetBEUI
// port. NetBEUI is a transport — it owns no service of its own, but
// publishes a Port that NetBIOS-over-NetBEUI can consume.
type NetBEUIHook interface {
	Start(ctx context.Context) error
	Stop() error
	Port() netbeui.Port
}

// NetBEUIConfig collects the values wireNetBEUI needs.
type NetBEUIConfig struct {
	Enabled   bool
	Rawlink   rawlink.RawLink
	Interface string
}
