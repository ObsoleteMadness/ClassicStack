//go:build tinygo

// TinyGo's baremetal targets have no multicast UDP socket (golang.org/x/net/ipv4)
// for LToUDP, and adapter/serial (host termios/COM-port enumeration) is desktop-only
// too, so neither LocalTalk transport is available here. The real implementations
// live in localtalk.go; an embedded board's own peripheral drivers (see
// hardware/peripherals) are the supported path for this build instead.
package link

import (
	"errors"

	"github.com/ObsoleteMadness/ClassicStack/core/link"
)

var errLocalTalkUnsupported = errors.New("link: LToUDP/TashTalk are not supported on this build")

func openLToUDP(_ string, _ uint16, srcNode uint8, _ bool) (link.DatagramLink, uint8, error) {
	return nil, srcNode, errLocalTalkUnsupported
}

func openTashTalk(_ string, _ uint, _ uint16, srcNode uint8, _ bool) (link.DatagramLink, uint8, error) {
	return nil, srcNode, errLocalTalkUnsupported
}
