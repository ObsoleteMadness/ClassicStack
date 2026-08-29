//go:build windows && driverint

package e2e

// driver_mount_windows_test.go performs a REAL WinFsp mount: it mounts an in-process
// classicstack share at an actual drive letter via the WinFsp kernel driver, then drives
// file operations through the OS (os.Create/os.ReadFile/os.ReadDir/os.Rename/os.Remove) so
// the whole path — Windows file API → WinFsp driver → go-winfsp dispatcher → our Adapter
// delegates → the ForkFS — is exercised end to end. This is the mount coverage the
// adapter_test.go unit test (delegates in-process) and the winfsp_windows_test.go
// (Adapter delegates, no driver) cannot provide.
//
// The mount is backed by the AFP client over the DDP bridge (afpServer), so the drive
// letter reflects a real remote protocol session, not just a local fs.

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	winfspclient "github.com/ObsoleteMadness/ClassicStack/client/winfsp"
)

// TestDriverWinFSPMount mounts a live share at a free drive letter through the WinFsp
// driver and exercises real OS file operations against it.
func TestDriverWinFSPMount(t *testing.T) {
	requireDriverEnv(t)

	remote := afpServer(t) // a live, connected AFP client fs.ForkFS
	drive := freeDriveLetter(t)

	mount, err := winfspclient.MountAt(remote, drive, winfspclient.Options{VolumeLabel: "E2E"})
	if err != nil {
		t.Skipf("WinFsp MountAt(%s) failed (driver present but mount refused, e.g. not elevated): %v", drive, err)
	}
	t.Cleanup(mount.Unmount)

	// WinFsp mounts asynchronously; wait briefly for the drive to materialise.
	root := drive + `\`
	if !waitFor(func() bool { _, err := os.Stat(root); return err == nil }, 10*time.Second) {
		t.Fatalf("drive %s never appeared after mount", drive)
	}

	// Write a file through the OS onto the mounted drive.
	name := filepath.Join(root, "mounted.txt")
	payload := []byte("written through a REAL WinFsp mount onto a live AFP share")
	if err := os.WriteFile(name, payload, 0o644); err != nil {
		t.Fatalf("os.WriteFile %s: %v", name, err)
	}

	// Read it back through the OS.
	got, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("os.ReadFile %s: %v", name, err)
	}
	if string(got) != string(payload) {
		t.Errorf("read back %q, want %q", got, payload)
	}

	// The file must be visible to the underlying client too (it landed on the remote share).
	if _, err := remote.Stat("mounted.txt"); err != nil {
		t.Errorf("mounted.txt not on remote share after OS write: %v", err)
	}

	// List the drive through the OS.
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("os.ReadDir %s: %v", root, err)
	}
	if !containsName(entries, "mounted.txt") {
		t.Errorf("mounted.txt not listed on drive: %v", names(entries))
	}

	// Create a directory + nested file through the OS.
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatalf("os.Mkdir %s: %v", sub, err)
	}
	nested := filepath.Join(sub, "inner.txt")
	if err := os.WriteFile(nested, []byte("nested"), 0o644); err != nil {
		t.Fatalf("os.WriteFile nested: %v", err)
	}

	// Rename through the OS. WinFsp passes the rename source name upper-cased (from the
	// normalized FileName, not the case-preserved Open path); the Adapter renames using the
	// open handle's authoritative correctly-cased path instead, so a case-sensitive AFP
	// server no longer returns kFPObjectNotFound (which surfaced as STATUS_INTERNAL_ERROR).
	// It also closes the source data fork before the rename (legacy SMB SMB_COM_RENAME
	// sharing rule) and reopens it on the target so WinFsp's post-rename handle use stays
	// valid.
	moved := filepath.Join(root, "moved.txt")
	if err := os.Rename(name, moved); err != nil {
		t.Fatalf("os.Rename %s -> %s through the driver: %v", name, moved, err)
	}
	if _, err := os.Stat(name); err == nil {
		t.Errorf("%s still present after rename", name)
	}
	got, err = os.ReadFile(moved)
	if err != nil {
		t.Fatalf("os.ReadFile %s after rename: %v", moved, err)
	}
	if string(got) != string(payload) {
		t.Errorf("after rename read %q, want %q", got, payload)
	}
	if err := os.Remove(moved); err != nil {
		t.Errorf("os.Remove %s: %v", moved, err)
	} else if _, err := os.Stat(moved); err == nil {
		t.Errorf("%s still present after os.Remove", moved)
	}

	// Clean up the nested tree so unmount is tidy.
	_ = os.Remove(nested)
	_ = os.Remove(sub)
}

// freeDriveLetter returns the first unused drive letter as "X:" (from Z down to G to avoid
// system-reserved letters), skipping the test when none is free.
func freeDriveLetter(t *testing.T) string {
	t.Helper()
	for c := 'Z'; c >= 'G'; c-- {
		drive := string(c) + ":"
		if _, err := os.Stat(drive + `\`); err != nil {
			return drive
		}
	}
	t.Skip("no free drive letter for the mount test")
	return ""
}

// waitFor polls cond until it is true or the timeout elapses.
func waitFor(cond func() bool, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return cond()
}

func containsName(entries []os.DirEntry, name string) bool {
	for _, e := range entries {
		if e.Name() == name {
			return true
		}
	}
	return false
}

func names(entries []os.DirEntry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}
