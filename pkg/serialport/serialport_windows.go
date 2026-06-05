//go:build windows

package serialport

import (
	"sort"
	"strconv"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// list reads the COM port names from the Windows serial device map at
// HKLM\HARDWARE\DEVICEMAP\SERIALCOMM. Each value's data is the port name
// (e.g. "COM3"); the value name is the underlying driver device path. We
// surface the COM name as the human-friendly label (e.g. "COM3"), appending
// the driver path only when it adds context, and sort numerically so the
// dropdown reads COM1, COM2, COM3 rather than driver-path order.
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
		// Label with the COM name first so the dropdown reads "COM3" rather
		// than the underlying \Device\... driver path. Keep the driver path
		// as trailing context when it differs and looks informative.
		desc := port
		if driver := strings.TrimSpace(valueName); driver != "" && !strings.EqualFold(driver, port) {
			desc = port + " (" + driver + ")"
		}
		out = append(out, Info{Name: port, Description: desc})
	}

	// Order COM1, COM2, COM10 numerically rather than by registry/driver order.
	sort.Slice(out, func(i, j int) bool {
		ni, oki := comNumber(out[i].Name)
		nj, okj := comNumber(out[j].Name)
		if oki && okj {
			return ni < nj
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// comNumber extracts the numeric suffix of a "COM<n>" name for sorting.
func comNumber(name string) (int, bool) {
	if !strings.HasPrefix(strings.ToUpper(name), "COM") {
		return 0, false
	}
	n, err := strconv.Atoi(name[3:])
	if err != nil {
		return 0, false
	}
	return n, true
}
