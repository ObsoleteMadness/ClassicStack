// Package netlog provides leveled logging and optional network traffic logging,
package netlog

import (
	"encoding/binary"
	"fmt"
	"log"
	"sync"

	"github.com/pgodw/omnitalk/protocol/ddp"
)

// Level controls the minimum severity of messages that are emitted.
type Level int

const (
	LevelDebug Level = iota // log debug, info, and warn
	LevelInfo               // log info and warn (default)
	LevelWarn               // log warn only
)

var (
	levelMu  sync.RWMutex
	minLevel = LevelInfo
)

// SetLevel sets the minimum log level. Messages below this level are suppressed.
// Default is LevelInfo.
func SetLevel(l Level) {
	levelMu.Lock()
	minLevel = l
	levelMu.Unlock()
}

// ParseLevel converts a string ("debug", "info", "warn"/"warning") to a Level.
// Returns (level, true) on success or (LevelInfo, false) if unrecognised.
func ParseLevel(s string) (Level, bool) {
	switch s {
	case "debug":
		return LevelDebug, true
	case "info":
		return LevelInfo, true
	case "warn", "warning":
		return LevelWarn, true
	}
	return LevelInfo, false
}

func enabled(l Level) bool {
	levelMu.RLock()
	ok := l >= minLevel
	levelMu.RUnlock()
	return ok
}

// Debug logs a debug-level message (suppressed unless level is LevelDebug).
func Debug(format string, args ...any) {
	if enabled(LevelDebug) {
		log.Printf("DEBUG "+format, args...)
	}
}

// Info logs an info-level message.
func Info(format string, args ...any) {
	if enabled(LevelInfo) {
		log.Printf("INFO  "+format, args...)
	}
}

// Warn logs a warning-level message.
func Warn(format string, args ...any) {
	if enabled(LevelWarn) {
		log.Printf("WARN  "+format, args...)
	}
}

// ShortStringer is implemented by ports that can provide a short description string.
type ShortStringer interface {
	ShortString() string
}

// LogFunc is a function that receives a single formatted network traffic log line.
// Pass a function such as func(s string) { Debug("%s", s) } to channel traffic
// output through the leveled logger.
type LogFunc func(string)

// NetLogger logs DDP datagrams and Ethernet/LocalTalk frames for debug purposes.
// Logging is disabled (no-op) until SetLogFunc is called with a non-nil function.
type NetLogger struct {
	mu    sync.Mutex
	fn    LogFunc
	dirW  int
	portW int
	hdrW  int
}

// SetLogFunc enables network traffic logging and sets the output function.
// Pass nil to disable. To enable, pass e.g.:
//
//	netlog.SetLogFunc(func(s string) { netlog.Debug("%s", s) })
func (n *NetLogger) SetLogFunc(fn LogFunc) {
	n.mu.Lock()
	n.fn = fn
	n.mu.Unlock()
}

func (n *NetLogger) emit(direction, port, header string, data []byte) {
	n.mu.Lock()
	fn := n.fn
	if len(direction) > n.dirW {
		n.dirW = len(direction)
	}
	if len(port) > n.portW {
		n.portW = len(port)
	}
	if len(header) > n.hdrW {
		n.hdrW = len(header)
	}
	dw, pw, hw := n.dirW, n.portW, n.hdrW
	n.mu.Unlock()
	if fn == nil {
		return
	}
	fn(fmt.Sprintf("%-*s %-*s %-*s %x", dw, direction, pw, port, hw, header, data))
}

func portName(p ShortStringer) string {
	if p == nil {
		return ""
	}
	return p.ShortString()
}

func datagramHeader(d ddp.Datagram) string {
	return fmt.Sprintf("%2d %d.%-3d %d.%-3d %3d %3d %d",
		d.HopCount,
		d.DestinationNetwork, d.DestinationNode,
		d.SourceNetwork, d.SourceNode,
		d.DestinationSocket, d.SourceSocket,
		d.DDPType)
}

func ethernetFrameHeader(frame []byte) string {
	if len(frame) < 12 {
		return ""
	}
	return fmt.Sprintf("%02X%02X%02X%02X%02X%02X %02X%02X%02X%02X%02X%02X",
		frame[0], frame[1], frame[2], frame[3], frame[4], frame[5],
		frame[6], frame[7], frame[8], frame[9], frame[10], frame[11])
}

func localtalkFrameHeader(frame []byte) string {
	if len(frame) < 3 {
		return ""
	}
	return fmt.Sprintf("%3d %3d  type %02X", frame[0], frame[1], frame[2])
}

