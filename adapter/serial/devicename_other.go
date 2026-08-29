//go:build !windows

package serial

// normalizeDeviceName is a no-op off Windows: Unix/macOS device paths
// (/dev/ttyUSB0, /dev/cu.usbserial-*) are passed to the serial library verbatim.
func normalizeDeviceName(name string) string { return name }
