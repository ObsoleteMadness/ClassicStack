package main

import "github.com/ObsoleteMadness/ClassicStack/pkg/shortname"

type ShortnameHook interface {
	Mapper() shortname.Mapper
}

type ShortnameConfig struct {
	Enabled bool
	Backend string
	DBPath  string
}
