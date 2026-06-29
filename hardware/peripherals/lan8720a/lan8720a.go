//go:build tinygo

package lan8720a

import (
	"errors"
	"machine"
	"time"
)

// Registers
const (
	RegBCR = 0  // Basic Control Register
	RegBSR = 1  // Basic Status Register
	RegPHY1 = 2 // PHY Identifier 1
	RegPHY2 = 3 // PHY Identifier 2
	RegSCSR = 31 // Special Control/Status Register (LAN8720A specific)
)

// Basic Control Register bits
const (
	BCRReset      = 1 << 15
	BCRLoopback   = 1 << 14
	BCRSpeed100   = 1 << 13
	BCRAutoNeg    = 1 << 12
	BCRPowerDown  = 1 << 11
	BCRIsolate    = 1 << 10
	BCRRestartNeg = 1 << 9
	BCRDuplexFull = 1 << 8
)

// Basic Status Register bits
const (
	BSRAutoNegComp = 1 << 5
	BSRLinkStatus  = 1 << 2
)

// Special Control/Status Register bits (Register 31)
const (
	SCSRSpeedMask   = 0x001C
	SCSRSpeed10Half = 0x0004
	SCSRSpeed10Full = 0x0014
	SCSRSpeed100Half = 0x0008
	SCSRSpeed100Full = 0x0018
)

type Driver struct {
	mdc     machine.Pin
	mdio    machine.Pin
	power   machine.Pin
	phyAddr uint8
}

// New creates a new LAN8720A driver instance.
func New(mdc, mdio, power machine.Pin, phyAddr uint8) *Driver {
	return &Driver{
		mdc:     mdc,
		mdio:    mdio,
		power:   power,
		phyAddr: phyAddr,
	}
}

// Init configures the control pins and powers up the PHY.
func (d *Driver) Init() error {
	// Configure pins
	d.mdc.Configure(machine.PinConfig{Mode: machine.PinOutput})
	d.mdio.Configure(machine.PinConfig{Mode: machine.PinOutput})
	
	if d.power != machine.NoPin {
		d.power.Configure(machine.PinConfig{Mode: machine.PinOutput})
		// Power cycle PHY (active high)
		d.power.Low()
		time.Sleep(100 * time.Millisecond)
		d.power.High()
		time.Sleep(100 * time.Millisecond)
	}

	// Verify we can communicate with the PHY by reading its ID
	id1 := d.readReg(RegPHY1)
	id2 := d.readReg(RegPHY2)
	if id1 == 0xFFFF || id1 == 0x0000 {
		return errors.New("lan8720a: failed to communicate with PHY")
	}

	return d.Reset()
}

// Reset triggers a software reset on the PHY and waits for it to complete.
func (d *Driver) Reset() error {
	d.writeReg(RegBCR, BCRReset)
	
	// Wait for reset bit to clear (up to 500ms)
	for i := 0; i < 50; i++ {
		time.Sleep(10 * time.Millisecond)
		bcr := d.readReg(RegBCR)
		if bcr&BCRReset == 0 {
			// Configure auto-negotiation by default
			d.writeReg(RegBCR, BCRAutoNeg|BCRRestartNeg)
			return nil
		}
	}
	return errors.New("lan8720a: PHY reset timeout")
}

type LinkInfo struct {
	Up     bool
	Speed  int  // 10 or 100 Mbps
	Duplex bool // true = full, false = half
}

// GetLinkInfo reads the PHY registers and returns the current link status.
func (d *Driver) GetLinkInfo() LinkInfo {
	bsr := d.readReg(RegBSR)
	if bsr&BSRLinkStatus == 0 {
		return LinkInfo{Up: false}
	}

	scsr := d.readReg(RegSCSR)
	speed := 10
	duplex := false

	switch scsr & SCSRSpeedMask {
	case SCSRSpeed10Half:
		speed = 10
		duplex = false
	case SCSRSpeed10Full:
		speed = 10
		duplex = true
	case SCSRSpeed100Half:
		speed = 100
		duplex = false
	case SCSRSpeed100Full:
		speed = 100
		duplex = true
	}

	return LinkInfo{
		Up:     true,
		Speed:  speed,
		Duplex: duplex,
	}
}

// --- Low-level bit-banged SMI (MDC/MDIO) interface ---

func (d *Driver) writeBit(bit bool) {
	if bit {
		d.mdio.High()
	} else {
		d.mdio.Low()
	}
	time.Sleep(1 * time.Microsecond)
	d.mdc.High()
	time.Sleep(1 * time.Microsecond)
	d.mdc.Low()
}

func (d *Driver) readBit() bool {
	d.mdc.High()
	time.Sleep(1 * time.Microsecond)
	bit := d.mdio.Get()
	d.mdc.Low()
	time.Sleep(1 * time.Microsecond)
	return bit
}

func (d *Driver) writeReg(reg uint8, value uint16) {
	d.mdio.Configure(machine.PinConfig{Mode: machine.PinOutput})
	
	// Preamble: 32 ones
	for i := 0; i < 32; i++ {
		d.writeBit(true)
	}
	// ST: Start of frame (01)
	d.writeBit(false)
	d.writeBit(true)
	// OP: Write (01)
	d.writeBit(false)
	d.writeBit(true)
	// PHYAD: 5 bits
	for i := 4; i >= 0; i-- {
		d.writeBit((d.phyAddr >> i) & 1 == 1)
	}
	// REGAD: 5 bits
	for i := 4; i >= 0; i-- {
		d.writeBit((reg >> i) & 1 == 1)
	}
	// TA: Turnaround (10)
	d.writeBit(true)
	d.writeBit(false)
	// DATA: 16 bits
	for i := 15; i >= 0; i-- {
		d.writeBit((value >> i) & 1 == 1)
	}
	
	// Release MDIO line
	d.mdio.Configure(machine.PinConfig{Mode: machine.PinInput})
}

func (d *Driver) readReg(reg uint8) uint16 {
	d.mdio.Configure(machine.PinConfig{Mode: machine.PinOutput})
	
	// Preamble: 32 ones
	for i := 0; i < 32; i++ {
		d.writeBit(true)
	}
	// ST: Start of frame (01)
	d.writeBit(false)
	d.writeBit(true)
	// OP: Read (10)
	d.writeBit(true)
	d.writeBit(false)
	// PHYAD: 5 bits
	for i := 4; i >= 0; i-- {
		d.writeBit((d.phyAddr >> i) & 1 == 1)
	}
	// REGAD: 5 bits
	for i := 4; i >= 0; i-- {
		d.writeBit((reg >> i) & 1 == 1)
	}
	// TA: Turnaround (Z0)
	d.mdio.Configure(machine.PinConfig{Mode: machine.PinInput})
	d.mdc.High()
	time.Sleep(1 * time.Microsecond)
	d.mdc.Low()
	time.Sleep(1 * time.Microsecond)
	
	// DATA: 16 bits
	var val uint16
	for i := 15; i >= 0; i-- {
		val <<= 1
		if d.readBit() {
			val |= 1
		}
	}
	return val
}
