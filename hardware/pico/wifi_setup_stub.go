//go:build pico && !picow

package main

import "github.com/ObsoleteMadness/ClassicStack/core/config"

func setupWiFi(m *config.Model, wifiIP *string) {
	// No-op on non-wireless Pico
}
