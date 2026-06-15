package smb

import (
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/bus"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	"github.com/ObsoleteMadness/ClassicStack/core/metastore"
)

// TestShareSectionSecretMasking mirrors the AFP volume masking test: MaskedClone
// redacts a secret-keyed option, Unmask restores the sentinel from the live section
// and keeps a genuine edit.
func TestShareSectionSecretMasking(t *testing.T) {
	fs.RegisterFSWithParams("test-smb-secret", func(_ fs.ShareSpec, _ bus.Bus, _ metastore.Store) (fs.FileSystem, error) {
		return nil, nil
	},
		fs.Param{Key: "username"},
		fs.Param{Key: "password", Secret: true},
	)

	live := &ShareSection{
		SName:   "Public",
		FSType:  "test-smb-secret",
		Options: []string{"username=alice", "password=hunter2"},
	}

	masked := live.MaskedClone().(*ShareSection)
	if masked.Options[1] != "password="+config.RedactedSecret {
		t.Fatalf("secret option not redacted: %q", masked.Options[1])
	}
	if live.Options[1] != "password=hunter2" {
		t.Fatalf("MaskedClone mutated the receiver: %q", live.Options[1])
	}

	unmasked := masked.Unmask(live).(*ShareSection)
	if unmasked.Options[1] != "password=hunter2" {
		t.Fatalf("Unmask did not restore the stored secret: %q", unmasked.Options[1])
	}

	edited := &ShareSection{SName: "Public", FSType: "test-smb-secret", Options: []string{"password=newpw"}}
	if got := edited.Unmask(live).(*ShareSection).Options[0]; got != "password=newpw" {
		t.Fatalf("Unmask clobbered an edited secret: %q", got)
	}
}

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
