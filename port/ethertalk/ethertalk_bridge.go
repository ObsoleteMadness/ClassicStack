package ethertalk

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/pgodw/omnitalk/go/port/rawlink"
)

type bridgeMode uint8

const (
	bridgeModeAuto bridgeMode = iota
	bridgeModeEthernet
	bridgeModeWiFi
)

func (m bridgeMode) String() string {
	switch m {
	case bridgeModeAuto:
		return "auto"
	case bridgeModeEthernet:
		return "ethernet"
	case bridgeModeWiFi:
		return "wifi"
	default:
		return "unknown"
	}
}

func parseBridgeModeString(value string) (bridgeMode, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "auto":
		return bridgeModeAuto, nil
	case "ethernet", "wired":
		return bridgeModeEthernet, nil
	case "wifi", "wireless":
		return bridgeModeWiFi, nil
	default:
		return bridgeModeAuto, fmt.Errorf("invalid bridge mode %q (expected auto, ethernet, or wifi)", value)
	}
}

func detectEthertalkBridgeModeFromMedium(medium rawlink.PhysicalMedium) bridgeMode {
	if medium == rawlink.MediumWiFi {
		return bridgeModeWiFi
	}
	return bridgeModeEthernet
}

func bridgeModeRequiresWiFiEncapsulation(medium rawlink.PhysicalMedium) bool {
	return medium == rawlink.MediumWiFi
}

type bridgeFrameAdapter interface {
	inboundFrame(frame []byte) ([]byte, error)
	outboundFrame(frame []byte) ([]byte, error)
}

type ethernetBridgeAdapter struct {
	hostMAC []byte
}

type wifiBridgeAdapter struct {
	hostMAC    []byte
	virtualMAC []byte
	bssid      []byte
	wifiEncap  bool

	mu            sync.Mutex
	peerToVirtual map[[6]byte]peerMapEntry
}

type peerMapEntry struct {
	virtual [6]byte
	until   time.Time
}

const peerMapTTL = 2 * time.Minute

func newEthertalkBridgeAdapter(hostMAC, virtualMAC []byte, mode bridgeMode) bridgeFrameAdapter {
	return newEthertalkBridgeAdapterWithWiFiEncap(hostMAC, virtualMAC, mode, true)
}

func newEthertalkBridgeAdapterWithWiFiEncap(hostMAC, virtualMAC []byte, mode bridgeMode, wifiEncap bool) bridgeFrameAdapter {
	hw := append([]byte(nil), hostMAC...)
	vw := append([]byte(nil), virtualMAC...)
	if mode == bridgeModeWiFi {
		return &wifiBridgeAdapter{
			hostMAC:       hw,
			virtualMAC:    vw,
			bssid:         append([]byte(nil), hw...),
			wifiEncap:     wifiEncap,
			peerToVirtual: make(map[[6]byte]peerMapEntry),
		}
	}
	return &ethernetBridgeAdapter{hostMAC: hw}
}

func (b *ethernetBridgeAdapter) inboundFrame(frame []byte) ([]byte, error) {
	if len(frame) == 0 {
		return nil, fmt.Errorf("empty inbound frame")
	}
	// Phase 1 behavior: pass through as-is.
	return append([]byte(nil), frame...), nil
}

func (b *ethernetBridgeAdapter) outboundFrame(frame []byte) ([]byte, error) {
	if len(frame) == 0 {
		return nil, fmt.Errorf("empty outbound frame")
	}
	// Phase 1 behavior: pass through as-is.
	return append([]byte(nil), frame...), nil
}

func (b *wifiBridgeAdapter) inboundFrame(frame []byte) ([]byte, error) {
	if len(frame) == 0 {
		return nil, fmt.Errorf("empty inbound frame")
	}
	ethernetFrame, err := toEthernetFrame(frame)
	if err != nil {
		return nil, err
	}
	if len(ethernetFrame) < 14 {
		return nil, fmt.Errorf("ethernet frame too short")
	}

	out := append([]byte(nil), ethernetFrame...)
	if len(b.hostMAC) != 6 || len(b.virtualMAC) != 6 {
		return nil, fmt.Errorf("invalid host or virtual mac")
	}

	// Reverse destination rewrite so the EtherTalk port still sees frames
	// addressed to its virtual MAC identity.
	if bytes.Equal(out[0:6], b.hostMAC) {
		virtual := b.lookupVirtualForPeer(out[6:12])
		if virtual == nil {
			virtual = b.virtualMAC
		}
		copy(out[0:6], virtual)
		rewriteAARPTargetHardware(out[14:], b.hostMAC, virtual)
	}
	return out, nil
}

