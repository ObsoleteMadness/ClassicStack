package atlink

import (
	"flag"
	"testing"
)

// TestFlags_Defaults checks the flag defaults Flags binds: ltoudp transport,
// claim on, everything else zero-valued.
func TestFlags_Defaults(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	o := Flags(fs)
	if err := fs.Parse(nil); err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if o.Transport != TransportLToUDP {
		t.Errorf("Transport = %q, want %q", o.Transport, TransportLToUDP)
	}
	if !o.Claim {
		t.Error("Claim = false, want true (the documented default)")
	}
	if o.Iface != "" || o.Device != "" || o.Baud != 0 || o.ListIface {
		t.Errorf("non-transport/claim defaults not zero: %+v", o)
	}
}

// TestFlags_ParsesArgs checks each bound flag lands in the right Options
// field when supplied on the command line.
func TestFlags_ParsesArgs(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	o := Flags(fs)
	args := []string{
		"-transport", TransportTashTalk,
		"-iface", "en0",
		"-device", "/dev/ttyUSB0",
		"-baud", "230400",
		"-list-ifaces",
		"-claim=false",
	}
	if err := fs.Parse(args); err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := Options{
		Transport: TransportTashTalk,
		Iface:     "en0",
		Device:    "/dev/ttyUSB0",
		Baud:      230400,
		ListIface: true,
		Claim:     false,
	}
	if *o != want {
		t.Errorf("got %+v, want %+v", *o, want)
	}
}

// TestOpen_ZeroSrcNodeWithoutClaimErrors checks the documented invariant:
// srcNode == 0 (no explicit candidate) with Claim false is rejected rather
// than silently asserting node 0, which is not a valid LocalTalk address.
func TestOpen_ZeroSrcNodeWithoutClaimErrors(t *testing.T) {
	o := &Options{Transport: TransportLToUDP, Claim: false}
	_, _, err := o.Open(0xFF00, 0)
	if err == nil {
		t.Fatal("Open with srcNode 0 and Claim false: got nil error, want one")
	}
}
