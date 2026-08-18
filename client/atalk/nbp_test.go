package atalk

import "testing"

func TestSplitNameZone(t *testing.T) {
	tests := []struct {
		in, obj, zone string
	}{
		{"Mac Classic", "Mac Classic", "*"},
		{"ClassicStack:EtherTalk Network", "ClassicStack", "EtherTalk Network"},
		{"name:zone:with:colons", "name:zone:with", "colons"},
	}
	for _, tc := range tests {
		obj, zone := splitNameZone(tc.in)
		if obj != tc.obj || zone != tc.zone {
			t.Errorf("splitNameZone(%q) = (%q, %q), want (%q, %q)", tc.in, obj, zone, tc.obj, tc.zone)
		}
	}
}

func TestFilterScanZones(t *testing.T) {
	got := filterScanZones([]string{"ZoneA", "*", " zonea ", "", "ZoneB"})
	if len(got) != 2 || got[0] != "ZoneA" || got[1] != "ZoneB" {
		t.Fatalf("filterScanZones = %v, want [ZoneA ZoneB]", got)
	}
}

func TestMergeNBPEntityPrefersNamedZone(t *testing.T) {
	seen := map[string]NBPEntity{}
	wild := NBPEntity{Object: "snow", Type: AFPServerType, Zone: "*", Addr: Addr{Network: 10, Node: 5}}
	named := NBPEntity{Object: "snow", Type: AFPServerType, Zone: "EtherTalk", Addr: Addr{Network: 10, Node: 5}}
	mergeNBPEntity(seen, wild)
	mergeNBPEntity(seen, named)
	ent := seen[nbpDedupKey(named)]
	if ent.Zone != "EtherTalk" {
		t.Fatalf("got zone %q, want EtherTalk", ent.Zone)
	}
}
