package link

import (
	"errors"
	"strings"
	"sync"
	"time"
)

// This file holds the Bridge frame-altitude decorator (§2): Wi-Fi / bridged MAC
// adaptation for shared-L2 consumers (MacIP/IPX/NetBEUI). Ported from the legacy
// port/rawlink/bridge_link.go. Core is reflection-free AND may not import
// encoding/binary (it pulls in reflect, archtest-gated), so the big/little-endian
// helpers are hand-rolled below.

// BridgeMode selects how Bridge adapts frames between the wire and the
// Ethernet form the ports expect.
type bridgeMode uint8

const (
	bridgeAuto     bridgeMode = iota // pick from the link's reported medium
	bridgeEthernet                   // pure pass-through
	bridgeWiFi                       // Ethernet <-> 802.11+radiotap adaptation
)

const bridgePeerMapTTL = 2 * time.Minute

// ErrBridgeBadMAC is returned by BridgeWiFi when a supplied MAC is not 6 bytes.
var ErrBridgeBadMAC = errors.New("link: bridge requires 6-byte MACs")

// ErrBridgeBadMode is returned by Bridge/BridgeWiFi for an unrecognised mode.
var ErrBridgeBadMode = errors.New("link: invalid bridge mode (want auto|ethernet|wifi)")

// Bridge wraps inner with frame-mode adaptation selected by mode
// ("auto"|"ethernet"|"wifi", case-insensitive; empty == auto). Ethernet mode —
// and auto over a non-Wi-Fi medium — is pure pass-through, so Bridge returns
// inner unchanged. Wi-Fi adaptation rewrites MAC identity and therefore needs
// the host/virtual MACs; for that, call BridgeWiFi. Bridge alone never rewrites
// MACs: an unknown mode falls back to pass-through (use BridgeWiFi for the
// erroring, MAC-aware form).
func Bridge(inner FrameLink, mode string) FrameLink {
	m, err := parseBridgeMode(mode)
	if err != nil {
		return inner // lenient: unknown mode is a no-op here
	}
	if resolveBridgeMode(m, inner) != bridgeWiFi {
		return inner
	}
	// Wi-Fi requested but no MACs available through this entry point: the
	// MAC-rewrite path is unsafe without them, so stay pass-through. Callers
	// that want real Wi-Fi adaptation use BridgeWiFi.
	return inner
}

// BridgeWiFi wraps inner with full Wi-Fi bridge adaptation: in resolved Wi-Fi
// mode it converts between Ethernet and 802.11+radiotap frames and rewrites the
// virtual/host MAC identity. hostMAC/virtualMAC must be 6 bytes. Ethernet (or
// auto over a wired medium) returns inner unchanged.
func BridgeWiFi(inner FrameLink, mode string, hostMAC, virtualMAC []byte) (FrameLink, error) {
	m, err := parseBridgeMode(mode)
	if err != nil {
		return nil, err
	}
	if len(hostMAC) != 6 || len(virtualMAC) != 6 {
		return nil, ErrBridgeBadMAC
	}
	resolved := resolveBridgeMode(m, inner)
	if resolved != bridgeWiFi {
		return inner, nil
	}
	medium := MediumEthernet
	if mr, ok := inner.(MediumReporter); ok {
		medium = mr.Medium()
	}
	return &bridgedLink{
		inner:         inner,
		hostMAC:       append([]byte(nil), hostMAC...),
		virtualMAC:    append([]byte(nil), virtualMAC...),
		bssid:         append([]byte(nil), hostMAC...),
		wifiEncap:     medium == MediumWiFi,
		peerToVirtual: make(map[[6]byte]bridgePeerEntry),
	}, nil
}

func parseBridgeMode(s string) (bridgeMode, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "auto":
		return bridgeAuto, nil
	case "ethernet", "wired":
		return bridgeEthernet, nil
	case "wifi", "wireless":
		return bridgeWiFi, nil
	default:
		return bridgeAuto, ErrBridgeBadMode
	}
}

// resolveBridgeMode folds auto into a concrete mode using the link's medium.
func resolveBridgeMode(m bridgeMode, inner FrameLink) bridgeMode {
	if m != bridgeAuto {
		return m
	}
	if mr, ok := inner.(MediumReporter); ok && mr.Medium() == MediumWiFi {
		return bridgeWiFi
	}
	return bridgeEthernet
}

