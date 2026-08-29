//go:build pcap || all

package link

import (
	"fmt"
	"net"

	"github.com/ObsoleteMadness/ClassicStack/adapter/capture/pcapfile"
	"github.com/ObsoleteMadness/ClassicStack/adapter/link/framing"
	"github.com/ObsoleteMadness/ClassicStack/adapter/link/pcap"
	"github.com/ObsoleteMadness/ClassicStack/core/link"
)

// openPcapFrame opens a NIC via libpcap as a raw FrameLink using the SAME
// adapter/link/pcap the servers use — no capture code is duplicated here, only the
// per-protocol Config. filter is the kernel BPF narrowing the handle to the protocol's
// frames ("" captures everything); EtherDFS/IPX/NetBEUI pass their own filter, so they
// are NOT constrained to the EtherTalk "atalk or aarp" preset. The caller's framer
// deframes what survives.
func openPcapFrame(device, filter, capturePath string, captureSnaplen uint32) (link.FrameLink, error) {
	if device == "" {
		return nil, fmt.Errorf("pcap transport needs a NIC device name")
	}
	cfg := pcap.DefaultEtherTalkConfig(device) // promiscuous + immediate defaults
	cfg.Filter = filter                        // override the EtherTalk-only BPF
	fl, err := pcap.Open(cfg)
	if err != nil {
		return nil, fmt.Errorf("open pcap %s: %w", device, err)
	}
	return maybeCapture(fl, capturePath, pcapfile.LinkTypeEthernet, captureSnaplen), nil
}

// openPcapDDP opens an EtherTalk NIC via libpcap with Ethernet/SNAP DDP framing
// (mirrors atlink.openPcap): the AFP-over-EtherTalk path. It keeps the EtherTalk BPF
// filter so the handle only surfaces DDP + AARP.
func openPcapDDP(device string, mac [6]byte, capturePath string, captureSnaplen uint32) (link.DatagramLink, error) {
	fl, err := openPcapFrame(device, pcap.EtherTalkBPFFilter, capturePath, captureSnaplen)
	if err != nil {
		return nil, err
	}
	src := mac[:]
	if mac == ([6]byte{}) {
		src = interfaceMAC(device)
	}
	framer := &framing.EtherTalk{SrcMAC: src}
	dl, err := framer.Framing(fl)
	if err != nil {
		_ = fl.Close()
		return nil, fmt.Errorf("frame EtherTalk: %w", err)
	}
	return dl, nil
}

// listPcapDevices enumerates the host's libpcap/Npcap devices, mapping the adapter's
// DeviceInfo to the client-ring Interface type. It reuses the SAME adapter/link/pcap
// enumeration the servers' NIC picker uses, so the device names are identical.
func listPcapDevices() ([]Interface, error) {
	devs, err := pcap.ListDevices()
	if err != nil {
		return nil, err
	}
	out := make([]Interface, 0, len(devs))
	for _, d := range devs {
		out = append(out, Interface{
			Name:        d.Name,
			Description: d.Description,
			Addresses:   append([]string(nil), d.Addresses...),
		})
	}
	return out, nil
}

// interfaceMAC resolves the named interface's hardware address, or nil if it cannot
// be resolved (the EtherTalk framer then stamps a zero source MAC).
func interfaceMAC(name string) []byte {
	if ifi, err := net.InterfaceByName(name); err == nil && len(ifi.HardwareAddr) == 6 {
		return append([]byte(nil), ifi.HardwareAddr...)
	}
	return nil
}
