//go:build pcap || all

// Package pcap — see doc.go. The libpcap-backed implementation is gated behind
// the `pcap` or `all` build tags because it requires cgo + libpcap/Npcap at
// build time; builds without those tags get the stub in pcap_stub.go so the tree
// still compiles (and TinyGo/embedded targets never pull in cgo). Ported from
// the legacy port/rawlink/pcap.go.
package pcap

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcap"

	"github.com/ObsoleteMadness/ClassicStack/core/link"
)

// ErrUnavailable mirrors the stub build's sentinel so callers can test for "no pcap
// backend" with errors.Is on EITHER build. In the tagged build Open never returns it
// (libpcap is present) — it exists only to keep the symbol defined so cmd-edge code
// that maps ErrUnavailable → inert compiles identically with or without the tag.
var ErrUnavailable = errors.New("pcap: built without the 'pcap' tag (libpcap/cgo unavailable)")

// Config holds parameters for opening a libpcap handle. Promiscuous mode, snap
// length, and read timeout are fixed at construction; they are not part of the
// core/link.FrameLink contract.
type Config struct {
	Interface     string        // pcap device name to open
	SnapLen       int           // max bytes captured per packet (0 -> 65535)
	Promiscuous   bool          // enable promiscuous capture
	ReadTimeout   time.Duration // libpcap read timeout (-> link.ErrTimeout)
	ImmediateMode bool          // immediate-mode delivery (low latency)
	// Filter is a kernel BPF expression applied at Activate ("" = capture everything).
	// A promiscuous handle sees ALL NIC traffic (and loops back this station's own TX);
	// a per-protocol filter narrows the read loop to the frames the port understands and
	// keeps a wire-capture file clean. Applied best-effort — a rejected expression logs
	// nothing here but leaves the handle unfiltered rather than failing the open.
	Filter string
}

const defaultSnapLen = 65535

// EtherTalkBPFFilter narrows an EtherTalk capture to AppleTalk traffic: DDP over
// 802.2/SNAP (tcpdump's "atalk") plus the AppleTalk ARP used for node-claim/resolution
// ("aarp"). It excludes the IPv4/ARP/etc. background a promiscuous handle would otherwise
// grab — and, crucially, keeps the read loop from re-processing unrelated frames.
const EtherTalkBPFFilter = "atalk or aarp"

// DefaultEtherTalkConfig returns a Config suited to EtherTalk: promiscuous, immediate
// mode, 250ms read timeout — the low-latency shape EtherTalk needs — plus the AppleTalk
// BPF filter so the handle only surfaces DDP + AARP frames.
func DefaultEtherTalkConfig(iface string) Config {
	return Config{
		Interface:     iface,
		SnapLen:       defaultSnapLen,
		Promiscuous:   true,
		ReadTimeout:   250 * time.Millisecond,
		ImmediateMode: true,
		Filter:        EtherTalkBPFFilter,
	}
}

// DefaultMacIPConfig returns a Config suited to MacIP: promiscuous, 100ms read
// timeout, no immediate mode.
func DefaultMacIPConfig(iface string) Config {
	return Config{
		Interface:   iface,
		SnapLen:     defaultSnapLen,
		Promiscuous: true,
		ReadTimeout: 100 * time.Millisecond,
	}
}

// frameLink implements link.FrameLink, link.MediumReporter, and
// link.FilterableLink over a libpcap handle.
type frameLink struct {
	handle *pcap.Handle
	medium link.PhysicalMedium

	// mu guards closed so Close (on any goroutine) cannot free the libpcap
	// handle while a Read/Write/SetFilter call is inside the cgo boundary.
	// libpcap frees the C-side handle in pcap_close; touching it afterwards is
	// a use-after-free (a 0xC0000005 access violation on Windows). The lock is
	// held only around the closed check + the cgo call, never across blocking
	// work, so it does not serialise reads against writes.
	mu     sync.RWMutex
	closed bool
}

// Compile-time interface assertions.
var (
	_ link.FrameLink      = (*frameLink)(nil)
	_ link.MediumReporter = (*frameLink)(nil)
	_ link.FilterableLink = (*frameLink)(nil)
)

// DeviceInfo summarises a discovered pcap device.
type DeviceInfo struct {
	Name        string
	Description string
	Addresses   []string
}

