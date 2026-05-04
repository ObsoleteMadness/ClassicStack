//go:build smb || all

package main

import (
	"context"

	"github.com/ObsoleteMadness/ClassicStack/netlog"
	"github.com/ObsoleteMadness/ClassicStack/service/netbios"
	"github.com/ObsoleteMadness/ClassicStack/service/smb"
)

type smbHookEnabled struct {
	svc *smb.Service
}

func (h *smbHookEnabled) Start(ctx context.Context) error { return h.svc.Start(ctx) }
func (h *smbHookEnabled) Stop() error                     { return h.svc.Stop() }
func (h *smbHookEnabled) Service() *smb.Service           { return h.svc }

func wireSMB(cfg SMBConfig) (SMBHook, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	opts := smb.ServerOptions{
		NBTBinding:    cfg.NBTBinding,
		DirectBinding: cfg.DirectBinding,
		GuestOk:       cfg.GuestOk,
		Workgroup:     cfg.Workgroup,
		ServerName:    cfg.ServerName,
	}
	if cfg.Shortname != nil {
		opts.Shortname = cfg.Shortname.Mapper()
	}

	var nb netbios.NameService
	if cfg.NetBIOS != nil {
		nb = cfg.NetBIOS.NameService()
	}

	svc := smb.NewService(opts, nb, cfg.Shares)

	// Wire SMB into the NetBIOS dispatch chain so inbound session
	// PDUs reach the SMB command handler.
	if cfg.NetBIOS != nil {
		if nbSvc := cfg.NetBIOS.Service(); nbSvc != nil {
			nbSvc.SetCommandHandler(svc)
		}
	}

	netlog.Info("[MAIN][SMB] server=%q workgroup=%q shares=%d guest=%t (stub)",
		cfg.ServerName, cfg.Workgroup, len(cfg.Shares), cfg.GuestOk)
	return &smbHookEnabled{svc: svc}, nil
}
