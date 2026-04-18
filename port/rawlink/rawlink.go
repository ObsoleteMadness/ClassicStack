// Package rawlink defines the RawLink interface and optional capability
// extensions for reading and writing raw Ethernet frames. It abstracts the
// underlying packet capture backend (libpcap, TUN/TAP, SLIRP, etc.) so that
// EtherTalk and MacIP can be tested and deployed with alternative backends.
package rawlink

import "errors"

// PhysicalMedium describes the data-link layer technology detected by the
// hardware. It replaces gopacket's layers.LinkType at the interface boundary,
// keeping gopacket imports isolated inside backend implementations.
type PhysicalMedium uint8

const (
	// MediumEthernet covers wired 802.3, virtual Ethernet, TAP devices, and
	// any interface that delivers standard Ethernet frames.
	MediumEthernet PhysicalMedium = iota
	// MediumWiFi covers raw 802.11 interfaces that require radiotap or
	// similar encapsulation (Prism, native 802.11 frame format).
	MediumWiFi
)

// ErrTimeout is returned by ReadFrame when no packet arrived within the
// configured read timeout. Callers should loop on ErrTimeout rather than
// treating it as a fatal error. It replaces pcap.NextErrorTimeoutExpired
// as the sentinel so callers have no pcap dependency.
var ErrTimeout = errors.New("rawlink: read timeout")

// RawLink is the minimal interface for reading and writing raw Ethernet frames
// to a network medium. Implementations must be safe for concurrent use from
// a single reader goroutine and a single writer goroutine simultaneously.
//
// Promiscuous mode, snap length, and read timeout are configured at
// construction time by the implementation, not through this interface.
type RawLink interface {
	// ReadFrame blocks until a raw frame is available or the read deadline
	// expires, then returns the frame bytes. On timeout it returns
	// (nil, ErrTimeout). On an unrecoverable error it returns (nil, err).
	// Callers own the returned slice.
	ReadFrame() ([]byte, error)

	// WriteFrame transmits a raw frame. The implementation must not retain
	// the slice after WriteFrame returns.
	WriteFrame(frame []byte) error

	// Close releases all resources. Subsequent calls to ReadFrame or
	// WriteFrame must return errors. Close is idempotent.
	Close() error
}

// MediumReporter is an optional extension of RawLink for implementations that
// can report the physical medium type detected at link activation. EtherTalk
// probes this interface to select the correct WiFi bridge encapsulation
// strategy without importing gopacket.
type MediumReporter interface {
	// Medium returns the physical layer type detected at link activation.
	Medium() PhysicalMedium
}

// FilterableLink is an optional extension of RawLink for implementations that
// support kernel-level or driver-level packet filtering (e.g. BPF). Both
// EtherTalk and MacIP apply a filter when available; they fall back to software
// filtering in their read loops when this interface is not implemented.
type FilterableLink interface {
	// SetFilter applies a BPF-syntax filter expression. Returns an error if
	// the expression is invalid or unsupported by the backend.
	SetFilter(expr string) error
}
