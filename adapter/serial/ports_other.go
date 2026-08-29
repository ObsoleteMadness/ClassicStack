//go:build !windows

package serial

import (
	"path/filepath"
	"sort"
)

// serialGlobs are the device-node patterns that typically correspond to serial ports
// across Linux and macOS: /dev/ttyS* (16550 UARTs), ttyUSB*/ttyACM* (USB adaptors),
// ttyAMA* (the Raspberry Pi PL011 UART, a common TashTalk host), and tty.*/cu.* (the
// macOS callout/dial-in nodes).
var serialGlobs = []string{
	"/dev/ttyS*",
	"/dev/ttyUSB*",
	"/dev/ttyACM*",
	"/dev/ttyAMA*",
	"/dev/tty.*",
	"/dev/cu.*",
}

// listPorts globs the well-known serial device-node patterns, de-duplicating and
// sorting for stable UI ordering. A missing pattern contributes no matches.
func listPorts() ([]PortInfo, error) {
	seen := make(map[string]struct{})
	var names []string
	for _, pattern := range serialGlobs {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			continue // only ErrBadPattern, impossible for our static patterns
		}
		for _, m := range matches {
			if _, ok := seen[m]; ok {
				continue
			}
			seen[m] = struct{}{}
			names = append(names, m)
		}
	}
	sort.Strings(names)

	out := make([]PortInfo, 0, len(names))
	for _, n := range names {
		out = append(out, PortInfo{Device: n, Label: n})
	}
	return out, nil
}
