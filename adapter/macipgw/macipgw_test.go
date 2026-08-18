package macipgw

import (
	"io"
	"log/slog"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/service/macip"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestNewNATOnlySkipsPcap(t *testing.T) {
	cfg := Config{
		Interface:  "no-such-pcap-device",
		NATEnabled: true,
		GatewayIP:  "192.168.100.1",
		Network:    "192.168.100.0",
		SubnetMask: "255.255.255.0",
	}
	eg, err := New(cfg, func(macip.IPv4) bool { return false }, testLogger())
	if err != nil {
		t.Fatalf("NAT-only New: %v (must not open pcap)", err)
	}
	defer eg.Close()
	if eg.ether != nil {
		t.Fatal("NAT-only egress opened a pcap link; want ether == nil")
	}
	if eg.osnat == nil {
		t.Fatal("NAT-only egress missing OSNAT forwarder")
	}
	eg.Start() // must not panic on nil ether
}

func TestNewBridgeStillOpensPcap(t *testing.T) {
	cfg := Config{
		Interface:  "no-such-pcap-device",
		HostMAC:    "00:11:22:33:44:55",
		NATEnabled: false,
		GatewayIP:  "192.168.0.50",
		Network:    "192.168.0.0",
		SubnetMask: "255.255.255.0",
	}
	eg, err := New(cfg, func(macip.IPv4) bool { return false }, testLogger())
	if err == nil {
		eg.Close()
		t.Fatal("bridge New succeeded without a pcap device; want open error")
	}
}
