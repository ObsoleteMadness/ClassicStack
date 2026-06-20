//go:build windows

package serial

import (
	"errors"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// listPorts reads the COM port names from the Windows serial device map at
// HKLM\HARDWARE\DEVICEMAP\SERIALCOMM. Each value's data is the port name (e.g. "COM3");
// the value name is the underlying driver device path. The COM name is the friendly
// label, with the driver path appended only when it adds context, sorted numerically so
// the dropdown reads COM1, COM2, COM10.
func listPorts() ([]PortInfo, error) {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, `HARDWARE\DEVICEMAP\SERIALCOMM`, registry.QUERY_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return nil, nil // no serial ports present
		}
		return nil, err
	}
	defer func() { _ = key.Close() }()

	names, err := key.ReadValueNames(0)
	if err != nil {
		return nil, err
	}

	out := make([]PortInfo, 0, len(names))
	for _, valueName := range names {
		port, _, err := key.GetStringValue(valueName)
		if err != nil || port == "" {
			continue
		}
		label := port
		if driver := strings.TrimSpace(valueName); driver != "" && !strings.EqualFold(driver, port) {
			label = port + " (" + driver + ")"
		}
		out = append(out, PortInfo{Device: port, Label: label})
	}

	sort.Slice(out, func(i, j int) bool {
		ni, oki := comNumber(out[i].Device)
		nj, okj := comNumber(out[j].Device)
		if oki && okj {
			return ni < nj
		}
		return out[i].Device < out[j].Device
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
