package csconnect

import (
	"strings"
	"testing"

	_ "github.com/ObsoleteMadness/ClassicStack/client/afp" // register "afp" + its transports
	_ "github.com/ObsoleteMadness/ClassicStack/client/smb" // register "smb" + its pcap carriers
	"github.com/ObsoleteMadness/ClassicStack/client/uri"
)

// TestOpenerForValidatesTransport asserts the scheme×ifacetype matrix: AFP accepts
// ltoudp/pcap/tashtalk/tcp and defaults to ltoudp; a transport the scheme does not
// declare is rejected with a clear error.
func TestOpenerForValidatesTransport(t *testing.T) {
	afp, _ := uri.Parse("afp://server/Vol")

	// Default (no -ifacetype) → ltoudp.
	op, err := OpenerFor(Config{}, afp)
	if err != nil {
		t.Fatalf("default afp opener: %v", err)
	}
	if op.Spec.Kind != "ltoudp" {
		t.Errorf("default afp transport = %q, want ltoudp", op.Spec.Kind)
	}

	// Explicit pcap is accepted.
	if _, err := OpenerFor(Config{IfaceType: "pcap"}, afp); err != nil {
		t.Errorf("afp over pcap should be valid: %v", err)
	}

	// tcp is a declared AFP kind so discover URIs (afp://host,tcp/) resolve; DSI
	// itself is not implemented yet (Connect returns that error, not this opener).
	op, err = OpenerFor(Config{IfaceType: "tcp"}, afp)
	if err != nil {
		t.Fatalf("afp over tcp opener should be valid: %v", err)
	}
	if op.Spec.Kind != "tcp" {
		t.Errorf("afp over tcp kind = %q, want tcp", op.Spec.Kind)
	}

	_, err = OpenerFor(Config{IfaceType: "inmem"}, afp)
	if err == nil {
		t.Fatal("afp over inmem should be rejected")
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
	op, err := OpenerFor(Config{IfaceType: "pcap"}, nbf)
	if err != nil {
		t.Fatalf("smb ,nbf opener: %v", err)
	}
	if op.Spec.Kind != "pcap" || op.Spec.Carrier != "nbf" {
		t.Errorf("Kind=%q Carrier=%q, want pcap/nbf", op.Spec.Kind, op.Spec.Carrier)
	}

	// No -ifacetype: kind falls back to the scheme default (pcap), carrier still from URI.
	op, err = OpenerFor(Config{}, nbf)
	if err != nil {
		t.Fatalf("smb ,nbf default-ifacetype opener: %v", err)
	}
	if op.Spec.Kind != "pcap" || op.Spec.Carrier != "nbf" {
		t.Errorf("default-ifacetype Kind=%q Carrier=%q, want pcap/nbf", op.Spec.Kind, op.Spec.Carrier)
	}

	// Explicit -transport wins over the URI tail.
	op, err = OpenerFor(Config{IfaceType: "pcap", Transport: "nbipx"}, nbf)
	if err != nil {
		t.Fatalf("smb -transport override opener: %v", err)
	}
	if op.Spec.Carrier != "nbipx" {
		t.Errorf("Carrier=%q, want nbipx (-transport flag wins over URI tail)", op.Spec.Carrier)
	}

	// A link-kind tail (",tcp") selects the Kind, NOT the carrier.
	tcp, _ := uri.Parse("smb://host,tcp/share")
	op, err = OpenerFor(Config{}, tcp)
	if err != nil {
		t.Fatalf("smb ,tcp opener: %v", err)
	}
	if op.Spec.Kind != "tcp" || op.Spec.Carrier != "" {
		t.Errorf("Kind=%q Carrier=%q, want tcp/empty", op.Spec.Kind, op.Spec.Carrier)
	}
}

// TestParseGlobalFlags checks flags may precede the subcommand and support -f=v and -f v.
func TestParseGlobalFlags(t *testing.T) {
	cfg, rest, err := ParseGlobalFlags([]string{"-ifacetype", "ltoudp", "-iface=192.168.1.5", "ls", "afp://x/y"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.IfaceType != "ltoudp" || cfg.Iface != "192.168.1.5" {
		t.Errorf("cfg = %+v", cfg)
	}
	if len(rest) != 2 || rest[0] != "ls" {
		t.Errorf("rest = %v", rest)
	}
}

// TestParseGlobalFlagsListIfaces checks the boolean -list-ifaces flag sets Config.ListIfaces
// (taking no value, so the next token is NOT consumed) and may sit among the value flags.
func TestParseGlobalFlagsListIfaces(t *testing.T) {
	cfg, rest, err := ParseGlobalFlags([]string{"-ifacetype", "pcap", "-list-ifaces"})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.ListIfaces {
		t.Error("ListIfaces = false, want true")
	}
	if cfg.IfaceType != "pcap" {
		t.Errorf("IfaceType = %q, want pcap", cfg.IfaceType)
	}
	if len(rest) != 0 {
		t.Errorf("rest = %v, want empty (a boolean flag consumes no value)", rest)
	}
}

// TestParseGlobalFlagsCacheMs checks -cache-ms parses signed integers (including -1 for
// WinFsp infinite FileInfoTimeout) and sets CacheMsSet.
func TestParseGlobalFlagsCacheMs(t *testing.T) {
	cfg, rest, err := ParseGlobalFlags([]string{"-cache-ms", "2500", "afp://x/y", "X:"})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.CacheMsSet || cfg.CacheMs != 2500 {
		t.Errorf("CacheMs=%d Set=%v, want 2500/true", cfg.CacheMs, cfg.CacheMsSet)
	}
	if len(rest) != 2 {
		t.Errorf("rest = %v", rest)
	}

	cfg, _, err = ParseGlobalFlags([]string{"-cache-ms=-1", "afp://x/y"})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.CacheMsSet || cfg.CacheMs != -1 {
		t.Errorf("CacheMs=%d Set=%v, want -1/true", cfg.CacheMs, cfg.CacheMsSet)
	}

	cfg, _, err = ParseGlobalFlags([]string{"-cache-ms", "0"})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.CacheMsSet || cfg.CacheMs != 0 {
		t.Errorf("CacheMs=%d Set=%v, want 0/true", cfg.CacheMs, cfg.CacheMsSet)
	}

	cfg, _, err = ParseGlobalFlags([]string{"-v"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CacheMsSet {
		t.Error("CacheMsSet should be false when -cache-ms is omitted")
	}
}
