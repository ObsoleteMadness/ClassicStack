// Package netlog is ClassicStack's logging API.
//
// It is a thin facade over log/slog: cmd/classicstack constructs a structured
// logger via pkg/logging and installs it here with SetLogger, then every
// service calls Debug/Info/Warn from this package. The facade keeps call
// sites short (no per-package logger plumbing) while still letting the
// process-wide handler decide formatting (console vs JSON) and level.
//
// Use this package for ordinary diagnostic logging. Use pkg/logging
// directly only when you need a *slog.Logger value (e.g. attaching
// structured fields with .With for the lifetime of an object).
package netlog

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"log/slog"
	"strings"
	"sync"

	"github.com/ObsoleteMadness/ClassicStack/protocol/ddp"
)

// Level mirrors the legacy three-value enum but maps onto slog.Level.
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
)

var (
	levelMu  sync.RWMutex
	minLevel = LevelInfo
)

// logger is the slog instance the shim forwards through. It is
// deliberately separate from slog.Default(): netlog.SetLevel needs to
// gate Debug traffic without disturbing whatever handler the application
// has installed as the process-wide default. Callers that want
// structured output install a pkg/logging-built logger here via
// SetLogger; the zero value routes through slog.Default() with our own
// level gate out front.
var (
	loggerMu sync.RWMutex
	logger   *slog.Logger
)

// SetLogger installs the logger that Debug/Info/Warn forward to. Passing
// nil reverts to slog.Default().
func SetLogger(l *slog.Logger) {
	loggerMu.Lock()
	logger = l
	loggerMu.Unlock()
}

func activeLogger() *slog.Logger {
	loggerMu.RLock()
	l := logger
	loggerMu.RUnlock()
	if l != nil {
		return l
	}
	return slog.Default()
}

// SetLevel sets the minimum level. Kept for call-site compatibility; new
// code should configure pkg/logging sinks directly.
func SetLevel(l Level) {
	levelMu.Lock()
	minLevel = l
	levelMu.Unlock()
}

// ParseLevel accepts "debug" / "info" / "warn" / "warning".
func ParseLevel(s string) (Level, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
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

func slogLevel(l Level) slog.Level {
	switch l {
	case LevelDebug:
		return slog.LevelDebug
	case LevelWarn:
		return slog.LevelWarn
	default:
		return slog.LevelInfo
	}
}

// emit forwards to slog.Default(). Callers construct the root logger via
// pkg/logging and install it with logging.SetDefault; this shim simply
// adapts the legacy printf-style API onto slog. The netlog level gate
// remains so callers that call SetLevel(LevelDebug) still see debug lines
// even when slog.Default's handler is at Info — the shim uses
// slog.Log(level), which slog honours regardless of the handler's level
// as long as the handler is enabled at that level.
func emit(l Level, format string, args ...any) {
	if !enabled(l) {
		return
	}
	lg := activeLogger()
	// When no custom logger is installed the shim falls back to stdlib
	// log so the historical format (captured by tests via log.SetOutput)
	// stays intact. As soon as main installs a pkg/logging logger via
	// SetLogger, output shifts to the structured pipeline.
	loggerMu.RLock()
	custom := logger != nil
	loggerMu.RUnlock()
	if !custom {
		var tag string
		switch l {
		case LevelDebug:
			tag = "DEBUG "
		case LevelWarn:
			tag = "WARN  "
		default:
			tag = "INFO  "
		}
		log.Printf(tag+format, args...)
		return
	}
	lg.Log(context.Background(), slogLevel(l), fmt.Sprintf(format, args...))
}

// Debug / Info / Warn are the legacy entry points. They now route through
// slog.Default(); install a pkg/logging-constructed logger as default in
// main and you get structured output with source tags for free.
func Debug(format string, args ...any) { emit(LevelDebug, format, args...) }
func Info(format string, args ...any)  { emit(LevelInfo, format, args...) }
func Warn(format string, args ...any)  { emit(LevelWarn, format, args...) }

// ShortStringer is implemented by ports that provide a short description.
type ShortStringer interface {
	ShortString() string
}

// LogFunc receives a single formatted network traffic log line.
type LogFunc func(string)

// NetLogger logs DDP datagrams and link-layer frames for debug purposes.
type NetLogger struct {
	mu    sync.Mutex
	fn    LogFunc
	dirW  int
	portW int
	hdrW  int
}

// SetLogFunc enables network traffic logging and sets the output function.
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

func (n *NetLogger) LogDatagramInbound(network uint16, node uint8, d ddp.Datagram, p ShortStringer) {
	n.emit(fmt.Sprintf("in to %d.%d", network, node), portName(p), datagramHeader(d), d.Data)
}
func (n *NetLogger) LogDatagramUnicast(network uint16, node uint8, d ddp.Datagram, p ShortStringer) {
	n.emit(fmt.Sprintf("out to %d.%d", network, node), portName(p), datagramHeader(d), d.Data)
}
func (n *NetLogger) LogDatagramBroadcast(d ddp.Datagram, p ShortStringer) {
	n.emit("out broadcast", portName(p), datagramHeader(d), d.Data)
}
func (n *NetLogger) LogDatagramMulticast(zoneName []byte, d ddp.Datagram, p ShortStringer) {
	n.emit(fmt.Sprintf("out to %s", string(zoneName)), portName(p), datagramHeader(d), d.Data)
}
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
func (n *NetLogger) LogLocaltalkFrameInbound(frame []byte, p ShortStringer) {
	if len(frame) < 3 {
		return
	}
	n.emit("frame in", portName(p), localtalkFrameHeader(frame), frame[3:])
}
func (n *NetLogger) LogLocaltalkFrameOutbound(frame []byte, p ShortStringer) {
	if len(frame) < 3 {
		return
	}
	n.emit("frame out", portName(p), localtalkFrameHeader(frame), frame[3:])
}

// Default is the package-level NetLogger instance.
var Default = &NetLogger{}

// SetLogFunc configures the Default NetLogger's output function.
func SetLogFunc(fn LogFunc) { Default.SetLogFunc(fn) }

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
