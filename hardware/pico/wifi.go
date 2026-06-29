//go:build pico && picow

package main

import (
	"errors"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/hardware/peripherals/cyw43439"
)

type PicoWiFi struct {
	driver *cyw43439.Driver
}

func NewPicoWiFi() *PicoWiFi {
	return &PicoWiFi{
		driver: cyw43439.New(),
	}
}

func (w *PicoWiFi) Connect(ssid, key string) (string, error) {
	println("Initializing CYW43439 WiFi driver...")
	if err := w.driver.Init(); err != nil {
		return "", err
	}

	println("Connecting to SSID:", ssid)
	if err := w.driver.Join(ssid, key); err != nil {
		return "", err
	}

	// Wait for IP assignment
	for i := 0; i < 10; i++ {
		time.Sleep(1 * time.Second)
		ip, err := w.driver.GetIP()
		if err == nil && ip != "0.0.0.0" && ip != "" {
			return ip, nil
		}
	}

	return "", errors.New("wifi: DHCP IP assignment timeout")
}