type bridgePeerEntry struct {
	virtual [6]byte
	until   time.Time
}

// bridgedLink adapts 802.11 capture/inject to the Ethernet frames the ports use,
// while presenting a stable virtual MAC to the wire. Only the Wi-Fi path is
// instantiated (Ethernet returns inner directly).
type bridgedLink struct {
	inner      FrameLink
	hostMAC    []byte
	virtualMAC []byte
	bssid      []byte
	wifiEncap  bool

	peerMu        sync.Mutex
	peerToVirtual map[[6]byte]bridgePeerEntry
}

func (l *bridgedLink) Read() (Frame, error) {
	frame, err := l.inner.Read()
	if err != nil {
		return nil, err
	}
	eth, err := bridgeToEthernet(frame)
	if err != nil {
		return nil, err
	}
	if len(eth) < 14 {
		return nil, errors.New("link: bridge ethernet frame too short")
	}
	// Suppress our own injected frames echoed back by the medium.
	if macEqual(eth[6:12], l.hostMAC) || macEqual(eth[6:12], l.virtualMAC) {
		return nil, ErrTimeout
	}
	out := append([]byte(nil), eth...)
	if macEqual(out[0:6], l.hostMAC) {
		virtual := l.lookupVirtual(out[6:12])
		if virtual == nil {
			virtual = l.virtualMAC
		}
		copy(out[0:6], virtual)
	}
	return out, nil
}

func (l *bridgedLink) Write(frame Frame) error {
	if len(frame) < 14 {
		return errors.New("link: bridge ethernet frame too short")
	}
	prepared := append([]byte(nil), frame...)
	virtualSrc := append([]byte(nil), prepared[6:12]...)
	dst := append([]byte(nil), prepared[0:6]...)
	if !macEqual(prepared[6:12], l.hostMAC) {
		copy(prepared[6:12], l.hostMAC)
	}
	if !isBroadcastMAC(dst) && !isMulticastMAC(dst) {
		l.rememberVirtual(dst, virtualSrc)
	}
	if l.wifiEncap {
		wifi, err := bridgeToWiFi(prepared, l.hostMAC, l.bssid)
		if err != nil {
			return err
		}
		prepared = wifi
	}
	return l.inner.Write(prepared)
}

func (l *bridgedLink) Close() error { return l.inner.Close() }

func (l *bridgedLink) Medium() PhysicalMedium {
	if mr, ok := l.inner.(MediumReporter); ok {
		return mr.Medium()
	}
	return MediumEthernet
}

func (l *bridgedLink) SetFilter(expr string) error {
	fl, ok := l.inner.(FilterableLink)
	if !ok {
		return errors.New("link: bridge underlying link does not support filters")
	}
	return fl.SetFilter(expr)
}

func (l *bridgedLink) rememberVirtual(peerMAC, virtualMAC []byte) {
	if len(peerMAC) != 6 || len(virtualMAC) != 6 {
		return
	}
	l.peerMu.Lock()
	l.peerToVirtual[toMACKey(peerMAC)] = bridgePeerEntry{
		virtual: toMACKey(virtualMAC),
		until:   time.Now().Add(bridgePeerMapTTL),
	}
	l.peerMu.Unlock()
}

func (l *bridgedLink) lookupVirtual(peerMAC []byte) []byte {
	if len(peerMAC) != 6 {
		return nil
	}
	key := toMACKey(peerMAC)
	now := time.Now()
	l.peerMu.Lock()
	defer l.peerMu.Unlock()
	entry, ok := l.peerToVirtual[key]
	if !ok {
		return nil
	}
	if now.After(entry.until) {
		delete(l.peerToVirtual, key)
		return nil
	}
	out := make([]byte, 6)
	copy(out, entry.virtual[:])
	return out
}

// --- frame conversion (hand-rolled endianness; no encoding/binary in core) ---

