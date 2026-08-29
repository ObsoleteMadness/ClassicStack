package link_test

import (
	"fmt"

	"github.com/ObsoleteMadness/ClassicStack/client/link"
)

// ExampleParseSpec parses a "kind:name" transport selector — the form a CLI
// -transport flag threads straight through to NewOpener.
func ExampleParseSpec() {
	spec, err := link.ParseSpec("pcap:en0")
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(spec.Kind, spec.Name)
	// Output: pcap en0
}

// ExampleRandomMAC generates a synthetic station address for a virtual
// client — locally-administered and unicast, per RFC 5342's private-use bit
// convention, so it never collides with a real NIC's burned-in address.
func ExampleRandomMAC() {
	mac := link.RandomMAC()
	locallyAdministered := mac[0]&0x02 != 0
	unicast := mac[0]&0x01 == 0
	fmt.Println(locallyAdministered, unicast)
	// Output: true true
}
