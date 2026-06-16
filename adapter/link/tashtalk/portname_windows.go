//go:build windows

package tashtalk

import (
	"strconv"
	"strings"
)

// normalizeSerialPortName maps a bare "COMn" to the "\\.\COMn" device path the
// Windows serial API requires for ports above COM9 (spec/08 §"Windows-Specific
// Notes"). An already-prefixed or non-COM name is returned unchanged. Ported
// from the legacy TashTalkPort.normalizeSerialPortName.
func normalizeSerialPortName(name string) string {
	if strings.HasPrefix(name, `\\.\`) {
		return name
	}
	upper := strings.ToUpper(strings.TrimSpace(name))
	if !strings.HasPrefix(upper, "COM") {
		return name
	}
	if _, err := strconv.Atoi(strings.TrimPrefix(upper, "COM")); err != nil {
		return name
	}
	return `\\.\` + upper
}
