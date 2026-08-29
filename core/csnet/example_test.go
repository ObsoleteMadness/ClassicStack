package csnet_test

import (
	"fmt"

	"github.com/ObsoleteMadness/ClassicStack/core/csnet"
)

// ExampleParseMAC parses a station address and formats it back, the shape most
// config/CLI code needs: validate an operator-supplied string, then normalize it
// for display or storage.
func ExampleParseMAC() {
	mac, err := csnet.ParseMAC("00:11:22:aa:bb:cc")
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(csnet.FormatMAC(mac))
	// Output: 00:11:22:AA:BB:CC
}

// ExampleParseIPv4 parses a dotted-quad address, the form ClassicStack's
// AppleTalk-side IP gateways (MacIP, IPX-over-AppleTalk) configure endpoints with.
func ExampleParseIPv4() {
	ip, err := csnet.ParseIPv4("10.0.0.1")
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(ip)
	// Output: 10.0.0.1
}
