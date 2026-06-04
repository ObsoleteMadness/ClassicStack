//go:build windows

package serialport

import (
	"golang.org/x/sys/windows/registry"
)

// list reads the COM port names from the Windows serial device map at
// HKLM\HARDWARE\DEVICEMAP\SERIALCOMM. Each value's data is the port name
// (e.g. "COM3"); the value name is the underlying driver device path,
// which we surface as the description.
func list() ([]Info, error) {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, `HARDWARE\DEVICEMAP\SERIALCOMM`, registry.QUERY_VALUE)
	if err != nil {
		// No serial ports present: the key is absent. Treat as empty.
		if err == registry.ErrNotExist {
			return nil, nil
		}
		return nil, err
	}
	defer key.Close()

	names, err := key.ReadValueNames(0)
	if err != nil {
		return nil, err
	}

	out := make([]Info, 0, len(names))
	for _, valueName := range names {
		port, _, err := key.GetStringValue(valueName)
		if err != nil || port == "" {
			continue
		}
		out = append(out, Info{Name: port, Description: valueName})
	}
	return out, nil
}
