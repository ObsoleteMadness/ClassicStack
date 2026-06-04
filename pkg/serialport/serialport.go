// Package serialport enumerates the host's serial ports so the management
// UI can offer a TashTalk port dropdown (COM* on Windows, /dev/tty* on
// Unix). It deliberately avoids a serial-library dependency — listing is a
// thin per-OS lookup. The package is untagged so any front-end can call it.
package serialport

// Info describes a single serial port.
type Info struct {
	// Name is the OS device path used to open the port, e.g. "COM3" or
	// "/dev/ttyUSB0".
	Name string `json:"name"`
	// Description is a human-friendly label when the OS provides one;
	// otherwise it equals Name.
	Description string `json:"description"`
}

// List returns the serial ports currently present on the host. The result
// is best-effort: an empty slice (not an error) is returned when none are
// found. Errors are reserved for failures querying the OS.
func List() ([]Info, error) {
	return list()
}
