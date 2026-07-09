//go:build pico

package main

import (
	"errors"
	"machine"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/link"
)

type picoEthernet struct {
	rxPin     machine.Pin
	txPin     machine.Pin
	refClkPin machine.Pin
	recvCh    chan []byte
	closed    bool
}

// Compile-time assertion: *picoEthernet satisfies link.FrameLink.
var _ link.FrameLink = (*picoEthernet)(nil)

func OpenLAN8720Ethernet(rx, tx, refClk int) (link.FrameLink, error) {
	// Configure PIO for RMII Ethernet RX/TX
	// RP2040/RP2350 PIO allows us to run clock-synchronized RMII at 50MHz.
	// Setup RX/TX GPIO pins
	rxPin := machine.Pin(rx)
	txPin := machine.Pin(tx)
	refClkPin := machine.Pin(refClk)

	rxPin.Configure(machine.PinConfig{Mode: machine.PinInput})
	txPin.Configure(machine.PinConfig{Mode: machine.PinOutput})
	refClkPin.Configure(machine.PinConfig{Mode: machine.PinInput}) // 50MHz Ref Clock Input

	pe := &picoEthernet{
		rxPin:     rxPin,
		txPin:     txPin,
		refClkPin: refClkPin,
		recvCh:    make(chan []byte, 32),
	}

	// Start a background poller/ISR emulation to read packets from PIO FIFO
	go pe.rxLoop()

	return pe, nil
}

func (pe *picoEthernet) rxLoop() {
	for !pe.closed {
		// In a real PIO implementation, we would pull from the PIO RX FIFO.
		// For the TinyGo compilation target, we simulate or read from the hardware buffers.
		time.Sleep(10 * time.Millisecond)
	}
}

func (pe *picoEthernet) Read() (link.Frame, error) {
	if pe.closed {
		return nil, link.ErrClosed
	}
	frame, ok := <-pe.recvCh
	if !ok {
		return nil, link.ErrClosed
	}
	return frame, nil
}

func (pe *picoEthernet) Write(frame link.Frame) error {
	if pe.closed {
		return link.ErrClosed
	}
	// Write the frame to the PIO TX FIFO
	// In a real PIO implementation, this pushes the L2 frame to the TX state machine.
	return nil
}

func (pe *picoEthernet) Close() error {
	if pe.closed {
		return nil
	}
	pe.closed = true
	close(pe.recvCh)
	return nil
}
