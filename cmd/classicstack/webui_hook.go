package main

import "context"

// WebUIHook is the cmd-layer abstraction over the optional management web
// UI. Like SMB, the web UI is not a DDP service; main.go drives Start/Stop
// on it directly. The concrete implementation lives behind //go:build
// webui (webui_enabled.go); the disabled stub satisfies the same contract
// so the rest of main.go is tag-agnostic.
type WebUIHook interface {
	Start(ctx context.Context) error
	Stop() error
}

// WebUIWiring collects everything wireWebUI needs. The control plane is
// passed as an interface{} so this neutral file does not depend on the
// pkg/control types (which the disabled build still links). The enabled
// build type-asserts it back to *control.Plane.
type WebUIWiring struct {
	Options WebUIConfigOptions
	// Plane is the *control.Plane the UI drives. Typed as any so the
	// disabled stub (which ignores it) need not import pkg/control.
	Plane any
}
