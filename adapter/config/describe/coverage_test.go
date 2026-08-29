package describe_test

import (
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/adapter/config/describe"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/port"
	"github.com/ObsoleteMadness/ClassicStack/core/service/afp"
	"github.com/ObsoleteMadness/ClassicStack/core/service/etherdfs"
	"github.com/ObsoleteMadness/ClassicStack/core/service/ipxgw"
	"github.com/ObsoleteMadness/ClassicStack/core/service/macip"
	"github.com/ObsoleteMadness/ClassicStack/core/service/ncp"
	"github.com/ObsoleteMadness/ClassicStack/core/service/netbios"
	"github.com/ObsoleteMadness/ClassicStack/core/service/netboot"
	"github.com/ObsoleteMadness/ClassicStack/core/service/smb"
)

// TestConfigFieldsAreDocumented enforces the web-UI/self-describing-config contract:
// every operator-facing config field carries a DisplayName, a Description (desc tag), and
// an Example (example tag) so the generated form and server.toml.example are self-
// documenting. bool fields need no example (the checkbox is self-evident); slice fields
// (strings) show a per-line editor and take no single-value example either. A new config
// field added without the tags fails HERE — add display/desc/example to the struct tag.
func TestConfigFieldsAreDocumented(t *testing.T) {
	sections := []struct {
		name string
		s    any
	}{
		{"EtherTalk", &port.EtherTalkSection{}},
		{"IPX", &port.IPXSection{}},
		{"LToUDP", &port.LToUDPSection{}},
		{"TashTalk", &port.TashTalkSection{}},
		{"identity", &config.Identity{}},
		{"http", &config.HTTPSection{}},
		{"Client", &config.ClientSection{}},
		{"FUSE", &config.FUSESection{}},
		{"FUSE.volume", &config.FUSEVolumeSection{}},
		{"MacIP", &macip.Section{}},
		{"IPXGW", &ipxgw.Section{}},
		{"NetBIOS", &netbios.Section{}},
		{"Netboot", &netboot.Section{}},
		{"AFP", &afp.ServerSection{}},
		{"AFP.volume", &afp.VolumeSection{}},
		{"SMB", &smb.ServerSection{}},
		{"SMB.share", &smb.ShareSection{}},
		{"NCP", &ncp.ServerSection{}},
		{"NCP.volume", &ncp.VolumeSection{}},
		{"EtherDFS", &etherdfs.ServerSection{}},
		{"EtherDFS.drive", &etherdfs.DriveSection{}},
	}
	for _, sec := range sections {
		for _, f := range describe.FieldsOf(sec.s) {
			if f.DisplayName == "" {
				t.Errorf("%s.%s: missing display name (add display:\"…\")", sec.name, f.TOML)
			}
			if f.Description == "" {
				t.Errorf("%s.%s: missing description (add desc:\"…\")", sec.name, f.TOML)
			}
			// bool = a self-evident checkbox; strings = a per-line list editor. Neither
			// carries a single scalar example.
			if f.Example == "" && f.Type != "bool" && f.Type != "strings" {
				t.Errorf("%s.%s (%s): missing example (add example:\"…\")", sec.name, f.TOML, f.Type)
			}
		}
	}
}
