//go:build all

package registry

import (
	"context"
	"testing"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/service/afp"
	"github.com/ObsoleteMadness/ClassicStack/core/service/smb"
)

// TestAFPSMBCoordinateOnSamePath is the §10d end-to-end: an AFP volume and an SMB
// share configured on the SAME host path are built through the registry factories,
// which hand both the SAME FS-mutation bus (the fsBus broker). When AFP mutates the
// shared directory, SMB's reactor is notified (and not AFP's own — Origin filtered).
func TestAFPSMBCoordinateOnSamePath(t *testing.T) {
	root := t.TempDir()

	m := config.NewModel()
	m.AddInstance(&afp.VolumeSection{VName: "Shared", FSType: "local_fs", ForkBackend: "appledouble", FilenameCodec: "macroman-utf8", Path: root})
	m.AddInstance(&smb.ShareSection{SName: "Shared", FSType: "local_fs", ForkBackend: "appledouble", FilenameCodec: "macroman-utf8", Path: root})

	afpComp, ok, err := Build(afp.Name, &BuildContext{Model: m})
	if err != nil || !ok {
		t.Fatalf("Build(AFP) = (_, %v, %v)", ok, err)
	}
	smbComp, ok, err := Build(smb.Name, &BuildContext{Model: m})
	if err != nil || !ok {
		t.Fatalf("Build(SMB) = (_, %v, %v)", ok, err)
	}
	afpSvc := afpComp.(*afp.Service)
	smbSvc := smbComp.(*smb.Service)

	ctx := context.Background()
	if err := afpSvc.Start(ctx); err != nil {
		t.Fatalf("AFP Start: %v", err)
	}
	defer afpSvc.Stop(ctx)
	if err := smbSvc.Start(ctx); err != nil {
		t.Fatalf("SMB Start: %v", err)
	}
	defer smbSvc.Stop(ctx)

	// Mutate through the AFP volume's FS — a create under the shared host root.
	vols := afpSvc.Volumes()
	if len(vols) != 1 {
		t.Fatalf("AFP built %d volumes, want 1", len(vols))
	}
	if err := vols[0].FS().CreateDir("newdir"); err != nil {
		t.Fatalf("CreateDir via AFP volume: %v", err)
	}

	// SMB's reactor (different origin) should be notified; AFP's own reactor must not.
	waitFor(t, func() bool { return smbSvc.ReactorDelivered() >= 1 })
	if afpSvc.ReactorDelivered() != 0 {
		t.Fatalf("AFP reactor delivered %d for its OWN mutation, want 0 (Origin filter)", afpSvc.ReactorDelivered())
	}
}

// waitFor polls until pred() or a deadline (the reactor delivers asynchronously).
func waitFor(t *testing.T, pred func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if pred() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("coordination condition not met before deadline")
}

var _ component.Component = (*afp.Service)(nil)
