//go:build pico

package main

import (
	"errors"
	"machine"

	"github.com/ObsoleteMadness/ClassicStack/core/link"
	"github.com/ObsoleteMadness/ClassicStack/hardware/peripherals/w5500"
)

const (
	W5500_CLK  = 10
	W5500_MOSI = 11
	W5500_MISO = 12
	W5500_CS   = 13
	W5500_RST  = 8
	W5500_INT  = 9
)

func OpenW5500Ethernet() (link.FrameLink, error) {
	println("Initializing SPI1 for W5500...")
	spi := &machine.SPI1
	err := spi.Configure(machine.SPIConfig{
		Frequency: 16000000, // 16MHz SPI speed for W5500
		Mode:      0,
		SCK:       machine.Pin(W5500_CLK),
		SDO:       machine.Pin(W5500_MOSI),
		SDI:       machine.Pin(W5500_MISO),
	})
	if err != nil {
		return nil, errors.New("w5500: failed to configure SPI1")
	}

	println("Opening W5500 FrameLink...")
	csPin := machine.Pin(W5500_CS)
	rstPin := machine.Pin(W5500_RST)
	intPin := machine.Pin(W5500_INT)

	return w5500.OpenW5500(spi, csPin, rstPin, intPin)
}
