package main

import (
	"context"

	"github.com/ObsoleteMadness/ClassicStack/service/netbios"
)

// NetBIOSHook is the cmd-layer abstraction over the optional NetBIOS
// service. NetBIOS does not consume DDP datagrams and is not a member
// of the AppleTalk service set; it is driven independently via
// Start/Stop, like IPX and NetBEUI.
type NetBIOSHook interface {
	Start(ctx context.Context) error
	Stop() error
	NameService() netbios.NameService
	Service() *netbios.Service
}

// NetBIOSConfig collects every value wireNetBIOS needs. IPX and
// NetBEUI hooks are passed in so over_ipx / over_netbeui transports
// can share their underlying router/port.
type NetBIOSConfig struct {
	Enabled    bool
	Transports []string
	ScopeID    string
	ServerName string
	Workgroup  string
	IPX        IPXHook
	NetBEUI    NetBEUIHook
}