func bridgeToEthernet(frame []byte) ([]byte, error) {
	if len(frame) < 14 {
		return nil, errors.New("link: frame too short")
	}
	if !looksLikeRadiotap(frame) {
		return append([]byte(nil), frame...), nil
	}
	radiotapLen := int(leU16(frame[2:4]))
	if radiotapLen < 8 || radiotapLen >= len(frame) {
		return nil, errors.New("link: invalid radiotap length")
	}
	wifi := frame[radiotapLen:]
	if len(wifi) < 24 {
		return nil, errors.New("link: wifi frame too short")
	}

	fc := leU16(wifi[0:2])
	if (fc>>2)&0x3 != 0x2 { // data frame
		return nil, errors.New("link: not a data frame")
	}

	toDS := (fc & 0x0100) != 0
	fromDS := (fc & 0x0200) != 0
	subtype := (fc >> 4) & 0xF

	headerLen := 24
	if toDS && fromDS {
		headerLen = 30
	}
	if subtype&0x8 != 0 { // QoS data
		headerLen += 2
	}
	if len(wifi) < headerLen {
		return nil, errors.New("link: wifi header too short")
	}

	addr1 := wifi[4:10]
	addr2 := wifi[10:16]
	addr3 := wifi[16:22]

	var dstMAC, srcMAC []byte
	switch {
	case !toDS && !fromDS:
		dstMAC, srcMAC = addr1, addr2
	case toDS && !fromDS:
		dstMAC, srcMAC = addr3, addr2
	case !toDS && fromDS:
		dstMAC, srcMAC = addr1, addr3
	default:
		if len(wifi) < 30 {
			return nil, errors.New("link: wifi WDS header too short")
		}
		dstMAC, srcMAC = addr3, wifi[24:30]
	}

	payload := wifi[headerLen:]
	if len(payload) > 0xFFFF {
		return nil, errors.New("link: wifi payload too large")
	}

	out := make([]byte, 0, 14+len(payload))
	out = append(out, dstMAC...)
	out = append(out, srcMAC...)
	out = appendBEU16(out, uint16(len(payload)))
	out = append(out, payload...)
	return out, nil
}

func bridgeToWiFi(ethernetFrame, hostMAC, bssid []byte) ([]byte, error) {
	if len(ethernetFrame) < 14 {
		return nil, errors.New("link: ethernet frame too short")
	}
	if len(hostMAC) != 6 || len(bssid) != 6 {
		return nil, ErrBridgeBadMAC
	}
	dstMAC := ethernetFrame[0:6]
	payloadLen := int(beU16(ethernetFrame[12:14]))
	if payloadLen < 0 || 14+payloadLen > len(ethernetFrame) {
		return nil, errors.New("link: invalid ethernet payload length")
	}
	payload := ethernetFrame[14 : 14+payloadLen]

	radiotap := []byte{0x00, 0x00, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00}
	wifiHeader := make([]byte, 24)
	putLEU16(wifiHeader[0:2], 0x0108) // FC: data, fromDS
	putLEU16(wifiHeader[2:4], 0)      // duration
	copy(wifiHeader[4:10], bssid)
	copy(wifiHeader[10:16], hostMAC)
	copy(wifiHeader[16:22], dstMAC)
	putLEU16(wifiHeader[22:24], 0) // seq ctl

	out := make([]byte, 0, len(radiotap)+len(wifiHeader)+len(payload))
	out = append(out, radiotap...)
	out = append(out, wifiHeader...)
	out = append(out, payload...)
	return out, nil
}

func looksLikeRadiotap(frame []byte) bool {
	if len(frame) < 8 || frame[0] != 0 {
		return false
	}
	radiotapLen := int(leU16(frame[2:4]))
	return radiotapLen >= 8 && radiotapLen <= len(frame)
}

// --- small byte helpers (stdlib-free endianness) ---

func leU16(b []byte) uint16       { return uint16(b[0]) | uint16(b[1])<<8 }
func beU16(b []byte) uint16       { return uint16(b[0])<<8 | uint16(b[1]) }
func putLEU16(b []byte, v uint16) { b[0] = byte(v); b[1] = byte(v >> 8) }
func appendBEU16(dst []byte, v uint16) []byte {
	return append(dst, byte(v>>8), byte(v))
}

func macEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func toMACKey(mac []byte) [6]byte {
	var out [6]byte
	copy(out[:], mac)
	return out
}

func isBroadcastMAC(mac []byte) bool {
	if len(mac) != 6 {
		return false
	}
	for _, b := range mac {
		if b != 0xFF {
			return false
		}
	}
	return true
}

func isMulticastMAC(mac []byte) bool {
	return len(mac) == 6 && mac[0]&0x01 == 0x01
}
