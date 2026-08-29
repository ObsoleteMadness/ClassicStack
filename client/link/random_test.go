package link

import "testing"

// Full RandomMAC coverage lives in core/csnet (csnet_test.go); this just proves
// the wrapper actually delegates.
func TestRandomMAC(t *testing.T) {
	mac := RandomMAC()
	if mac[0]&0x02 == 0 {
		t.Errorf("RandomMAC() first octet %02x: locally-administered bit (0x02) not set", mac[0])
	}
	if mac[0]&0x01 != 0 {
		t.Errorf("RandomMAC() first octet %02x: unicast bit (0x01) should be clear", mac[0])
	}
}
