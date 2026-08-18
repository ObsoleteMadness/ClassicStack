//go:build !pcap && !all

package link

import (
	"errors"

	"github.com/ObsoleteMadness/ClassicStack/core/link"
)

// errNoPcap is returned by the pcap openers in a build without the 'pcap' or
// 'all' tag, so a client asking for a NIC transport fails loudly rather than
// silently.
var errNoPcap = errors.New("link: pcap transport requires the 'pcap' or 'all' build tag")

func openPcapFrame(device, filter, capturePath string, captureSnaplen uint32) (link.FrameLink, error) {
	_ = device
	_ = filter
	_ = capturePath
	_ = captureSnaplen
	return nil, errNoPcap
}

func openPcapDDP(device string, mac [6]byte, capturePath string, captureSnaplen uint32) (link.DatagramLink, error) {
	_ = device
	_ = mac
	_ = capturePath
	_ = captureSnaplen
	return nil, errNoPcap
}

// listPcapDevices reports the missing-tag error in a build without 'pcap', so a
// -list-ifaces run prints an honest "built without the 'pcap' tag" line rather than a
// misleading empty list.
func listPcapDevices() ([]Interface, error) { return nil, errNoPcap }