// LogDatagramInbound logs an inbound DDP datagram.
func (n *NetLogger) LogDatagramInbound(network uint16, node uint8, d ddp.Datagram, p ShortStringer) {
	n.emit(fmt.Sprintf("in to %d.%d", network, node), portName(p), datagramHeader(d), d.Data)
}

// LogDatagramUnicast logs an outbound unicast DDP datagram.
func (n *NetLogger) LogDatagramUnicast(network uint16, node uint8, d ddp.Datagram, p ShortStringer) {
	n.emit(fmt.Sprintf("out to %d.%d", network, node), portName(p), datagramHeader(d), d.Data)
}

// LogDatagramBroadcast logs an outbound broadcast DDP datagram.
func (n *NetLogger) LogDatagramBroadcast(d ddp.Datagram, p ShortStringer) {
	n.emit("out broadcast", portName(p), datagramHeader(d), d.Data)
}

// LogDatagramMulticast logs an outbound multicast DDP datagram.
func (n *NetLogger) LogDatagramMulticast(zoneName []byte, d ddp.Datagram, p ShortStringer) {
	n.emit(fmt.Sprintf("out to %s", string(zoneName)), portName(p), datagramHeader(d), d.Data)
}

// LogEthernetFrameInbound logs an inbound Ethernet frame (payload only).
func (n *NetLogger) LogEthernetFrameInbound(frame []byte, p ShortStringer) {
	if len(frame) < 14 {
		return
	}
	length := int(binary.BigEndian.Uint16(frame[12:14]))
	end := 14 + length
	if end > len(frame) {
		end = len(frame)
	}
	n.emit("frame in", portName(p), ethernetFrameHeader(frame), frame[14:end])
}

// LogEthernetFrameOutbound logs an outbound Ethernet frame (payload only).
func (n *NetLogger) LogEthernetFrameOutbound(frame []byte, p ShortStringer) {
	if len(frame) < 14 {
		return
	}
	length := int(binary.BigEndian.Uint16(frame[12:14]))
	end := 14 + length
	if end > len(frame) {
		end = len(frame)
	}
	n.emit("frame out", portName(p), ethernetFrameHeader(frame), frame[14:end])
}

// LogLocaltalkFrameInbound logs an inbound LocalTalk frame (payload only).
func (n *NetLogger) LogLocaltalkFrameInbound(frame []byte, p ShortStringer) {
	if len(frame) < 3 {
		return
	}
	n.emit("frame in", portName(p), localtalkFrameHeader(frame), frame[3:])
}

// LogLocaltalkFrameOutbound logs an outbound LocalTalk frame (payload only).
func (n *NetLogger) LogLocaltalkFrameOutbound(frame []byte, p ShortStringer) {
	if len(frame) < 3 {
		return
	}
	n.emit("frame out", portName(p), localtalkFrameHeader(frame), frame[3:])
}

// Default is the package-level NetLogger instance.
var Default = &NetLogger{}

// SetLogFunc configures the Default NetLogger's output function.
// Pass nil to disable. Example to enable at debug level:
//
//	netlog.SetLogFunc(func(s string) { netlog.Debug("%s", s) })
func SetLogFunc(fn LogFunc) { Default.SetLogFunc(fn) }

// Package-level convenience wrappers around Default.

func LogDatagramInbound(network uint16, node uint8, d ddp.Datagram, p ShortStringer) {
	Default.LogDatagramInbound(network, node, d, p)
}
func LogDatagramUnicast(network uint16, node uint8, d ddp.Datagram, p ShortStringer) {
	Default.LogDatagramUnicast(network, node, d, p)
}
func LogDatagramBroadcast(d ddp.Datagram, p ShortStringer) {
	Default.LogDatagramBroadcast(d, p)
}
func LogDatagramMulticast(zoneName []byte, d ddp.Datagram, p ShortStringer) {
	Default.LogDatagramMulticast(zoneName, d, p)
}
func LogEthernetFrameInbound(frame []byte, p ShortStringer) {
	Default.LogEthernetFrameInbound(frame, p)
}
func LogEthernetFrameOutbound(frame []byte, p ShortStringer) {
	Default.LogEthernetFrameOutbound(frame, p)
}
func LogLocaltalkFrameInbound(frame []byte, p ShortStringer) {
	Default.LogLocaltalkFrameInbound(frame, p)
}
func LogLocaltalkFrameOutbound(frame []byte, p ShortStringer) {
	Default.LogLocaltalkFrameOutbound(frame, p)
}
