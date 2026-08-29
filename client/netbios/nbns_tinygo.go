//go:build tinygo

// TinyGo's baremetal targets have no net.ListenUDP, so the NBNS (UDP 137) master-
// browser lookup is unavailable there; the real implementation lives in nbns.go.
package netbios

import (
	"errors"
	"net"
	"time"
)

// errNBNSUnsupported is returned by LookupMasterBrowser on builds with no UDP socket.
var errNBNSUnsupported = errors.New("netbios: NBNS lookup is not supported on this build")

// LookupMasterBrowser is a stub on TinyGo builds: see errNBNSUnsupported.
func LookupMasterBrowser(_ net.IP, _ string, _ time.Duration) ([]NBNSAnswer, error) {
	return nil, errNBNSUnsupported
}
