package main

import (
	"github.com/ObsoleteMadness/ClassicStack/config"
	"github.com/ObsoleteMadness/ClassicStack/service"
	"github.com/ObsoleteMadness/ClassicStack/service/zip"
)

// AFPHook is the cmd-layer abstraction over the optional AFP file
// server (and its ASP/DSI transports). The real implementation lives
// behind //go:build afp; the disabled stub returns a nil hook so
// router-only builds compile without pulling in the AFP subsystem.
type AFPHook interface {
	// Services returns the services to register with the router.
	Services() []service.Service
	// AttachMacIP wires AFP's ASP session lifecycle to MacIP DHCP lease
	// pinning. No-op when AFP runs DSI-only or MacIP is not built.
	AttachMacIP(hooks AFPSessionHooks)
}

// AFPSessionHooks bridges ASP session lifecycle events to MacIP without
// exposing service/asp at the cmd-neutral layer.
type AFPSessionHooks interface {
	OnOpen(net uint16, node, sessID uint8)
	OnClose(sessID uint8)
	OnActivity(sessID uint8)
}

// AFPFlagInputs collects the flag values required to build AFP when no
// TOML config file is in use. When -config is given, flagInputs is
// ignored and AFP reads its section from the config.Source instead.
type AFPFlagInputs struct {
	ServerName         string
	Zone               string
	Protocols          string
	TCPAddr            string
	ExtensionMap       string
	DecomposedNames    bool
	CNIDBackend        string
	AppleDoubleMode    string
	VolumeFlagValues   []string // raw "Name:Path" flag entries
}

// AFPWiring is the input bundle for wireAFP.
type AFPWiring struct {
	// Source is the loaded TOML, when -config was used. Zero value
	// (Source{}) signals flag-only configuration.
	Source     config.Source
	FromConfig bool
	Flags      AFPFlagInputs
	NBP        *zip.NameInformationService
}
