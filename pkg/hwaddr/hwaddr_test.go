package hwaddr

import (
	"testing"
)

func TestParseEthernetRoundTrip(t *testing.T) {
	t.Parallel()
	cases := []string{"de:ad:be:ef:ca:fe", "DE-AD-BE-EF-CA-FE", "deadbeefcafe"}
	want := Ethernet{0xde, 0xad, 0xbe, 0xef, 0xca, 0xfe}
	for _, s := range cases {
		got, err := ParseEthernet(s)
		if err != nil {
			t.Fatalf("ParseEthernet(%q): %v", s, err)
		}
		if got != want {
			t.Errorf("ParseEthernet(%q) = %v, want %v", s, got, want)
		}
	}
	if got := want.String(); got != "de:ad:be:ef:ca:fe" {
		t.Errorf("Ethernet.String = %q", got)
	}
}

func TestParseEthernetErrors(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"", "zz:zz:zz:zz:zz:zz", "de:ad:be:ef"} {
		if _, err := ParseEthernet(s); err == nil {
			t.Errorf("ParseEthernet(%q) expected error", s)
		}
	}
}

func TestLocalTalkParse(t *testing.T) {
	t.Parallel()
	cases := map[string]LocalTalk{"0xFE": 0xFE, "0x01": 1, "128": 128, "254": 254}
	for in, want := range cases {
		got, err := ParseLocalTalk(in)
		if err != nil {
			t.Fatalf("ParseLocalTalk(%q): %v", in, err)
		}
		if got != want {
			t.Errorf("ParseLocalTalk(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestLocalTalkValidity(t *testing.T) {
	t.Parallel()
	if LocalTalk(0).Valid() || LocalTalk(0xFF).Valid() {
		t.Error("reserved ids should be invalid")
	}
	if !LocalTalk(1).Valid() || !LocalTalk(200).Valid() {
		t.Error("unicast ids should be valid")
	}
	if !LocalTalk(200).IsServerRange() || LocalTalk(50).IsServerRange() {
		t.Error("IsServerRange boundary wrong")
	}
}

func TestAppleTalkEthernetRoundTrip(t *testing.T) {
	t.Parallel()
	oui := MacIPOUI
	for n := 0; n < 0x10000; n += 257 {
		for _, node := range []uint8{1, 42, 0x80, 0xFD, 0xFE} {
			a := AppleTalk{Network: uint16(n), Node: node}
			e := EthernetFromAppleTalk(oui, a)
			got, ok := AppleTalkFromEthernet(oui, e)
			if !ok || got != a {
				t.Fatalf("round-trip failed for %+v: got %+v ok=%v", a, got, ok)
			}
		}
	}
}

func TestAppleTalkFromEthernetRejectsWrongOUI(t *testing.T) {
	t.Parallel()
	e := EthernetFromAppleTalk(MacIPOUI, AppleTalk{Network: 1, Node: 2})
	if _, ok := AppleTalkFromEthernet(AppleOUI, e); ok {
		t.Error("expected mismatched OUI to return ok=false")
	}
}

func TestGenerateLocalTalkPreferredFirst(t *testing.T) {
	t.Parallel()
	preferred := []LocalTalk{200, 201, 0xFF, 200} // 0xFF invalid, dup ignored
	out := GenerateLocalTalk(preferred, nil)
	if out[0] != 200 || out[1] != 201 {
		t.Errorf("expected preferred ids first, got %v", out[:2])
	}
	if len(out) != 254 {
		t.Errorf("expected 254 candidate ids, got %d", len(out))
	}
	seen := map[LocalTalk]bool{}
	for _, id := range out {
		if !id.Valid() {
			t.Errorf("generated id %v is invalid", id)
		}
		if seen[id] {
			t.Errorf("duplicate id %v", id)
		}
		seen[id] = true
	}
}
