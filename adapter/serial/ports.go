package serial

// ports.go enumerates the host's serial ports so the management UI can offer a TashTalk
// port dropdown (COM* on Windows, /dev/tty* on Unix). It is a thin per-OS lookup with no
// serial-library dependency. Ported from the legacy pkg/serialport, re-homed into the
// serial adapter that already owns the device-name helpers.

// PortInfo describes one serial port.
type PortInfo struct {
	// Device is the OS device path used to open the port (e.g. "COM3", "/dev/ttyUSB0").
	Device string `json:"device"`
	// Label is a human-friendly name when the OS provides one; else it equals Device.
	Label string `json:"label"`
}

// ListPorts returns the serial ports currently present on the host. Best-effort: an
// empty slice (not an error) when none are found; errors are reserved for OS-query
// failures. The per-OS body lives in ports_windows.go / ports_other.go.
func ListPorts() ([]PortInfo, error) {
	return listPorts()
}
