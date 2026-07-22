package main

import (
	"strings"
	"testing"

	_ "github.com/ObsoleteMadness/ClassicStack/client/afp" // register "afp" + its transports
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
