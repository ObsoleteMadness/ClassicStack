package main

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
	Shortname     ShortnameHook
}
