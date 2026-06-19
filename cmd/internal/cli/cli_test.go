package cli

import (
	"testing"

	configtoml "github.com/ObsoleteMadness/ClassicStack/adapter/config/toml"
	configuci "github.com/ObsoleteMadness/ClassicStack/adapter/config/uci"
)

// TestPickCodec locks the path→codec selection: OpenWRT UCI for an /etc/config path
// or a *.uci file, TOML otherwise — so one binary reads server.toml on a desktop and
// /etc/config/classicstack on a router.
func TestPickCodec(t *testing.T) {
	t.Parallel()
	uci := []string{
		"/etc/config/classicstack",
		"/etc/config/foo",
		`C:\etc\config\classicstack`, // ToSlash normalises the separator
		"settings.uci",
		"/tmp/test.UCI",
		"openwrt/files/classicstack.config",
	}
	for _, p := range uci {
		if _, ok := pickCodec(p).(*configuci.Codec); !ok {
			t.Errorf("pickCodec(%q) = TOML, want UCI", p)
		}
	}
	toml := []string{
		"server.toml",
		"/etc/classicstack/server.toml",
		"config.json",
		"",
	}
	for _, p := range toml {
		if _, ok := pickCodec(p).(*configtoml.Codec); !ok {
			t.Errorf("pickCodec(%q) = UCI, want TOML", p)
		}
	}
}
