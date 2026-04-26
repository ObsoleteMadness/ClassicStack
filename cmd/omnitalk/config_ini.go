package main

import (
	"github.com/pgodw/omnitalk/config"
)

// fileConfig is the wiring-layer alias for config.Root. TOML parsing
// and defaulting live in the config package; this file preserves the
// cmd-local identifiers so main.go and its tests keep compiling.
type fileConfig = config.Root

func loadConfigFromFile(path string) (fileConfig, error) { return config.Load(path) }
func defaultFileConfig() fileConfig                      { return config.Defaults() }
func defaultMacGardenVolumePath(name string) string      { return config.DefaultMacGardenVolumePath(name) }
