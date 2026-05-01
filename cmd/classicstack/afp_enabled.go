//go:build afp || all

package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/config"
	"github.com/ObsoleteMadness/ClassicStack/netlog"
	"github.com/ObsoleteMadness/ClassicStack/service"
	"github.com/ObsoleteMadness/ClassicStack/service/afp"
	"github.com/ObsoleteMadness/ClassicStack/service/asp"
	"github.com/ObsoleteMadness/ClassicStack/service/dsi"
)

type afpHookEnabled struct {
	services []service.Service
	asp      *asp.Service // nil when DDP transport disabled
}

func (h *afpHookEnabled) Services() []service.Service { return h.services }

func (h *afpHookEnabled) AttachMacIP(hooks AFPSessionHooks) {
	if h == nil || h.asp == nil || hooks == nil {
		return
	}
	h.asp.SetSessionLifecycleHooks(
		func(sess *asp.Session) { hooks.OnOpen(sess.WSNet, sess.WSNode, sess.ID) },
		func(sess *asp.Session) { hooks.OnClose(sess.ID) },
		func(sess *asp.Session) { hooks.OnActivity(sess.ID) },
	)
}

// wireAFP builds the AFP file server, its transports (ASP over DDP and
// DSI over TCP), and returns a hook the rest of main.go uses to attach
// the resulting services to the router.
func wireAFP(in AFPWiring) (AFPHook, error) {
	cfg := afp.DefaultConfig()
	if in.FromConfig {
		if err := loadAFPSection(in.Source, &cfg); err != nil {
			return nil, err
		}
	} else {
		applyAFPFlagsToConfig(in.Flags, &cfg)
	}

	if !cfg.Enabled || len(cfg.Volumes) == 0 {
		return &afpHookEnabled{}, nil
	}

	vols, err := cfg.ResolvedVolumes()
	if err != nil {
		return nil, fmt.Errorf("AFP volume config: %w", err)
	}

	var extMap *afp.ExtensionMap
	if cfg.ExtensionMap != "" {
		loaded, err := loadAFPExtensionMap(cfg.ExtensionMap)
		if err != nil {
			return nil, fmt.Errorf("failed loading AFP extension map %q: %w", cfg.ExtensionMap, err)
		}
		extMap = loaded
	}

	hasDDP, hasTCP := splitAFPProtocols(cfg.Protocols)

	hook := &afpHookEnabled{}
	var transports []afp.Transport

	if hasDDP {
		aspSvc := asp.New(cfg.Name, nil, in.NBP, []byte(cfg.Zone))
		hook.asp = aspSvc
		transports = append(transports, aspSvc)
		netlog.Info("[MAIN][AFP] enabled DDP transport on socket %d", asp.ServerSocket)
	}

	if hasTCP {
		dsiSvc := dsi.NewServer(cfg.Name, cfg.Binding, nil)
		transports = append(transports, dsiSvc)
		netlog.Info("[MAIN][AFP] enabled TCP transport on %s", cfg.Binding)
	}

	mode, err := afp.ParseAppleDoubleMode(cfg.AppleDoubleMode)
	if err != nil {
		return nil, fmt.Errorf("AFP: %w", err)
	}
	afpSvc := afp.NewService(
		cfg.Name,
		vols,
		nil,
		transports,
		afp.Options{
			DecomposedFilenames: cfg.UseDecomposedNames,
			CNIDBackend:         cfg.CNIDBackend,
			AppleDoubleMode:     mode,
			ExtensionMap:        extMap,
			PersistentVolumeIDs: cfg.PersistentVolumeIDs,
		},
	)
	for _, t := range transports {
		switch transport := t.(type) {
		case *asp.Service:
			transport.SetCommandHandler(afpSvc)
		case *dsi.Server:
			transport.SetCommandHandler(afpSvc)
		}
	}

	hook.services = append(hook.services, afpSvc)
	netlog.Info("[MAIN][AFP] server=%q volumes=%d zone=%q protocols=%q", cfg.Name, len(vols), cfg.Zone, cfg.Protocols)
	return hook, nil
}

// loadAFPSection unmarshals [AFP] into cfg, validates it, and resolves
// a relative extension_map path against the config-file directory.
func loadAFPSection(src config.Source, cfg *afp.Config) error {
	if err := loadSection(src.K, "AFP", cfg); err != nil {
		return err
	}
	if cfg.ExtensionMap != "" && !filepath.IsAbs(cfg.ExtensionMap) && src.ConfigDir != "" {
		cfg.ExtensionMap = filepath.Join(src.ConfigDir, cfg.ExtensionMap)
	}
	if !cfg.Enabled {
		cfg.Volumes = nil
	}
	return nil
}

func applyAFPFlagsToConfig(f AFPFlagInputs, cfg *afp.Config) {
	if f.ServerName != "" {
		cfg.Name = f.ServerName
	}
	cfg.Zone = f.Zone
	if f.Protocols != "" {
		cfg.Protocols = f.Protocols
	}
	if f.TCPAddr != "" {
		cfg.Binding = f.TCPAddr
	}
	cfg.ExtensionMap = f.ExtensionMap
	cfg.UseDecomposedNames = f.DecomposedNames
	if f.CNIDBackend != "" {
		cfg.CNIDBackend = f.CNIDBackend
	}
	if f.AppleDoubleMode != "" {
		cfg.AppleDoubleMode = f.AppleDoubleMode
	}
	if len(f.VolumeFlagValues) == 0 {
		return
	}
	if cfg.Volumes == nil {
		cfg.Volumes = make(map[string]afp.VolumeConfig)
	}
	for _, raw := range f.VolumeFlagValues {
		v, err := afp.ParseVolumeFlag(raw)
		if err != nil {
			netlog.Warn("[MAIN][AFP] %v", err)
			continue
		}
		cfg.Volumes[v.Name] = v
	}
}

func splitAFPProtocols(s string) (ddp, tcp bool) {
	for _, p := range strings.Split(s, ",") {
		switch strings.ToLower(strings.TrimSpace(p)) {
		case "ddp":
			ddp = true
		case "tcp":
			tcp = true
		}
	}
	return
}
