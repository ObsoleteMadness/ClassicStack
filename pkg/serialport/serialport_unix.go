//go:build !windows

package serialport

import (
	"path/filepath"
	"sort"
)

// serialGlobs are the device-node patterns that typically correspond to
// serial ports across Linux and macOS. /dev/ttyS* are 16550 UARTs,
// ttyUSB*/ttyACM* are USB adaptors, ttyAMA* is the Raspberry Pi PL011
// UART (a common TashTalk host), and tty.*/cu.* are the macOS callout and
// dial-in nodes.
var serialGlobs = []string{
	"/dev/ttyS*",
	"/dev/ttyUSB*",
	"/dev/ttyACM*",
	"/dev/ttyAMA*",
	"/dev/tty.*",
	"/dev/cu.*",
}

// list globs the well-known serial device-node patterns. Missing patterns
// simply contribute no matches; the result is de-duplicated and sorted for
// stable UI ordering.
func list() ([]Info, error) {
	seen := make(map[string]struct{})
	var names []string
	for _, pattern := range serialGlobs {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			// Only ErrBadPattern is possible here, and our patterns are
			// static, so this should never happen; skip defensively.
			continue
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

	out := make([]Info, 0, len(names))
	for _, n := range names {
		out = append(out, Info{Name: n, Description: n})
	}
	return out, nil
}
