//go:build pico && picow

package cyw43439

import (
	"errors"
	"machine"
	"time"

	"tinygo.org/x/drivers/net"
	"tinygo.org/x/drivers/net/cyw43439"
)

type Driver struct {
	dev    *cyw43439.Device
	netdev net.Device
}

// New creates a new CYW43439 driver.
func New() *Driver {
	return &Driver{}
}

// Init initializes the CYW43439 hardware interface on the Pico W.
func (d *Driver) Init() error {
	// Setup the internal pins for the CYW43439 on Pico W
	// These are defined in the machine package for the pico target.
	d.dev = cyw43439.New(
		machine.GPIO23, // WL_ON
		machine.GPIO24, // WL_DATA / SPI_MISO
		machine.GPIO25, // WL_CS
		machine.GPIO29, // WL_CLK
	)

	// Configure internal pins
	err := d.dev.Init()
	if err != nil {
		return err
	}

	// Retrieve the net.Device interface from the cyw43439 device
	d.netdev = d.dev.NetDevice()
	return nil
}

// Join connects to a WiFi access point with the given SSID and Key.
func (d *Driver) Join(ssid, key string) error {
	if d.dev == nil {
		return errors.New("cyw43439: driver not initialized")
	}

	// Configure WiFi client mode
	d.dev.SetMode(cyw43439.ModeSTA)

	// Join the access point
	cfg := cyw43439.Config{
		SSID:     ssid,
		Password: key,
	}

	// Try to connect (up to 15 seconds)
	var err error
	for i := 0; i < 3; i++ {
		err = d.dev.Join(&cfg)
		if err == nil {
			return nil
		}
		time.Sleep(5 * time.Second)
	}

	return err
}

// NetDevice returns the net.Device interface for registering with the network stack.
func (d *Driver) NetDevice() net.Device {
	return d.netdev
}

// GetIP returns the current IP address assigned to the WiFi interface.
func (d *Driver) GetIP() (string, error) {
	if d.dev == nil {
		return "", errors.New("cyw43439: driver not initialized")
	}
	ip, _, _, err := d.dev.GetIP()
	if err != nil {
		return "", err
	}
	return ip.String(), nil
}
