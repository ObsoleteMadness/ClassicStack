package app

import (
	"context"

	"github.com/ObsoleteMadness/ClassicStack/service/smb"
)

// SMBHook is the cmd-layer abstraction over the optional SMB 1.0
// server. SMB does not consume DDP and is not a member of the
// AppleTalk service set; main.go drives Start/Stop on it directly.
type SMBHook interface {
	Start(ctx context.Context) error
	Stop() error
	Service() *smb.Service
	// IPXDirect returns the SMB-over-direct-IPX transport, or nil when SMB
	// has no IPX transport (IPX disabled). The supervisor binds it so that
	// stopping IPX detaches it and starting IPX re-attaches it. It is a
	// minimal lifecycle handle to keep this interface free of build-tagged
	// transport types.
	IPXDirect() startStopper
}

// startStopper is the minimal lifecycle surface the supervisor needs to
// attach/detach a sub-transport binding.
type startStopper interface {
	Start(ctx context.Context) error
	Stop() error
}

// SMBConfig collects every value wireSMB needs.
type SMBConfig struct {
	Enabled       bool
	NBTBinding    string
	DirectBinding string
	GuestOk       bool
	Workgroup     string
	ServerName    string
	Shares        []smb.ShareConfig
	NetBIOS       NetBIOSHook
	IPX           IPXHook
	Shortname     ShortnameHook
}
