package diagflags

import (
	"bytes"
	"flag"
	"strings"
	"testing"
)

func newFlagSet() *flag.FlagSet {
	return flag.NewFlagSet("test", flag.ContinueOnError)
}

func TestRegisterCommonDefaults(t *testing.T) {
	fs := newFlagSet()
	c := RegisterCommon(fs)
	if err := fs.Parse(nil); err != nil {
		t.Fatal(err)
	}
	if *c.Verbose {
		t.Error("Verbose defaulted true")
	}
	if *c.Version {
		t.Error("Version defaulted true")
	}
}

func TestCommonHandleVersion(t *testing.T) {
	fs := newFlagSet()
	c := RegisterCommon(fs)
	if err := fs.Parse([]string{"-version"}); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if !c.HandleVersion(&buf, "csfoo", "1.2.3", "abc123", "2026-08-30") {
		t.Fatal("HandleVersion returned false with -version set")
	}
	out := buf.String()
	if !strings.Contains(out, "csfoo") || !strings.Contains(out, "1.2.3") {
		t.Errorf("HandleVersion output missing tool/version: %q", out)
	}
}

func TestCommonHandleVersionAbsent(t *testing.T) {
	fs := newFlagSet()
	c := RegisterCommon(fs)
	if err := fs.Parse(nil); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if c.HandleVersion(&buf, "csfoo", "1.2.3", "abc123", "2026-08-30") {
		t.Fatal("HandleVersion returned true without -version")
	}
	if buf.Len() != 0 {
		t.Errorf("HandleVersion wrote output without -version: %q", buf.String())
	}
}

func TestRegisterLLAPSourceDefaults(t *testing.T) {
	fs := newFlagSet()
	s := RegisterLLAPSource(fs)
	if err := fs.Parse(nil); err != nil {
		t.Fatal(err)
	}
	if *s.Network != 0 || *s.SrcNode != 0 {
		t.Errorf("defaults: net=%d src=%d, want 0/0", *s.Network, *s.SrcNode)
	}
	if err := s.Validate(); err != nil {
		t.Errorf("Validate() on default src=0: %v", err)
	}
}

func TestLLAPSourceValidate(t *testing.T) {
	for _, tc := range []struct {
		src     string
		wantErr bool
	}{
		{"0", false},
		{"1", false},
		{"127", false},
		{"254", false},
		{"255", true},
		{"1000", true},
	} {
		fs := newFlagSet()
		s := RegisterLLAPSource(fs)
		if err := fs.Parse([]string{"-src", tc.src}); err != nil {
			t.Fatalf("-src %s: parse: %v", tc.src, err)
		}
		err := s.Validate()
		if (err != nil) != tc.wantErr {
			t.Errorf("-src %s: Validate() err=%v, wantErr=%v", tc.src, err, tc.wantErr)
		}
	}
}

func TestRegisterIface(t *testing.T) {
	fs := newFlagSet()
	iface := RegisterIface(fs, "network interface to send on")
	if err := fs.Parse([]string{"-iface", "eth0"}); err != nil {
		t.Fatal(err)
	}
	if *iface != "eth0" {
		t.Errorf("iface = %q, want eth0", *iface)
	}
}

func TestRegisterIfaceType(t *testing.T) {
	fs := newFlagSet()
	ifaceType := RegisterIfaceType(fs)
	if err := fs.Parse(nil); err != nil {
		t.Fatal(err)
	}
	if *ifaceType != "pcap" {
		t.Errorf("ifacetype default = %q, want pcap", *ifaceType)
	}
}

func TestRegisterMAC(t *testing.T) {
	fs := newFlagSet()
	mac := RegisterMAC(fs)
	if err := fs.Parse([]string{"-mac", "aa:bb:cc:dd:ee:ff"}); err != nil {
		t.Fatal(err)
	}
	if *mac != "aa:bb:cc:dd:ee:ff" {
		t.Errorf("mac = %q, want aa:bb:cc:dd:ee:ff", *mac)
	}
}

func TestRegisterListIfaces(t *testing.T) {
	fs := newFlagSet()
	listIf := RegisterListIfaces(fs)
	if err := fs.Parse([]string{"-list-ifaces"}); err != nil {
		t.Fatal(err)
	}
	if !*listIf {
		t.Error("list-ifaces not set after -list-ifaces")
	}
}
