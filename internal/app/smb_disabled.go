//go:build !smb && !all

package app

import (
	"context"

	"github.com/ObsoleteMadness/ClassicStack/netlog"
	"github.com/ObsoleteMadness/ClassicStack/service/smb"
)

type smbHookDisabled struct{}

func (smbHookDisabled) Start(_ context.Context) error { return nil }
func (smbHookDisabled) Stop() error                   { return nil }
func (smbHookDisabled) Service() *smb.Service         { return nil }
func (smbHookDisabled) IPXDirect() startStopper       { return nil }

func wireSMB(cfg SMBConfig) (SMBHook, error) {
	if cfg.Enabled {
		netlog.Warn("[MAIN][SMB] -smb-enabled set but binary was built without -tags smb; ignoring")
	}
	return smbHookDisabled{}, nil
}
