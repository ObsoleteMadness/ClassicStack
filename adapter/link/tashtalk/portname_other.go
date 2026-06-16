//go:build !windows

package tashtalk

// normalizeSerialPortName is a no-op off Windows: Unix/macOS device paths
// (/dev/ttyUSB0, /dev/cu.usbserial-*) are passed to the serial library verbatim.
func normalizeSerialPortName(name string) string { return name }
