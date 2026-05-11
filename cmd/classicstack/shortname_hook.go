package main

import "github.com/ObsoleteMadness/ClassicStack/pkg/vfs"

type ShortnameHook interface {
	Mapper() vfs.ShortnameMapper
}

type ShortnameConfig struct {
	WindowsShortnames bool
	Backend           string
	DBPath            string
}
