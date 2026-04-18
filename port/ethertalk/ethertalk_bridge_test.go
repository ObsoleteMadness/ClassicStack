package ethertalk

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/pgodw/omnitalk/go/port/rawlink"
)

func TestBridgeAdapterInboundPassThroughCopy(t *testing.T) {
	adapter := newEthertalkBridgeAdapter([]byte{1, 2, 3, 4, 5, 6}, []byte{1, 2, 3, 4, 5, 6}, bridgeModeEthernet)
	input := []byte{0, 1, 2, 3}
	got, err := adapter.inboundFrame(input)
	if err != nil {
		t.Fatalf("inboundFrame returned error: %v", err)
	}
	if !bytes.Equal(got, input) {
		t.Fatalf("inboundFrame = %v, want %v", got, input)
	}
	if len(got) > 0 {
		got[0] = 99
		if input[0] == 99 {
			t.Fatalf("inboundFrame returned aliased buffer")
		}
	}
}

func TestBridgeAdapterOutboundPassThroughCopy(t *testing.T) {
	adapter := newEthertalkBridgeAdapter([]byte{1, 2, 3, 4, 5, 6}, []byte{1, 2, 3, 4, 5, 6}, bridgeModeEthernet)
	input := []byte{10, 11, 12, 13}
	got, err := adapter.outboundFrame(input)
	if err != nil {
		t.Fatalf("outboundFrame returned error: %v", err)
	}
	if !bytes.Equal(got, input) {
		t.Fatalf("outboundFrame = %v, want %v", got, input)
	}
	if len(got) > 0 {
		got[0] = 77
		if input[0] == 77 {
			t.Fatalf("outboundFrame returned aliased buffer")
		}
	}
}

func TestBridgeAdapterRejectsEmptyFrames(t *testing.T) {
	adapter := newEthertalkBridgeAdapter([]byte{1, 2, 3, 4, 5, 6}, []byte{1, 2, 3, 4, 5, 6}, bridgeModeEthernet)
	if _, err := adapter.inboundFrame(nil); err == nil {
		t.Fatalf("inboundFrame should reject empty frames")
	}
	if _, err := adapter.outboundFrame(nil); err == nil {
		t.Fatalf("outboundFrame should reject empty frames")
	}
}

func TestDetectEthertalkBridgeModeFromMedium(t *testing.T) {
	tests := []struct {
		name   string
		medium rawlink.PhysicalMedium
		want   bridgeMode
	}{
		{name: "ethernet", medium: rawlink.MediumEthernet, want: bridgeModeEthernet},
		{name: "wifi", medium: rawlink.MediumWiFi, want: bridgeModeWiFi},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := detectEthertalkBridgeModeFromMedium(tc.medium); got != tc.want {
				t.Fatalf("detectEthertalkBridgeModeFromMedium(%v) = %v, want %v", tc.medium, got, tc.want)
			}
		})
	}
}

