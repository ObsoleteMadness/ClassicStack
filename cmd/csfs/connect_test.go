package main

import (
	"strings"
	"testing"

	_ "github.com/ObsoleteMadness/ClassicStack/client/afp" // register "afp" + its transports
	_ "github.com/ObsoleteMadness/ClassicStack/client/smb" // register "smb" + its pcap carriers
	"github.com/ObsoleteMadness/ClassicStack/client/uri"
)

// TestOpenerForValidatesTransport asserts the scheme×ifacetype matrix: AFP accepts
// ltoudp/pcap/tashtalk and defaults to ltoudp; a transport the scheme does not declare
// is rejected with a clear error.
func TestOpenerForValidatesTransport(t *testing.T) {
	afp, _ := uri.Parse("afp://server/Vol")

	// Default (no -ifacetype) → ltoudp.
	op, err := openerFor(config{}, afp)
	if err != nil {
		t.Fatalf("default afp opener: %v", err)
	}
	if op.Spec.Kind != "ltoudp" {
		t.Errorf("default afp transport = %q, want ltoudp", op.Spec.Kind)
	}

	// Explicit pcap is accepted.
	if _, err := openerFor(config{ifaceType: "pcap"}, afp); err != nil {
		t.Errorf("afp over pcap should be valid: %v", err)
	}

	// tcp is NOT an AFP transport (DSI does not exist) → rejected.
	_, err = openerFor(config{ifaceType: "tcp"}, afp)
	if err == nil {
		t.Fatal("afp over tcp should be rejected")
	}
	if !strings.Contains(err.Error(), "not valid for afp") {
		t.Errorf("error = %q, want a clear scheme-mismatch message", err)
	}
}

// TestOpenerForURICarrier asserts the URI ",<transport>" tail selects the SMB pcap
// carrier (Spec.Carrier) when it is not a link kind, that an explicit -transport flag
// still wins, and that a link-kind tail (",tcp") selects the opener Kind — not a carrier.
func TestOpenerForURICarrier(t *testing.T) {
	// smb://host,nbf/share over pcap: the ",nbf" tail (not a link kind) → Carrier=nbf.
	nbf, _ := uri.Parse("smb://host,nbf/share")
	op, err := openerFor(config{ifaceType: "pcap"}, nbf)
	if err != nil {
		t.Fatalf("smb ,nbf opener: %v", err)
	}
	if op.Spec.Kind != "pcap" || op.Spec.Carrier != "nbf" {
		t.Errorf("Kind=%q Carrier=%q, want pcap/nbf", op.Spec.Kind, op.Spec.Carrier)
	}

	// No -ifacetype: kind falls back to the scheme default (pcap), carrier still from URI.
	op, err = openerFor(config{}, nbf)
	if err != nil {
		t.Fatalf("smb ,nbf default-ifacetype opener: %v", err)
	}
	if op.Spec.Kind != "pcap" || op.Spec.Carrier != "nbf" {
		t.Errorf("default-ifacetype Kind=%q Carrier=%q, want pcap/nbf", op.Spec.Kind, op.Spec.Carrier)
	}

	// Explicit -transport wins over the URI tail.
	op, err = openerFor(config{ifaceType: "pcap", transport: "nbipx"}, nbf)
	if err != nil {
		t.Fatalf("smb -transport override opener: %v", err)
	}
	if op.Spec.Carrier != "nbipx" {
		t.Errorf("Carrier=%q, want nbipx (-transport flag wins over URI tail)", op.Spec.Carrier)
	}

	// A link-kind tail (",tcp") selects the Kind, NOT the carrier.
	tcp, _ := uri.Parse("smb://host,tcp/share")
	op, err = openerFor(config{}, tcp)
	if err != nil {
		t.Fatalf("smb ,tcp opener: %v", err)
	}
	if op.Spec.Kind != "tcp" || op.Spec.Carrier != "" {
		t.Errorf("Kind=%q Carrier=%q, want tcp/empty", op.Spec.Kind, op.Spec.Carrier)
	}
}

// TestParseGlobalFlags checks flags may precede the subcommand and support -f=v and -f v.
func TestParseGlobalFlags(t *testing.T) {
	cfg, rest, err := parseGlobalFlags([]string{"-ifacetype", "ltoudp", "-iface=192.168.1.5", "ls", "afp://x/y"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ifaceType != "ltoudp" || cfg.iface != "192.168.1.5" {
		t.Errorf("cfg = %+v", cfg)
	}
	if len(rest) != 2 || rest[0] != "ls" {
		t.Errorf("rest = %v", rest)
	}
}