// ListDevices enumerates devices available to libpcap/Npcap.
func ListDevices() ([]DeviceInfo, error) {
	devs, err := pcap.FindAllDevs()
	if err != nil {
		return nil, err
	}
	out := make([]DeviceInfo, 0, len(devs))
	for _, d := range devs {
		info := DeviceInfo{
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

// Open opens a libpcap handle via the inactive-handle API (which supports
// ImmediateMode) and returns it as a core/link.FrameLink. Probe for the optional
// MediumReporter / FilterableLink capabilities with a type assertion.
func Open(cfg Config) (link.FrameLink, error) {
	if cfg.SnapLen == 0 {
		cfg.SnapLen = defaultSnapLen
	}
	inactive, err := pcap.NewInactiveHandle(cfg.Interface)
	if err != nil {
		return nil, fmt.Errorf("pcap: inactive handle on %s: %w%s", cfg.Interface, err, permissionHint(err))
	}
	defer inactive.CleanUp()
	if err := inactive.SetSnapLen(cfg.SnapLen); err != nil {
		return nil, fmt.Errorf("pcap: set snap len: %w", err)
	}
	if err := inactive.SetPromisc(cfg.Promiscuous); err != nil {
		return nil, fmt.Errorf("pcap: set promisc: %w", err)
	}
	if err := inactive.SetTimeout(cfg.ReadTimeout); err != nil {
		return nil, fmt.Errorf("pcap: set timeout: %w", err)
	}
	if cfg.ImmediateMode {
		if err := inactive.SetImmediateMode(true); err != nil {
			return nil, fmt.Errorf("pcap: set immediate mode: %w", err)
		}
	}
	h, err := inactive.Activate()
	if err != nil {
		return nil, fmt.Errorf("pcap: activate %s: %w%s", cfg.Interface, err, permissionHint(err))
	}
	// Apply the kernel BPF filter best-effort: a promiscuous handle otherwise surfaces all
	// NIC traffic (and this station's own looped-back TX). A rejected expression must not
	// fail the open — the read loop still demuxes by SNAP PID / socket — so we leave the
	// handle unfiltered on error rather than propagating it.
	if cfg.Filter != "" {
		_ = h.SetBPFFilter(cfg.Filter)
	}
	return &frameLink{handle: h, medium: linkTypeToMedium(h.LinkType())}, nil
}

// Read returns the next captured frame, mapping a libpcap read timeout to
// link.ErrTimeout (caller loops) and post-Close use to link.ErrClosed.
func (l *frameLink) Read() (link.Frame, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if l.closed {
		return nil, link.ErrClosed
	}
	data, _, err := l.handle.ReadPacketData()
	if err != nil {
		if errors.Is(err, pcap.NextErrorTimeoutExpired) {
			return nil, link.ErrTimeout
		}
		return nil, err
	}
	return data, nil
}

// Write injects a raw frame. It does not retain the slice past the call.
func (l *frameLink) Write(frame link.Frame) error {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if l.closed {
		return link.ErrClosed
	}
	return l.handle.WritePacketData(frame)
}

// Close frees the pcap handle. Idempotent; takes the write lock so it cannot
// free the handle mid-call against a concurrent Read/Write/SetFilter.
func (l *frameLink) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	l.handle.Close()
	return nil
}

// Medium implements link.MediumReporter.
func (l *frameLink) Medium() link.PhysicalMedium { return l.medium }

// SetFilter implements link.FilterableLink, pushing a kernel BPF expression.
func (l *frameLink) SetFilter(expr string) error {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if l.closed {
		return link.ErrClosed
	}
	return l.handle.SetBPFFilter(expr)
}

// linkTypeToMedium maps gopacket LinkType to core/link.PhysicalMedium, keeping
// the gopacket dependency inside this adapter.
func linkTypeToMedium(lt layers.LinkType) link.PhysicalMedium {
	switch lt {
	case layers.LinkTypeIEEE802_11, layers.LinkTypeIEEE80211Radio, layers.LinkTypePrismHeader:
		return link.MediumWiFi
	default:
		return link.MediumEthernet
	}
}

// permissionHint appends a macOS BPF grant hint when libpcap refused the device.
// /dev/bpf* is root-only unless the user is in the access_bpf group (Wireshark's
// ChmodBPF) or the process is running as root.
func permissionHint(err error) string {
	if err == nil || runtime.GOOS != "darwin" {
		return ""
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "permission") ||
		strings.Contains(msg, "operation not permitted") ||
		strings.Contains(msg, "bpf") {
		return "; macOS needs /dev/bpf access (sudo, or install Wireshark ChmodBPF and log out so your user is in access_bpf)"
	}
	return ""
}
