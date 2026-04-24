package main

import (
	"github.com/pgodw/omnitalk/config"
)

// iniConfig is the wiring-layer alias for config.Root. INI parsing and
// defaulting live in the config package; this file preserves the legacy
// cmd-local identifiers so main.go and its tests keep compiling during
// the refactor.
type iniConfig = config.Root

func loadConfigFromINI(path string) (iniConfig, error) { return config.LoadINI(path) }
func defaultINIConfig() iniConfig                      { return config.Defaults() }
func defaultMacGardenVolumePath(name string) string    { return config.DefaultMacGardenVolumePath(name) }
