//go:build !pcap

package link

import (
	"errors"

	"github.com/ObsoleteMadness/ClassicStack/core/link"
)

// errNoPcap is returned by the pcap openers in a build without the 'pcap' tag, so a
// client asking for a NIC transport fails loudly rather than silently.
var errNoPcap = errors.New("link: pcap transport requires the 'pcap' build tag")

func openPcapFrame(device, filter string) (link.FrameLink, error) {
	_ = device
	_ = filter
	return nil, errNoPcap
}

func openPcapDDP(device string, network uint16, srcNode uint8) (link.DatagramLink, error) {
	_ = device
	_ = network
	_ = srcNode
	return nil, errNoPcap
}

// listPcapDevices reports the missing-tag error in a build without 'pcap', so a
// -list-ifaces run prints an honest "built without the 'pcap' tag" line rather than a
// misleading empty list.
func listPcapDevices() ([]Interface, error) { return nil, errNoPcap }
