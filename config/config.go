// Package config abstracts where OmniTalk's configuration comes from
// (TOML file today; environment variables, JSON, etc. tomorrow). It owns
// no schema knowledge: each component decides what keys it consumes by
// reading from the returned koanf instance.
//
// Defaults live with the consumers (typically as flag defaults in
// cmd/omnitalk). The config package's only job is to surface a populated
// koanf source to those consumers.
package config

import (
	"path/filepath"

	"github.com/knadh/koanf/parsers/toml/v2"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

// Source is a parsed configuration source. Components read keys from K
// using their own schema. ConfigDir is the directory of the source file
// (or "" when no file backed the source) and is useful for resolving
// paths declared relative to the config file.
type Source struct {
	K         *koanf.Koanf
	ConfigDir string
}

// Empty returns a Source backed by an empty koanf instance — useful when
// no config file is present and consumers should fall back entirely to
// flag defaults.
func Empty() Source {
	return Source{K: koanf.New("."), ConfigDir: ""}
}

// Load parses path as TOML and returns a Source. The koanf delimiter is
// "." so nested tables (e.g. [Volumes.Default]) become "Volumes.Default".
func Load(path string) (Source, error) {
	k := koanf.New(".")
	if err := k.Load(file.Provider(path), toml.Parser()); err != nil {
		return Source{K: k}, err
	}
	return Source{K: k, ConfigDir: filepath.Dir(path)}, nil
}
