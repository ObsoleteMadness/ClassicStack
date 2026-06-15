package afp

import (
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/bus"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	"github.com/ObsoleteMadness/ClassicStack/core/metastore"
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

// TestVolumeSectionSecretMasking covers the SecretMasker round-trip: MaskedClone
// redacts a secret-keyed option and leaves a plain one; Unmask restores a sentinel
// from the live section and keeps a genuine edit. The fs_type declares "password"
// Secret so the section can consult its schema.
func TestVolumeSectionSecretMasking(t *testing.T) {
	fs.RegisterFSWithParams("test-afp-secret", func(_ fs.ShareSpec, _ bus.Bus, _ metastore.Store) (fs.FileSystem, error) {
		return nil, nil
	},
		fs.Param{Key: "username"},
		fs.Param{Key: "password", Secret: true},
	)

	live := &VolumeSection{
		VName:   "V",
		FSType:  "test-afp-secret",
		Options: []string{"username=alice", "password=hunter2"},
	}

	masked := live.MaskedClone().(*VolumeSection)
	if masked.Options[0] != "username=alice" {
		t.Fatalf("plain option masked: %q", masked.Options[0])
	}
	if masked.Options[1] != "password="+config.RedactedSecret {
		t.Fatalf("secret option not redacted: %q", masked.Options[1])
	}
	// Masking must not disturb the live section.
	if live.Options[1] != "password=hunter2" {
		t.Fatalf("MaskedClone mutated the receiver: %q", live.Options[1])
	}

	// Blind round-trip: the UI returns the masked options unchanged → secret restored.
	unmasked := masked.Unmask(live).(*VolumeSection)
	if unmasked.Options[1] != "password=hunter2" {
		t.Fatalf("Unmask did not restore the stored secret: %q", unmasked.Options[1])
	}

	// A genuine edit is kept verbatim.
	edited := &VolumeSection{VName: "V", FSType: "test-afp-secret", Options: []string{"password=newpw"}}
	if got := edited.Unmask(live).(*VolumeSection).Options[0]; got != "password=newpw" {
		t.Fatalf("Unmask clobbered an edited secret: %q", got)
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
