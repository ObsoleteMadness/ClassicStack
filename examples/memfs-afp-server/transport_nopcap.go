//go:build !pcap

package main

import (
	"fmt"

	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/router"
)

// openPcap is the stub used when this program is built without the 'pcap' tag
// (the default): -transport pcap needs libpcap/Npcap linked in, so it fails
// loudly with a rebuild hint rather than silently falling back to LToUDP.
func openPcap(iface string, rtr router.Router, logger log.Logger) (router.RoutedPort, error) {
	_, _, _ = iface, rtr, logger
	return nil, fmt.Errorf("pcap transport requires rebuilding with -tags pcap (e.g. go run -tags pcap ./examples/memfs-afp-server -transport pcap -iface en0)")
}
