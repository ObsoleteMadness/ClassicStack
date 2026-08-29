package link

import (
	"net"
	"testing"
)

func TestNewOpenerUnknownDeviceLeavesMACZero(t *testing.T) {
	o := NewOpener(Spec{Kind: KindPcap, Name: "dev-does-not-exist-classicstack"})
	if o.MAC != ([6]byte{}) {
		t.Fatalf("MAC = %v, want zero so transports can fall back", o.MAC)
	}
}

func TestNewOpenerHostMAC(t *testing.T) {
	ifi, err := net.InterfaceByName("en0")
	if err != nil || len(ifi.HardwareAddr) != 6 {
		t.Skip("no en0 with a 6-byte MAC")
	}
	opener := NewOpener(Spec{Kind: KindPcap, Name: "en0"})
	var want [6]byte
	copy(want[:], ifi.HardwareAddr)
	if opener.MAC != want {
		t.Fatalf("MAC = %v, want host en0 %v", opener.MAC, want)
	}
}
