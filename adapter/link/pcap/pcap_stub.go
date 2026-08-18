//go:build !pcap && !all

// This is the no-pcap stub of the pcap link adapter. It is selected whenever
// neither the `pcap` nor `all` build tag is present, so the default build (and
// any cgo-free or TinyGo target) compiles without libpcap/Npcap or gopacket
// linked. Every entry point returns ErrUnavailable rather than capturing real
// frames.
//
// To get the real libpcap-backed link, build with `-tags pcap` or `-tags all`
// (and have libpcap/Npcap + a cgo toolchain available).
package pcap

import (
	"errors"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/link"
)

// ErrUnavailable is returned by every entry point when the binary was built
// without the `pcap` tag.
var ErrUnavailable = errors.New("pcap: built without the 'pcap' tag (libpcap/cgo unavailable)")

// Config mirrors the real adapter's Config so callers compile identically
// regardless of the build tag.
type Config struct {
	Interface     string
	SnapLen       int
	Promiscuous   bool
	ReadTimeout   time.Duration
	ImmediateMode bool
	Filter        string
}

// EtherTalkBPFFilter mirrors the tagged build's AppleTalk (DDP + AARP) capture filter.
const EtherTalkBPFFilter = "atalk or aarp"

// DefaultEtherTalkConfig mirrors the tagged build's constructor.
func DefaultEtherTalkConfig(iface string) Config {
	return Config{Interface: iface, SnapLen: 65535, Promiscuous: true, ReadTimeout: 250 * time.Millisecond, ImmediateMode: true, Filter: EtherTalkBPFFilter}
}

// DefaultMacIPConfig mirrors the tagged build's constructor.
func DefaultMacIPConfig(iface string) Config {
	return Config{Interface: iface, SnapLen: 65535, Promiscuous: true, ReadTimeout: 100 * time.Millisecond}
}

// DeviceInfo mirrors the tagged build's type.
type DeviceInfo struct {
	Name        string
	Description string
	Addresses   []string
}

// ListDevices always fails in the stub build.
func ListDevices() ([]DeviceInfo, error) { return nil, ErrUnavailable }

// Open always fails in the stub build.
func Open(cfg Config) (link.FrameLink, error) { return nil, ErrUnavailable }
