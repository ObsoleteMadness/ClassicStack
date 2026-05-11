package rawlink

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
	"sync"
	"time"
)

type bridgeFrameMode uint8

const (
	bridgeFrameModeAuto bridgeFrameMode = iota
	bridgeFrameModeEthernet
	bridgeFrameModeWiFi
)

const bridgePeerMapTTL = 2 * time.Minute

type bridgePeerEntry struct {
	virtual [6]byte
	until   time.Time
}

// BridgeLinkOptions controls shared L2 bridge adaptation for rawlink
// consumers (MacIP/IPX/NetBEUI). EtherTalk keeps its own adapter so it can
// additionally rewrite AARP hardware fields.
type BridgeLinkOptions struct {
	Mode       string
	HostMAC    []byte
	VirtualMAC []byte
}

type bridgedLink struct {
	inner         RawLink
	hostMAC       []byte
	virtualMAC    []byte
	bssid         []byte
	mode          bridgeFrameMode
	wifiEncap     bool
	peerMu        sync.Mutex
	peerToVirtual map[[6]byte]bridgePeerEntry
}

// WrapWithBridgeMode decorates a rawlink with shared frame-mode adaptation.
// "ethernet" is pass-through, while "wifi" performs MAC identity adaptation.
// In wifi mode, if the medium is native Wi-Fi, frames are converted between
// Ethernet and 802.11+radiotap form.
func WrapWithBridgeMode(link RawLink, opts BridgeLinkOptions) (RawLink, error) {
	mode, err := parseBridgeFrameMode(opts.Mode)
	if err != nil {
		return nil, err
	}
	if mode == bridgeFrameModeEthernet {
		return link, nil
	}
	if len(opts.HostMAC) != 6 {
		return nil, fmt.Errorf("rawlink bridge adapter requires 6-byte host MAC")
	}
	if len(opts.VirtualMAC) != 6 {
		return nil, fmt.Errorf("rawlink bridge adapter requires 6-byte virtual MAC")
	}
	resolvedMode := mode
	medium := MediumEthernet
	if mr, ok := link.(MediumReporter); ok {
		medium = mr.Medium()
	}
	if resolvedMode == bridgeFrameModeAuto {
		if medium == MediumWiFi {
			resolvedMode = bridgeFrameModeWiFi
		} else {
			resolvedMode = bridgeFrameModeEthernet
		}
	}
	if resolvedMode == bridgeFrameModeEthernet {
		return link, nil
	}

	return &bridgedLink{
		inner:         link,
		hostMAC:       append([]byte(nil), opts.HostMAC...),
		virtualMAC:    append([]byte(nil), opts.VirtualMAC...),
		bssid:         append([]byte(nil), opts.HostMAC...),
		mode:          resolvedMode,
		wifiEncap:     medium == MediumWiFi,
		peerToVirtual: make(map[[6]byte]bridgePeerEntry),
	}, nil
}

func parseBridgeFrameMode(s string) (bridgeFrameMode, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "auto":
		return bridgeFrameModeAuto, nil
	case "ethernet", "wired":
		return bridgeFrameModeEthernet, nil
	case "wifi", "wireless":
		return bridgeFrameModeWiFi, nil
	default:
		return bridgeFrameModeAuto, fmt.Errorf("invalid bridge frame mode %q (expected auto, ethernet, or wifi)", s)
	}
}

func (l *bridgedLink) ReadFrame() ([]byte, error) {
	frame, err := l.inner.ReadFrame()
	if err != nil {
		return nil, err
	}
	if l.mode != bridgeFrameModeWiFi {
		return frame, nil
	}
	eth, err := bridgeToEthernet(frame)
	if err != nil {
		return nil, err
	}
	if len(eth) < 14 {
		return nil, fmt.Errorf("ethernet frame too short")
	}
	if bytes.Equal(eth[6:12], l.hostMAC) || bytes.Equal(eth[6:12], l.virtualMAC) {
		return nil, ErrTimeout
	}
	out := append([]byte(nil), eth...)
	if bytes.Equal(out[0:6], l.hostMAC) {
		virtual := l.lookupVirtual(out[6:12])
		if virtual == nil {
			virtual = l.virtualMAC
		}
		copy(out[0:6], virtual)
	}
	return out, nil
}

func (l *bridgedLink) WriteFrame(frame []byte) error {
	if l.mode != bridgeFrameModeWiFi {
		return l.inner.WriteFrame(frame)
	}
	if len(frame) < 14 {
		return fmt.Errorf("ethernet frame too short")
	}
	prepared := append([]byte(nil), frame...)
	virtualSrc := append([]byte(nil), prepared[6:12]...)
	dst := append([]byte(nil), prepared[0:6]...)
	if !bytes.Equal(prepared[6:12], l.hostMAC) {
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
	return l.inner.WriteFrame(prepared)
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
		return fmt.Errorf("rawlink bridge adapter: underlying link does not support filters")
	}
	return fl.SetFilter(expr)
}