func TestParseBridgeModeString(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    bridgeMode
		wantErr bool
	}{
		{name: "default empty", input: "", want: bridgeModeAuto},
		{name: "auto", input: "auto", want: bridgeModeAuto},
		{name: "ethernet", input: "ethernet", want: bridgeModeEthernet},
		{name: "wired alias", input: "wired", want: bridgeModeEthernet},
		{name: "wifi", input: "wifi", want: bridgeModeWiFi},
		{name: "wireless alias", input: "wireless", want: bridgeModeWiFi},
		{name: "invalid", input: "bogus", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseBridgeModeString(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseBridgeModeString(%q) expected error", tc.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseBridgeModeString(%q) returned error: %v", tc.input, err)
			}
			if got != tc.want {
				t.Fatalf("parseBridgeModeString(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestToWiFiFrame_RewritesToHostMAC(t *testing.T) {
	host := []byte{0x10, 0x11, 0x12, 0x13, 0x14, 0x15}
	bssid := []byte{0x20, 0x21, 0x22, 0x23, 0x24, 0x25}
	dst := []byte{0x30, 0x31, 0x32, 0x33, 0x34, 0x35}
	src := []byte{0x40, 0x41, 0x42, 0x43, 0x44, 0x45}
	payload := []byte{0xAA, 0xAA, 0x03, 0x08, 0x00, 0x07, 0x80, 0x9B, 0x01, 0x02}

	eth := make([]byte, 0, 14+len(payload))
	eth = append(eth, dst...)
	eth = append(eth, src...)
	eth = binary.BigEndian.AppendUint16(eth, uint16(len(payload)))
	eth = append(eth, payload...)

	wifi, err := toWiFiFrame(eth, host, bssid)
	if err != nil {
		t.Fatalf("toWiFiFrame returned error: %v", err)
	}
	if len(wifi) < 32 {
		t.Fatalf("wifi frame too short: %d", len(wifi))
	}
	if !bytes.Equal(wifi[18:24], host) {
		t.Fatalf("wifi addr2 = %x, want host %x", wifi[18:24], host)
	}
	if !bytes.Equal(wifi[24:30], dst) {
		t.Fatalf("wifi addr3 = %x, want dst %x", wifi[24:30], dst)
	}
}

func TestWiFiAdapter_RewritesAARPSenderAndReverseDest(t *testing.T) {
	host := []byte{0x10, 0x11, 0x12, 0x13, 0x14, 0x15}
	virtual := []byte{0x40, 0x41, 0x42, 0x43, 0x44, 0x45}
	peer := []byte{0x30, 0x31, 0x32, 0x33, 0x34, 0x35}

	adapter := newEthertalkBridgeAdapter(host, virtual, bridgeModeWiFi)

	payload := make([]byte, 36)
	copy(payload[0:3], ieee8022Type1)
	copy(payload[3:8], snapAARP)
	copy(payload[8:14], aarpValidation)
	binary.BigEndian.PutUint16(payload[14:16], aarpFuncResponse)
	copy(payload[16:22], virtual)
	copy(payload[26:32], peer)

	inbound := make([]byte, 0, 14+len(payload))
	inbound = append(inbound, host...)
	inbound = append(inbound, peer...)
	inbound = binary.BigEndian.AppendUint16(inbound, uint16(len(payload)))
	inbound = append(inbound, payload...)

	in, err := adapter.inboundFrame(inbound)
	if err != nil {
		t.Fatalf("inboundFrame returned error: %v", err)
	}

	if !bytes.Equal(in[0:6], virtual) {
		t.Fatalf("rewritten inbound dst = %x, want virtual %x", in[0:6], virtual)
	}
}

func TestRewriteAARPSenderHardware(t *testing.T) {
	from := []byte{1, 2, 3, 4, 5, 6}
	to := []byte{6, 5, 4, 3, 2, 1}
	payload := make([]byte, 36)
	copy(payload[0:3], ieee8022Type1)
	copy(payload[3:8], snapAARP)
	copy(payload[8:14], aarpValidation)
	copy(payload[16:22], from)

	rewriteAARPSenderHardware(payload, from, to)
	if !bytes.Equal(payload[16:22], to) {
		t.Fatalf("AARP sender hw = %x, want %x", payload[16:22], to)
	}
}

func TestToEthernetFrame_FromRadiotapWiFi(t *testing.T) {
	host := []byte{0x10, 0x11, 0x12, 0x13, 0x14, 0x15}
	bssid := []byte{0x20, 0x21, 0x22, 0x23, 0x24, 0x25}
	dst := []byte{0x30, 0x31, 0x32, 0x33, 0x34, 0x35}
	payload := []byte{0xAA, 0xAA, 0x03, 0x08, 0x00, 0x07, 0x80, 0x9B, 0x99}

	eth := make([]byte, 0, 14+len(payload))
	eth = append(eth, dst...)
	eth = append(eth, []byte{0x40, 0x41, 0x42, 0x43, 0x44, 0x45}...)
	eth = binary.BigEndian.AppendUint16(eth, uint16(len(payload)))
	eth = append(eth, payload...)

	wifi, err := toWiFiFrame(eth, host, bssid)
	if err != nil {
		t.Fatalf("toWiFiFrame returned error: %v", err)
	}

	got, err := toEthernetFrame(wifi)
	if err != nil {
		t.Fatalf("toEthernetFrame returned error: %v", err)
	}

	if !bytes.Equal(got[0:6], dst) {
		t.Fatalf("ethernet dst = %x, want %x", got[0:6], dst)
	}
	if !bytes.Equal(got[6:12], host) {
		t.Fatalf("ethernet src = %x, want rewritten host %x", got[6:12], host)
	}
	if binary.BigEndian.Uint16(got[12:14]) != uint16(len(payload)) {
		t.Fatalf("ethernet length = %d, want %d", binary.BigEndian.Uint16(got[12:14]), len(payload))
	}
	if !bytes.Equal(got[14:], payload) {
		t.Fatalf("ethernet payload mismatch")
	}
}

func TestToEthernetFrame_PassThroughEthernet(t *testing.T) {
	frame := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 0, 2, 0xAA, 0xBB}
	got, err := toEthernetFrame(frame)
	if err != nil {
		t.Fatalf("toEthernetFrame returned error: %v", err)
	}
	if !bytes.Equal(got, frame) {
		t.Fatalf("pass-through mismatch")
	}
	got[0] = 99
	if frame[0] == 99 {
		t.Fatalf("toEthernetFrame returned aliased frame")
	}
}
