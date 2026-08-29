package router

import (
	"testing"
)

func TestAddAndQueryZone(t *testing.T) {
	z := NewZoneInformationTable()
	nmax := uint16(12)
	if err := z.AddNetworksToZone([]byte("Engineering"), 10, &nmax); err != nil {
		t.Fatalf("AddNetworksToZone: %v", err)
	}
	zones, err := z.ZonesInNetworkRange(10, nil)
	if err != nil {
		t.Fatalf("ZonesInNetworkRange: %v", err)
	}
	if len(zones) != 1 || string(zones[0]) != "Engineering" {
		t.Errorf("zones = %v, want [Engineering]", zones)
	}
	nets := z.NetworksInZone([]byte("engineering")) // case-insensitive
	if len(nets) != 3 {
		t.Errorf("NetworksInZone(case-folded) = %v, want 3 networks (10-12)", nets)
	}
}

func TestDefaultZoneIsFirst(t *testing.T) {
	z := NewZoneInformationTable()
	nmax := uint16(10)
	_ = z.AddNetworksToZone([]byte("Alpha"), 10, &nmax)
	_ = z.AddNetworksToZone([]byte("Beta"), 10, &nmax)
	zones, _ := z.ZonesInNetworkRange(10, nil)
	if len(zones) != 2 {
		t.Fatalf("zones = %v, want 2", zones)
	}
	if string(zones[0]) != "Alpha" {
		t.Errorf("default (first) zone = %q, want Alpha", zones[0])
	}
}

func TestRemoveNetworksForgetsZone(t *testing.T) {
	z := NewZoneInformationTable()
	nmax := uint16(10)
	_ = z.AddNetworksToZone([]byte("Solo"), 10, &nmax)
	if err := z.RemoveNetworks(10, &nmax); err != nil {
		t.Fatalf("RemoveNetworks: %v", err)
	}
	if got := z.Zones(); len(got) != 0 {
		t.Errorf("zone survived removal of its only network: %v", got)
	}
	if nets := z.NetworksInZone([]byte("Solo")); nets != nil {
		t.Errorf("removed zone still resolves to networks: %v", nets)
	}
}

func TestOverlappingRangeRejected(t *testing.T) {
	z := NewZoneInformationTable()
	nmax := uint16(20)
	if err := z.AddNetworksToZone([]byte("A"), 10, &nmax); err != nil {
		t.Fatalf("first add: %v", err)
	}
	overlap := uint16(25)
	if err := z.AddNetworksToZone([]byte("B"), 15, &overlap); err == nil {
		t.Errorf("overlapping range should be rejected")
	}
}