func (l *bridgedLink) rememberVirtual(peerMAC, virtualMAC []byte) {
	if len(peerMAC) != 6 || len(virtualMAC) != 6 {
		return
	}
	key := toMACKey(peerMAC)
	val := toMACKey(virtualMAC)
	l.peerMu.Lock()
	l.peerToVirtual[key] = bridgePeerEntry{virtual: val, until: time.Now().Add(bridgePeerMapTTL)}
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

func bridgeToEthernet(frame []byte) ([]byte, error) {
	if len(frame) < 14 {
		return nil, fmt.Errorf("frame too short")
	}
	if !looksLikeRadiotap(frame) {
		return append([]byte(nil), frame...), nil
	}
	radiotapLen := int(binary.LittleEndian.Uint16(frame[2:4]))
	if radiotapLen < 8 || radiotapLen >= len(frame) {
		return nil, fmt.Errorf("invalid radiotap length")
	}
	wifi := frame[radiotapLen:]
	if len(wifi) < 24 {
		return nil, fmt.Errorf("wifi frame too short")
	}

	fc := binary.LittleEndian.Uint16(wifi[0:2])
	typeBits := (fc >> 2) & 0x3
	if typeBits != 0x2 {
		return nil, fmt.Errorf("not a data frame")
	}

	toDS := (fc & 0x0100) != 0
	fromDS := (fc & 0x0200) != 0
	subtype := (fc >> 4) & 0xF

	headerLen := 24
	if toDS && fromDS {
		headerLen = 30
	}
	if subtype&0x8 != 0 {
		headerLen += 2
	}
	if len(wifi) < headerLen {
		return nil, fmt.Errorf("wifi header too short")
	}

	addr1 := wifi[4:10]
	addr2 := wifi[10:16]
	addr3 := wifi[16:22]

	var dstMAC []byte
	var srcMAC []byte
	if !toDS && !fromDS {
		dstMAC = addr1
		srcMAC = addr2
	} else if toDS && !fromDS {
		dstMAC = addr3
		srcMAC = addr2
	} else if !toDS && fromDS {
		dstMAC = addr1
		srcMAC = addr3
	} else {
		if len(wifi) < 30 {
			return nil, fmt.Errorf("wifi WDS header too short")
		}
		dstMAC = addr3
		srcMAC = wifi[24:30]
	}

	payload := wifi[headerLen:]
	if len(payload) > 0xFFFF {
		return nil, fmt.Errorf("wifi payload too large")
	}

	out := make([]byte, 0, 14+len(payload))
	out = append(out, dstMAC...)
	out = append(out, srcMAC...)
	out = binary.BigEndian.AppendUint16(out, uint16(len(payload)))
	out = append(out, payload...)
	return out, nil
}

func bridgeToWiFi(ethernetFrame []byte, hostMAC, bssid []byte) ([]byte, error) {
	if len(ethernetFrame) < 14 {
		return nil, fmt.Errorf("ethernet frame too short")
	}
	if len(hostMAC) != 6 || len(bssid) != 6 {
		return nil, fmt.Errorf("invalid host or bssid mac")
	}
	dstMAC := ethernetFrame[0:6]
	payloadLen := int(binary.BigEndian.Uint16(ethernetFrame[12:14]))
	if payloadLen < 0 || 14+payloadLen > len(ethernetFrame) {
		return nil, fmt.Errorf("invalid ethernet payload length")
	}
	payload := ethernetFrame[14 : 14+payloadLen]

	radiotap := []byte{0x00, 0x00, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00}
	wifiHeader := make([]byte, 24)
	binary.LittleEndian.PutUint16(wifiHeader[0:2], 0x0108)
	binary.LittleEndian.PutUint16(wifiHeader[2:4], 0)
	copy(wifiHeader[4:10], bssid)
	copy(wifiHeader[10:16], hostMAC)
	copy(wifiHeader[16:22], dstMAC)
	binary.LittleEndian.PutUint16(wifiHeader[22:24], 0)

	out := make([]byte, 0, len(radiotap)+len(wifiHeader)+len(payload))
	out = append(out, radiotap...)
	out = append(out, wifiHeader...)
	out = append(out, payload...)
	return out, nil
}

func looksLikeRadiotap(frame []byte) bool {
	if len(frame) < 8 {
		return false
	}
	if frame[0] != 0 {
		return false
	}
	radiotapLen := int(binary.LittleEndian.Uint16(frame[2:4]))
	return radiotapLen >= 8 && radiotapLen <= len(frame)
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
	if len(mac) != 6 {
		return false
	}
	return mac[0]&0x01 == 0x01
}
