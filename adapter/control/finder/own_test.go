package finder

import (
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/service/afp"
	"github.com/ObsoleteMadness/ClassicStack/core/service/etherdfs"
	"github.com/ObsoleteMadness/ClassicStack/core/service/ncp"
	"github.com/ObsoleteMadness/ClassicStack/core/service/smb"
)

func TestRememberKeepsNeighborsWithoutModel(t *testing.T) {
	svc := New(nil, nil)
	svc.remember(KindAFP, []VolumeInfo{
		{ID: "afp://ClassicStack,pcap/", Kind: KindAFP, Title: "ClassicStack"},
	})
	if got := svc.LastSeen(KindAFP); len(got) != 1 || got[0].Title != "ClassicStack" {
		t.Fatalf("got %+v, want ClassicStack kept when this instance has no model", got)
	}
}

func TestRememberHidesOwnServers(t *testing.T) {
	m := config.NewModel()
	m.Identity.Hostname = "classicstack"
	m.Set(&afp.ServerSection{AKey: afp.ServerKey, Enabled: true, ServerName: "ClassicStack"})
	m.Set(&smb.ServerSection{SKey: smb.ServerKey, Enabled: true})
	m.Set(&ncp.ServerSection{SKey: ncp.ServerKey, Enabled: true, ServerName: "Netware"})
	m.Set(&etherdfs.ServerSection{SKey: etherdfs.ServerKey, IsEnabled: true, ServerName: "CLASSICSTACK", MAC: "36:14:41:06:43:70"})
	svc := New(modelStub{m: m}, nil)

	svc.remember(KindAFP, []VolumeInfo{
		{ID: "afp://ClassicStack,pcap/", Kind: KindAFP, Title: "ClassicStack"},
		{ID: "afp://Mac,ltoudp/", Kind: KindAFP, Title: "Mac"},
	})
	afpSeen := svc.LastSeen(KindAFP)
	if len(afpSeen) != 1 || afpSeen[0].Title != "Mac" {
		t.Fatalf("afp = %+v, want only Mac", afpSeen)
	}

	svc.remember(KindSMB, []VolumeInfo{
		{ID: "smb://CLASSICSTACK,nbf/", Kind: KindSMB, Title: "CLASSICSTACK"},
		{ID: "smb://FILE,nbipx/", Kind: KindSMB, Title: "FILE"},
	})
	smbSeen := svc.LastSeen(KindSMB)
	if len(smbSeen) != 1 || smbSeen[0].Title != "FILE" {
		t.Fatalf("smb = %+v, want only FILE", smbSeen)
	}

	svc.remember(KindNCP, []VolumeInfo{
		{ID: "ncp://NETWARE/SYS", Kind: KindNCP, Title: "NETWARE"},
		{ID: "ncp://NW311/SYS", Kind: KindNCP, Title: "NW311"},
	})
	ncpSeen := svc.LastSeen(KindNCP)
	if len(ncpSeen) != 1 || ncpSeen[0].Title != "NW311" {
		t.Fatalf("ncp = %+v, want only NW311", ncpSeen)
	}

	svc.remember(KindEtherDFS, []VolumeInfo{
		{ID: "etherdfs://36:14:41:06:43:70/C", Kind: KindEtherDFS, Title: "CLASSICSTACK", Address: "36:14:41:06:43:70"},
		{ID: "etherdfs://aa:bb:cc:dd:ee:ff/C", Kind: KindEtherDFS, Title: "DOSBOX", Address: "aa:bb:cc:dd:ee:ff"},
	})
	edfs := svc.LastSeen(KindEtherDFS)
	if len(edfs) != 1 || edfs[0].Title != "DOSBOX" {
		t.Fatalf("etherdfs = %+v, want only DOSBOX", edfs)
	}
}

func TestRememberHidesEtherDFSByStationMAC(t *testing.T) {
	m := config.NewModel()
	m.Set(&etherdfs.ServerSection{SKey: etherdfs.ServerKey, IsEnabled: true, MAC: "36:14:41:06:43:70"})
	svc := New(modelStub{m: m}, nil)
	svc.remember(KindEtherDFS, []VolumeInfo{
		{ID: "etherdfs://36:14:41:06:43:70/C", Kind: KindEtherDFS, Title: "36:14:41:06:43:70", Address: "36:14:41:06:43:70"},
	})
	if got := svc.LastSeen(KindEtherDFS); len(got) != 0 {
		t.Fatalf("got %+v, want hidden by station MAC", got)
	}
}

func TestRememberKeepsOwnNameWhenServiceDisabled(t *testing.T) {
	m := config.NewModel()
	m.Identity.Hostname = "classicstack"
	m.Set(&afp.ServerSection{AKey: afp.ServerKey, Enabled: false, ServerName: "ClassicStack"})
	svc := New(modelStub{m: m}, nil)
	svc.remember(KindAFP, []VolumeInfo{
		{ID: "afp://ClassicStack,pcap/", Kind: KindAFP, Title: "ClassicStack"},
	})
	if got := svc.LastSeen(KindAFP); len(got) != 1 {
		t.Fatalf("got %+v, want ClassicStack kept when AFP is disabled", got)
	}
}

func TestAddressHasMAC(t *testing.T) {
	if !addressHasMAC("00000003:36:14:41:06:43:70", "36:14:41:06:43:70") {
		t.Fatal("NCP SAP address should match station MAC")
	}
	if addressHasMAC("00000010:01:02:03:04:05:06", "36:14:41:06:43:70") {
		t.Fatal("foreign NCP address must not match")
	}
}
