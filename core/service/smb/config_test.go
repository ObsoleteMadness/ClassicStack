package smb

import (
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/config"
)

func TestShareSectionSpecMapsFields(t *testing.T) {
	ss := &ShareSection{
		SName:         "Public",
		Description:   "the public tree",
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
	spec := ss.Spec()

	if spec.Name != "Public" || spec.Description != "the public tree" {
		t.Fatalf("name/description not mapped: %+v", spec)
	}
	fsSpec := spec.Share
	if fsSpec.FSType != "local_fs" || fsSpec.ForkBackend != "appledouble" || fsSpec.FilenameCodec != "macroman-utf8" {
		t.Fatalf("core fields not mapped: %+v", fsSpec)
	}
	if fsSpec.NameEngine != "long" || fsSpec.Metastore != "mem" || fsSpec.Path != "/srv/public" || !fsSpec.ReadOnly {
		t.Fatalf("engine/metastore/path/readonly not mapped: %+v", fsSpec)
	}
	if len(fsSpec.AllowedUsers) != 2 || fsSpec.AllowedUsers[0] != "alice" {
		t.Fatalf("allowed_users not mapped: %+v", fsSpec.AllowedUsers)
	}
	if got := fsSpec.Extra["url"]; got != "ftp://host" {
		t.Errorf("Extra[url] = %v, want ftp://host", got)
	}
	if got := fsSpec.Extra["partition"]; got != "2" {
		t.Errorf("Extra[partition] = %v, want 2", got)
	}
	if _, ok := fsSpec.Extra["flag"]; !ok {
		t.Errorf("bare flag option should be present as empty Extra value")
	}
}

func TestShareSectionCloneIsDeep(t *testing.T) {
	ss := &ShareSection{SName: "S", AllowedUsers: []string{"a"}, Options: []string{"k=v"}}
	cp := ss.Clone().(*ShareSection)
	cp.AllowedUsers[0] = "X"
	cp.Options[0] = "Y"
	if ss.AllowedUsers[0] != "a" || ss.Options[0] != "k=v" {
		t.Fatal("Clone aliased the original slices")
	}
}

func TestShareSectionValidate(t *testing.T) {
	if err := (&ShareSection{}).Validate(); err == nil {
		t.Fatal("empty name should fail validation")
	}
	if err := (&ShareSection{SName: "ok"}).Validate(); err != nil {
		t.Fatalf("named share should validate: %v", err)
	}
}

func TestSpecsFromModelInOrder(t *testing.T) {
	m := config.NewModel()
	m.AddInstance(&ShareSection{SName: "First", FSType: "memfs"})
	m.AddInstance(&ShareSection{SName: "Second", FSType: "memfs"})

	specs := SpecsFromModel(m)
	if len(specs) != 2 {
		t.Fatalf("got %d specs, want 2", len(specs))
	}
	if specs[0].Name != "First" || specs[1].Name != "Second" {
		t.Fatalf("order not preserved: %q, %q", specs[0].Name, specs[1].Name)
	}
}

func TestNewWithSharesAppliesDescription(t *testing.T) {
	m := config.NewModel()
	m.AddInstance(&ShareSection{
		SName:         "Docs",
		Description:   "shared documents",
		FSType:        "memfs",
		ForkBackend:   "appledouble",
		FilenameCodec: "macroman-utf8",
	})

	svc, err := NewWithShares(nil, SpecsFromModel(m)...)
	if err != nil {
		t.Fatalf("NewWithShares: %v", err)
	}
	sh, ok := svc.ShareByName("Docs")
	if !ok {
		t.Fatal("service did not build the configured share")
	}
	if sh.Description() != "shared documents" {
		t.Errorf("description not applied: %q", sh.Description())
	}
}