func (b *wifiBridgeAdapter) outboundFrame(frame []byte) ([]byte, error) {
	if len(frame) == 0 {
		return nil, fmt.Errorf("empty outbound frame")
	}
	if len(frame) < 14 {
		return nil, fmt.Errorf("ethernet frame too short")
	}
	if len(b.hostMAC) != 6 {
		return nil, fmt.Errorf("invalid host mac")
	}

	src := append([]byte(nil), frame[6:12]...)
	dst := append([]byte(nil), frame[0:6]...)

	if !bytes.Equal(src, b.hostMAC) {
		copy(frame[6:12], b.hostMAC)
		rewriteAARPSenderHardware(frame[14:], src, b.hostMAC)
	}

	if !isBroadcastMAC(dst) && !isMulticastMAC(dst) {
		b.rememberPeerVirtual(dst, src)
	}
	if !b.wifiEncap {
		return append([]byte(nil), frame...), nil
	}

	return toWiFiFrame(frame, b.hostMAC, b.bssid)
}

func toEthernetFrame(frame []byte) ([]byte, error) {
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

func toWiFiFrame(ethernetFrame []byte, hostMAC, bssid []byte) ([]byte, error) {
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

	// Minimal radiotap header: version=0, pad=0, len=8, present=0.
	radiotap := []byte{0x00, 0x00, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00}

	// Data frame with ToDS set. On air source/transmitter is always hostMAC in
	// Wi-Fi bridge shim mode, while destination is preserved in Address3.
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

func rewriteAARPSenderHardware(payload []byte, fromMAC, toMAC []byte) {
	if len(fromMAC) != 6 || len(toMAC) != 6 || !isAARPPayload(payload) {
		return
	}
	if bytes.Equal(payload[16:22], fromMAC) {
		copy(payload[16:22], toMAC)
	}
}

func rewriteAARPTargetHardware(payload []byte, fromMAC, toMAC []byte) {
	if len(fromMAC) != 6 || len(toMAC) != 6 || !isAARPPayload(payload) {
		return
	}
	if bytes.Equal(payload[26:32], fromMAC) {
		copy(payload[26:32], toMAC)
	}
}

func isAARPPayload(payload []byte) bool {
	if len(payload) < 36 {
		return false
	}
	if !bytes.Equal(payload[0:3], ieee8022Type1) {
		return false
	}
	if !bytes.Equal(payload[3:8], snapAARP) {
		return false
	}
	return bytes.Equal(payload[8:14], aarpValidation)
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

func toMACKey(mac []byte) [6]byte {
	var key [6]byte
	copy(key[:], mac)
	return key
}

func (b *wifiBridgeAdapter) rememberPeerVirtual(peerMAC, virtualMAC []byte) {
	if len(peerMAC) != 6 || len(virtualMAC) != 6 {
		return
	}
	b.mu.Lock()
	b.peerToVirtual[toMACKey(peerMAC)] = peerMapEntry{virtual: toMACKey(virtualMAC), until: time.Now().Add(peerMapTTL)}
	b.mu.Unlock()
}

func (b *wifiBridgeAdapter) lookupVirtualForPeer(peerMAC []byte) []byte {
	if len(peerMAC) != 6 {
		return nil
	}
	key := toMACKey(peerMAC)
	now := time.Now()
	b.mu.Lock()
	defer b.mu.Unlock()
	entry, ok := b.peerToVirtual[key]
	if !ok {
		return nil
	}
	if now.After(entry.until) {
		delete(b.peerToVirtual, key)
		return nil
	}
	out := make([]byte, 6)
	copy(out, entry.virtual[:])
	return out
}
