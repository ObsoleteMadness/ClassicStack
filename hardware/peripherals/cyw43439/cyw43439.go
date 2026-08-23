//go:build (pico || pico2) && picow

// KNOWN GAP: this driver does not work yet. tinygo.org/x/drivers/net and
// tinygo.org/x/drivers/net/cyw43439 -- the packages this file was written
// against -- do not exist in any released tinygo.org/x/drivers version (v0.35.0,
// the latest, has neither); the Pico W's CYW43439 WiFi/Bluetooth radio has no
// TinyGo driver available yet (mirrors the hardware/peripherals/sdcard gap: see
// hardware/pico/main.go's comment on tinygo.org/x/drivers/fatfs). Init/Join/GetIP
// fail cleanly instead of not compiling; tracked as follow-up, not attempted here.
package cyw43439

import "errors"

// ErrNotImplemented is returned by Driver's methods until a CYW43439 driver exists.
var ErrNotImplemented = errors.New("cyw43439: no TinyGo driver is available yet (see hardware/peripherals/cyw43439/cyw43439.go)")

// Driver is a stub: see ErrNotImplemented.
type Driver struct{}

// New returns a stub Driver; see ErrNotImplemented.
func New() *Driver { return &Driver{} }

// Init always fails; see ErrNotImplemented.
func (d *Driver) Init() error { return ErrNotImplemented }

// Join always fails; see ErrNotImplemented.
func (d *Driver) Join(_, _ string) error { return ErrNotImplemented }

// GetIP always fails; see ErrNotImplemented.
func (d *Driver) GetIP() (string, error) { return "", ErrNotImplemented }
