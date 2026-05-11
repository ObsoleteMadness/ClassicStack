package main

import (
	"github.com/ObsoleteMadness/ClassicStack/pkg/shortname"
	"github.com/ObsoleteMadness/ClassicStack/pkg/vfs"
)

type shortnameHook struct {
	mapper vfs.ShortnameMapper
}

func (h *shortnameHook) Mapper() vfs.ShortnameMapper { return h.mapper }

func wireShortname(cfg ShortnameConfig) (ShortnameHook, error) {
	var store shortname.Store
	if cfg.Backend == "sqlite" {
		// store = ... (left for future sqlite implementation)
	}
	if store == nil {
		store = shortname.NewMemoryStore()
	}
	mapper := shortname.NewMapper(store, shortname.Config{
		WindowsShortnames: cfg.WindowsShortnames,
	})
	return &shortnameHook{mapper: mapper}, nil
}
