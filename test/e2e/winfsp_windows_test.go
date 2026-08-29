//go:build windows

package e2e

// winfsp_windows_test.go drives the WinFsp mount Adapter (client/winfsp) over a LIVE
// classicstack server's ForkFS — the same Adapter csmount hands to the WinFsp kernel. It
// exercises the mount's delegate surface (Create → Write → Read → GetFileInfo →
// ReadDirectory → Rename → Cleanup(delete)) end to end against a real remote AFP share, so
// the mount is proven to reflect a remote protocol, not just an in-memory fs. It needs no
// WinFsp kernel driver: the delegates are called directly, exactly as adapter_test.go does
// against memfs, but here the fs.ForkFS is a connected AFP client.
//
// A real drive-letter mount (the WinFsp driver materialising X:) is inherently a manual,
// interactive check — it is in the mount runbook, not this test.

import (
	"bytes"
	"testing"

	winfspclient "github.com/ObsoleteMadness/ClassicStack/client/winfsp"
	winfsp "github.com/winfsp/go-winfsp"
	"golang.org/x/sys/windows"
)

// The WinFsp FSCTL create-option / cleanup-flag values the Adapter delegates interpret
// (go-winfsp exports no named constants; the Adapter defines its own, unexported). We
// mirror the same values here to drive Open(dir)/Cleanup(delete).
const (
	fileDirectoryFile = 0x00000001 // FILE_DIRECTORY_FILE
	fspCleanupDelete  = 0x01       // FspCleanupDelete
)

// TestWinFSPMount_E2E connects to a live AFP server and drives the mount Adapter's
// delegates against it, asserting the file round-trips through the mount surface.
func TestWinFSPMount_E2E(t *testing.T) {
	remote := afpServer(t) // a live, connected AFP client fs.ForkFS
	a := winfspclient.New(remote, winfspclient.Options{VolumeLabel: "E2E"})

	// Create a file through the mount.
	var info winfsp.FSP_FSCTL_FILE_INFO
	fileCtx, err := a.Create(nil, "\\mount.txt", 0, windows.GENERIC_WRITE, 0, nil, 0, &info)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0 {
		t.Errorf("new file marked as directory: attrs=%#x", info.FileAttributes)
	}

	// Write, then read back through the mount.
	payload := []byte("mounted over a real AFP session")
	var winfo winfsp.FSP_FSCTL_FILE_INFO
	if n, err := a.Write(nil, fileCtx, payload, 0, false, false, &winfo); err != nil {
		t.Fatalf("Write: %v", err)
	} else if n != len(payload) {
		t.Fatalf("Write n=%d, want %d", n, len(payload))
	}
	buf := make([]byte, len(payload))
	if rn, err := a.Read(nil, fileCtx, buf, 0); err != nil {
		t.Fatalf("Read: %v", err)
	} else if !bytes.Equal(buf[:rn], payload) {
		t.Errorf("Read got %q, want %q", buf[:rn], payload)
	}
	a.Close(nil, fileCtx)

	// The file must be visible to the server-side client too (mount landed it remotely).
	if _, err := remote.Stat("mount.txt"); err != nil {
		t.Fatalf("Stat mount.txt on remote: %v", err)
	}

	// List the root through the mount and confirm the entry appears.
	var dinfo winfsp.FSP_FSCTL_FILE_INFO
	dirCtx, err := a.Open(nil, "\\", fileDirectoryFile, windows.GENERIC_READ, &dinfo)
	if err != nil {
		t.Fatalf("Open root: %v", err)
	}
	seen := map[string]bool{}
	if err := a.ReadDirectory(nil, dirCtx, "", func(name string, _ *winfsp.FSP_FSCTL_FILE_INFO) (bool, error) {
		seen[name] = true
		return true, nil
	}); err != nil {
		t.Fatalf("ReadDirectory: %v", err)
	}
	a.Close(nil, dirCtx)
	if !seen["mount.txt"] {
		t.Errorf("mount.txt not listed through mount: %v", seen)
	}

	// Rename then delete through the mount.
	if err := a.Rename(nil, 0, "\\mount.txt", "\\moved.txt", false); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	var oinfo winfsp.FSP_FSCTL_FILE_INFO
	delCtx, err := a.Open(nil, "\\moved.txt", 0, windows.GENERIC_READ, &oinfo)
	if err != nil {
		t.Fatalf("Open moved.txt: %v", err)
	}
	a.Cleanup(nil, delCtx, "\\moved.txt", fspCleanupDelete)
	a.Close(nil, delCtx)
	if _, err := remote.Stat("moved.txt"); err == nil {
		t.Errorf("moved.txt still present on remote after mount delete")
	}
}
