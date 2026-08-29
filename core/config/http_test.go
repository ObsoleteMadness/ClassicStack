package config

import "testing"

func TestApplyHTTPDefaultsOmitted(t *testing.T) {
	got := ApplyHTTPDefaults(HTTPSection{}, false, false)
	want := DefaultHTTP()
	if got != want {
		t.Fatalf("omitted [http]: got %+v want %+v", got, want)
	}
}

func TestApplyHTTPDefaultsPresentWithoutEnabled(t *testing.T) {
	got := ApplyHTTPDefaults(HTTPSection{Addr: ":8080"}, true, false)
	if !got.Enabled {
		t.Fatal("present [http] without enabled should default enabled")
	}
	if got.Addr != ":8080" {
		t.Fatalf("addr = %q, want :8080", got.Addr)
	}
}

func TestApplyHTTPDefaultsDisabled(t *testing.T) {
	got := ApplyHTTPDefaults(HTTPSection{Enabled: false, Addr: ":9"}, true, true)
	if got.Enabled {
		t.Fatal("explicit enabled=false must stick")
	}
	if got.Addr != ":9" {
		t.Fatalf("addr = %q, want :9", got.Addr)
	}
}

func TestHTTPListenAddrDefault(t *testing.T) {
	if got := (HTTPSection{}).ListenAddr(); got != DefaultHTTPAddr {
		t.Fatalf("ListenAddr = %q, want %q", got, DefaultHTTPAddr)
	}
}

func TestHTTPValidate(t *testing.T) {
	if err := (HTTPSection{Addr: ":1984"}).Validate(); err != nil {
		t.Fatalf("valid addr: %v", err)
	}
	if err := (HTTPSection{}).Validate(); err != nil {
		t.Fatalf("empty addr (default :1984) should validate: %v", err)
	}
	if err := (HTTPSection{Addr: "not-a-port"}).Validate(); err == nil {
		t.Fatal("invalid listen address should fail Validate")
	}
}
