//go:build pico && picow

package main

import "github.com/ObsoleteMadness/ClassicStack/core/config"

func setupWiFi(m *config.Model, wifiIP *string) {
	for _, iface := range m.Interfaces {
		if iface.EffectiveKind() == config.IfaceKindWifi && iface.SSID != "" {
			println("Initializing WiFi connecting to SSID:", iface.SSID)
			wifi := NewPicoWiFi()
			ip, err := wifi.Connect(iface.SSID, iface.Key)
			if err != nil {
				println("WiFi connection failed:", err.Error())
			} else {
				*wifiIP = ip
				println("WiFi connected successfully. IP:", ip)
			}
			break
		}
	}
}
