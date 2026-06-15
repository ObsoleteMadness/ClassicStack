package afp

import (
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/config"
)

func TestVolumeSectionSpecMapsFields(t *testing.T) {
	vs := &VolumeSection{
		VName:         "Public",
		FSType:        "local_fs",
		ForkBackend:   "appledouble",
		FilenameCodec: "macroman-utf8",
		NameEngine:    "long",
		Metastore:     "mem",
		Path:          "/srv/public",
		ReadOnly:      true,
		AllowedUsers:  []string{"alice", "bob"},
		Options:       []string{"url=ftp://host", "flag", "partition=2"},
	}
	spec := vs.Spec()

	if spec.Name != "Public" || spec.FSType != "local_fs" || spec.ForkBackend != "appledouble" {
		t.Fatalf("core fields not mapped: %+v", spec)
	}
	if spec.FilenameCodec != "macroman-utf8" || spec.NameEngine != "long" || spec.Metastore != "mem" {
		t.Fatalf("codec/engine/metastore not mapped: %+v", spec)
	}
	if spec.Path != "/srv/public" || !spec.ReadOnly {
		t.Fatalf("path/readonly not mapped: %+v", spec)
	}
	if len(spec.AllowedUsers) != 2 || spec.AllowedUsers[0] != "alice" || spec.AllowedUsers[1] != "bob" {
		t.Fatalf("allowed_users not mapped: %+v", spec.AllowedUsers)
	}
	// Options "key=value" entries become Extra; a bare flag (no '=') is dropped (an
	// Option with no key contributes nothing).
	if got := spec.Extra["url"]; got != "ftp://host" {
		t.Errorf("Extra[url] = %v, want ftp://host", got)
	}
	if got := spec.Extra["partition"]; got != "2" {
		t.Errorf("Extra[partition] = %v, want 2", got)
	}
	if _, ok := spec.Extra["flag"]; !ok {
		t.Errorf("bare flag option should be present as empty Extra value")
	}
}

func TestVolumeSectionCloneIsDeep(t *testing.T) {
	vs := &VolumeSection{VName: "V", AllowedUsers: []string{"a"}, Options: []string{"k=v"}}
	cp := vs.Clone().(*VolumeSection)
	cp.AllowedUsers[0] = "X"
	cp.Options[0] = "Y"
	if vs.AllowedUsers[0] != "a" || vs.Options[0] != "k=v" {
		t.Fatal("Clone aliased the original slices")
	}
}

func TestVolumeSectionValidate(t *testing.T) {
	if err := (&VolumeSection{}).Validate(); err == nil {
		t.Fatal("empty name should fail validation")
	}
	if err := (&VolumeSection{VName: "ok"}).Validate(); err != nil {
		t.Fatalf("named volume should validate: %v", err)
	}
}

func TestSpecsFromModelInOrder(t *testing.T) {
	m := config.NewModel()
	m.AddInstance(&VolumeSection{VName: "First", FSType: "memfs"})
	m.AddInstance(&VolumeSection{VName: "Second", FSType: "memfs"})

	specs := SpecsFromModel(m)
	if len(specs) != 2 {
		t.Fatalf("got %d specs, want 2", len(specs))
	}
	if specs[0].Name != "First" || specs[1].Name != "Second" {
		t.Fatalf("order not preserved: %q, %q", specs[0].Name, specs[1].Name)
	}
}

func TestAddInstanceReplacesSameName(t *testing.T) {
	m := config.NewModel()
	m.AddInstance(&VolumeSection{VName: "V", Path: "/a"})
	m.AddInstance(&VolumeSection{VName: "V", Path: "/b"})
	list := m.List(VolumesKey)
	if len(list) != 1 {
		t.Fatalf("same-name AddInstance should replace, got %d instances", len(list))
	}
	if list[0].(*VolumeSection).Path != "/b" {
		t.Fatalf("replacement did not win: %+v", list[0])
	}
}

func TestNewWithVolumesFromMappedSpecs(t *testing.T) {
	m := config.NewModel()
	m.AddInstance(&VolumeSection{VName: "Media", FSType: "memfs", ForkBackend: "appledouble", FilenameCodec: "macroman-utf8"})

	specs := SpecsFromModel(m)
	var volSpecs []VolumeSpec
	for i, s := range specs {
		volSpecs = append(volSpecs, VolumeSpec{ID: uint16(i + 1), Name: s.Name, Share: s})
	}
	svc, err := NewWithVolumes(nil, volSpecs...)
	if err != nil {
		t.Fatalf("NewWithVolumes: %v", err)
	}
	vols := svc.Volumes()
	if len(vols) != 1 || vols[0].Name() != "Media" {
		t.Fatalf("service did not build the configured volume: %+v", vols)
	}
}
