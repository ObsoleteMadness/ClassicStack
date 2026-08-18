package config

import (
	"testing"
	"time"
)

func TestFUSEMountTimeoutDefault(t *testing.T) {
	if got := (FUSESection{}).MountTimeout(); got != 30*time.Second {
		t.Fatalf("zero FUSE MountTimeout = %s, want 30s", got)
	}
	if got := (FUSESection{MountTimeoutSeconds: 5}).MountTimeout(); got != 5*time.Second {
		t.Fatalf("MountTimeout = %s, want 5s", got)
	}
}

func TestFUSEValidate(t *testing.T) {
	if err := (FUSESection{MountTimeoutSeconds: -1}).Validate(); err == nil {
		t.Fatal("negative mount_timeout_seconds should fail")
	}
	if err := (FUSESection{MountTimeoutSeconds: 0}).Validate(); err != nil {
		t.Fatalf("zero timeout should be valid: %v", err)
	}
}

func TestApplyFUSEDefaults(t *testing.T) {
	got := ApplyFUSEDefaults(FUSESection{}, false)
	if got != DefaultFUSE() {
		t.Fatalf("omitted [FUSE]: got %+v want %+v", got, DefaultFUSE())
	}
	got = ApplyFUSEDefaults(FUSESection{}, true)
	if got.MountTimeoutSeconds != DefaultFUSEMountTimeoutSeconds {
		t.Fatalf("present [FUSE] without timeout: got %d", got.MountTimeoutSeconds)
	}
	got = ApplyFUSEDefaults(FUSESection{MountTimeoutSeconds: 12}, true)
	if got.MountTimeoutSeconds != 12 {
		t.Fatalf("explicit timeout must stick, got %d", got.MountTimeoutSeconds)
	}
}

func TestFUSEVolumeValidate(t *testing.T) {
	if err := (&FUSEVolumeSection{Remote: "smb://h/s"}).Validate(); err == nil {
		t.Fatal("missing mountpoint should fail")
	}
	if err := (&FUSEVolumeSection{Mountpoint: "/mnt/s"}).Validate(); err == nil {
		t.Fatal("missing remote should fail")
	}
	if err := (&FUSEVolumeSection{Remote: "smb://h/s", Mountpoint: "/mnt/s"}).Validate(); err != nil {
		t.Fatalf("valid volume: %v", err)
	}
}

func TestFUSEVolumeSecretMasking(t *testing.T) {
	live := &FUSEVolumeSection{
		Remote:     "smb://foo:secret@foohost,smb/share",
		Mountpoint: "/Volumes/share",
	}
	masked := live.MaskedClone().(*FUSEVolumeSection)
	if masked.Remote != "smb://foo:"+RedactedSecret+"@foohost,smb/share" {
		t.Fatalf("masked remote = %q", masked.Remote)
	}
	if live.Remote != "smb://foo:secret@foohost,smb/share" {
		t.Fatalf("MaskedClone mutated the receiver: %q", live.Remote)
	}

	round := masked.Unmask(live).(*FUSEVolumeSection)
	if round.Remote != live.Remote {
		t.Fatalf("unmask restore = %q, want %q", round.Remote, live.Remote)
	}

	changed := &FUSEVolumeSection{Remote: "smb://foo:newpass@foohost,smb/share", Mountpoint: live.Mountpoint}
	if got := changed.Unmask(live).(*FUSEVolumeSection).Remote; got != changed.Remote {
		t.Fatalf("changed password should stick, got %q", got)
	}

	noPass := &FUSEVolumeSection{Remote: "afp://server/Volume", Mountpoint: "/Volumes/v"}
	if got := noPass.MaskedClone().(*FUSEVolumeSection).Remote; got != noPass.Remote {
		t.Fatalf("URI without password should stay clear: %q", got)
	}
}

func TestFUSEVolumesFromModel(t *testing.T) {
	m := NewModel()
	if got := FUSEVolumesFromModel(m); len(got) != 0 {
		t.Fatalf("empty model: got %d", len(got))
	}
	m.AddInstance(&FUSEVolumeSection{Remote: "smb://h/s", Mountpoint: "/mnt/a"})
	m.AddInstance(&FUSEVolumeSection{Remote: "afp://h/v", Mountpoint: "/mnt/b"})
	got := FUSEVolumesFromModel(m)
	if len(got) != 2 || got[0].Mountpoint != "/mnt/a" || got[1].Mountpoint != "/mnt/b" {
		t.Fatalf("got %+v", got)
	}
}

func TestModelValidateFUSE(t *testing.T) {
	m := NewModel()
	m.FUSE = FUSESection{MountTimeoutSeconds: -1}
	if err := m.Validate(ValidateOptions{}); err == nil {
		t.Fatal("bad [FUSE] should fail Model.Validate")
	}
}
