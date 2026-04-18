package rawlink

import (
	"fmt"
	"time"

	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcap"
)

// PcapConfig holds parameters for opening a libpcap handle.
type PcapConfig struct {
	Interface     string        // Interface is the pcap device name to open.
	SnapLen       int           // SnapLen is the maximum number of bytes to capture per packet.
	Promiscuous   bool          // Promiscuous enables promiscuous capture mode when true.
	ReadTimeout   time.Duration // ReadTimeout sets the libpcap read timeout for packet reads.
	ImmediateMode bool          // ImmediateMode enables immediate-mode packet delivery when true.
}

// DefaultEtherTalkConfig returns a PcapConfig suitable for EtherTalk:
// promiscuous, immediate mode, 250ms read timeout.
func DefaultEtherTalkConfig(iface string) PcapConfig {
	return PcapConfig{
		Interface:     iface,
		SnapLen:       65535,
		Promiscuous:   true,
		ReadTimeout:   250 * time.Millisecond,
		ImmediateMode: true,
	}
}

// DefaultMacIPConfig returns a PcapConfig suitable for MacIP:
// promiscuous, 100ms read timeout, no immediate mode required.
func DefaultMacIPConfig(iface string) PcapConfig {
	return PcapConfig{
		Interface:     iface,
		SnapLen:       65535,
		Promiscuous:   true,
		ReadTimeout:   100 * time.Millisecond,
		ImmediateMode: false,
	}
}

// pcapLink implements RawLink, MediumReporter, and FilterableLink using libpcap.
type pcapLink struct {
	handle *pcap.Handle   // handle is the underlying libpcap handle used for I/O.
	medium PhysicalMedium // medium reports the detected physical medium for the handle.
}

// PcapDeviceInfo summarizes a discovered pcap device.
type PcapDeviceInfo struct {
	Name        string   // Name is the pcap device name.
	Description string   // Description contains a human-readable description of the device.
	Addresses   []string // Addresses lists IP addresses associated with the device.
}

// ListPcapDevices enumerates devices available to libpcap/Npcap.
func ListPcapDevices() ([]PcapDeviceInfo, error) {
	devs, err := pcap.FindAllDevs()
	if err != nil {
		return nil, err
	}
	out := make([]PcapDeviceInfo, 0, len(devs))
	for _, d := range devs {
		info := PcapDeviceInfo{
			Name:        d.Name,
			Description: d.Description,
			Addresses:   make([]string, 0, len(d.Addresses)),
		}
		for _, a := range d.Addresses {
			if a.IP == nil {
				continue
			}
			info.Addresses = append(info.Addresses, a.IP.String())
		}
		out = append(out, info)
	}
	return out, nil
}

// InterfaceNames returns pcap device names in discovery order.
func InterfaceNames() ([]string, error) {
	devs, err := pcap.FindAllDevs()
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(devs))
	for _, d := range devs {
		out = append(out, d.Name)
	}
	return out, nil
}

// OpenPcap opens a libpcap handle using the inactive handle API, which
// supports ImmediateMode. The returned value also satisfies MediumReporter
// and FilterableLink; probe with a type assertion before using those.
func OpenPcap(cfg PcapConfig) (RawLink, error) {
	inactive, err := pcap.NewInactiveHandle(cfg.Interface)
	if err != nil {
		return nil, fmt.Errorf("rawlink: pcap inactive handle on %s: %w", cfg.Interface, err)
	}
	defer inactive.CleanUp()
	if err := inactive.SetSnapLen(cfg.SnapLen); err != nil {
		return nil, fmt.Errorf("rawlink: set snap len: %w", err)
	}
	if err := inactive.SetPromisc(cfg.Promiscuous); err != nil {
		return nil, fmt.Errorf("rawlink: set promisc: %w", err)
	}
	if err := inactive.SetTimeout(cfg.ReadTimeout); err != nil {
		return nil, fmt.Errorf("rawlink: set timeout: %w", err)
	}
	if cfg.ImmediateMode {
		if err := inactive.SetImmediateMode(true); err != nil {
			return nil, fmt.Errorf("rawlink: set immediate mode: %w", err)
		}
	}
	h, err := inactive.Activate()
	if err != nil {
		return nil, fmt.Errorf("rawlink: activate %s: %w", cfg.Interface, err)
	}
	return &pcapLink{
		handle: h,
		medium: linkTypeToMedium(h.LinkType()),
	}, nil
}

// OpenPcapSimple opens a libpcap handle using pcap.OpenLive (single-call
// variant). Suitable when ImmediateMode is not required (e.g. MacIP).
// The returned value also satisfies MediumReporter and FilterableLink.
func OpenPcapSimple(iface string, snapLen int, promisc bool, timeout time.Duration) (RawLink, error) {
	h, err := pcap.OpenLive(iface, int32(snapLen), promisc, timeout)
	if err != nil {
		return nil, fmt.Errorf("rawlink: pcap open %s: %w", iface, err)
	}
	return &pcapLink{
		handle: h,
		medium: linkTypeToMedium(h.LinkType()),
	}, nil
}

// ReadFrame reads the next raw packet from the pcap handle.
// It returns ErrTimeout when the underlying libpcap read times out.
func (l *pcapLink) ReadFrame() ([]byte, error) {
	data, _, err := l.handle.ReadPacketData()
	if err != nil {
		if err == pcap.NextErrorTimeoutExpired {
			return nil, ErrTimeout
		}
		return nil, err
	}
	return data, nil
}

// WriteFrame writes a raw packet to the link via the pcap handle.
func (l *pcapLink) WriteFrame(frame []byte) error {
	return l.handle.WritePacketData(frame)
}

// Close closes the underlying pcap handle and releases resources.
func (l *pcapLink) Close() error {
	l.handle.Close()
	return nil
}

// Medium implements MediumReporter.
func (l *pcapLink) Medium() PhysicalMedium { return l.medium }

// SetFilter implements FilterableLink.
func (l *pcapLink) SetFilter(expr string) error {
	return l.handle.SetBPFFilter(expr)
}

// linkTypeToMedium maps gopacket LinkType values to the project-local
// PhysicalMedium enum. Keeping this mapping here isolates the gopacket
// dependency inside the pcap implementation file.
func linkTypeToMedium(lt layers.LinkType) PhysicalMedium {
	switch lt {
	case layers.LinkTypeIEEE802_11, layers.LinkTypeIEEE80211Radio, layers.LinkTypePrismHeader:
		return MediumWiFi
	default:
		return MediumEthernet
	}
}
