//go:build macip

package main

import (
	"net"
	"testing"
)

func TestResolveMacIPGatewayIP_PcapModeUsesUpstreamGateway(t *testing.T) {
	_, subnet, err := net.ParseCIDR("10.1.0.0/24")
	if err != nil {
		t.Fatalf("ParseCIDR: %v", err)
	}
	got := resolveMacIPGatewayIP("192.168.100.1", subnet, net.ParseIP("192.168.100.1"), false)
	if got == nil || got.String() != "192.168.100.1" {
		t.Fatalf("resolveMacIPGatewayIP pcap = %v, want 192.168.100.1", got)
	}
}

func TestResolveMacIPGatewayIP_NATModeUsesConfiguredOrSubnetDefault(t *testing.T) {
	_, subnet, err := net.ParseCIDR("10.1.0.0/24")
	if err != nil {
		t.Fatalf("ParseCIDR: %v", err)
	}
	configured := resolveMacIPGatewayIP("10.1.0.1", subnet, net.ParseIP("192.168.1.1"), true)
	if configured == nil || configured.String() != "10.1.0.1" {
		t.Fatalf("resolveMacIPGatewayIP configured = %v, want 10.1.0.1", configured)
	}

	fallback := resolveMacIPGatewayIP("", subnet, net.ParseIP("192.168.1.1"), true)
	if fallback == nil || fallback.String() != "10.1.0.1" {
		t.Fatalf("resolveMacIPGatewayIP fallback = %v, want 10.1.0.1", fallback)
	}
}
