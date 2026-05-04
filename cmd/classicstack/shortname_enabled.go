//go:build shortname || all

package main

import "github.com/ObsoleteMadness/ClassicStack/pkg/shortname"

type shortnameHook struct {
	mapper shortname.Mapper
}

func (h *shortnameHook) Mapper() shortname.Mapper { return h.mapper }

func wireShortname(cfg ShortnameConfig) (ShortnameHook, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	var store shortname.Store
	if cfg.Backend == "memory" || cfg.Backend == "" {
		store = shortname.NewMemoryStore()
	}
	mapper := shortname.NewMapper(store)
	return &shortnameHook{mapper: mapper}, nil
}
