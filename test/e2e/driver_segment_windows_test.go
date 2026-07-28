//go:build windows && driverint

// Package e2e's driver-integration variant (build tag `driverint`) exercises the REAL
// driver-backed wire paths that the in-process harness cannot: raw-Ethernet transports
// over an actual Npcap-captured segment, and a REAL WinFsp drive-letter mount driven
// through the OS file APIs. It is deliberately excluded from CI:
//
//   - CI runs `go test -tags all -race ./...` on ubuntu-latest; `driverint` is NOT in the
//     `all` tag and is never passed by any workflow, and these files are `//go:build
//     windows && driverint`, so they never even compile in CI.
//   - Even locally the whole suite gates on CLASSICSTACK_DRIVER_TEST=1 plus a runtime
//     preflight (WinFsp present, Npcap present, a usable isolated virtual adapter), so a
//     developer who builds with the tag but lacks the drivers gets a clean Skip, not a
//     failure.
//
// Run it with, from an ELEVATED shell (Npcap raw capture + Hyper-V switch creation both
// need Administrator):
//
//	set CLASSICSTACK_DRIVER_TEST=1
//	go test -tags "driverint pcap" -run TestDriver ./test/e2e/ -v
//
// driver_segment_windows_test.go owns preflight + the network segment: it prefers to spin
// up a temporary Hyper-V PRIVATE vSwitch (and tears it down at the end); failing that
// (Hyper-V absent or not elevated) it auto-selects an existing isolated virtual adapter
// (VirtualBox Host-Only / VMware VMnet / vEthernet) that passes a raw send+capture
// self-probe. If neither is available the whole driver suite skips.

package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/adapter/link/pcap"
	"github.com/ObsoleteMadness/ClassicStack/core/link"
)

// driverEnvVar must be set to "1" for the driver suite to run at all — belt-and-braces on
// top of the build tag so a stray `-tags driverint` build never silently drives the NIC.
const driverEnvVar = "CLASSICSTACK_DRIVER_TEST"

// probeEtherType is a locally-administered experimental EtherType (IEEE 802 "local
// experimental 1") used only for the segment self-probe frame, so it never collides with a
// real IPX/AppleTalk/NetBEUI frame on the wire.
var probeEtherType = [2]byte{0x88, 0xB5}

// segment is an acquired raw-Ethernet segment: a pcap device name plus an optional cleanup
// (removing a Hyper-V switch we created). Both a client and a server bind dev.
type segment struct {
	dev      string
	teardown func()
}

// requireDriverEnv skips the whole suite unless CLASSICSTACK_DRIVER_TEST=1, WinFsp is
// installed, and Npcap is installed. It is called by every driver test.
func requireDriverEnv(t *testing.T) {
	t.Helper()
	if os.Getenv(driverEnvVar) != "1" {
		t.Skipf("driver-backed test: set %s=1 (needs Npcap + WinFsp + an isolated virtual NIC, run elevated)", driverEnvVar)
	}
	if !winfspInstalled() {
		t.Skip("driver-backed test: WinFsp not installed")
	}
	if !npcapInstalled() {
		t.Skip("driver-backed test: Npcap not installed")
	}
}

// winfspInstalled reports whether the WinFsp runtime DLL is present (the mount needs it).
func winfspInstalled() bool {
	for _, p := range []string{
		`C:\Program Files (x86)\WinFsp\bin\winfsp-x64.dll`,
		`C:\Program Files\WinFsp\bin\winfsp-x64.dll`,
	} {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

// npcapInstalled reports whether the Npcap runtime is present.
func npcapInstalled() bool {
	_, err := os.Stat(`C:\Windows\System32\Npcap\wpcap.dll`)
	return err == nil
}

// acquireSegment obtains a usable raw-Ethernet segment: first a temporary Hyper-V PRIVATE
// vSwitch (torn down on cleanup), else an existing isolated virtual adapter that passes the
// send+capture self-probe. It skips the test if nothing works.
func acquireSegment(t *testing.T) segment {
	t.Helper()

	// 1. Preferred: create a temporary Hyper-V private switch and use its vEthernet adapter.
	if seg, ok := tryHyperVSwitch(t); ok {
		return seg
	}

	// 2. Fallback: an existing isolated virtual adapter that round-trips a raw frame.
	if dev, ok := findWorkingVirtualAdapter(t); ok {
		t.Logf("driver segment: using existing virtual adapter %s", dev)
		return segment{dev: dev, teardown: func() {}}
	}

	t.Skip("driver-backed test: no usable raw-Ethernet segment (no Hyper-V/elevation, no send-capable isolated virtual adapter)")
	return segment{}
}

// tryHyperVSwitch attempts to create a temporary Hyper-V private vSwitch and returns its
// pcap device once it round-trips a raw frame. It cleans the switch up via t.Cleanup. It
// returns ok=false (no error) when Hyper-V is unavailable or we lack the privilege — the
// caller then falls back to an existing adapter.
func tryHyperVSwitch(t *testing.T) (segment, bool) {
	t.Helper()
	if _, err := exec.LookPath("powershell"); err != nil {
		return segment{}, false
	}
	// Is New-VMSwitch available (Hyper-V PowerShell module + VMMS)?
	if out, err := runPS(`(Get-Command New-VMSwitch -ErrorAction SilentlyContinue) -ne $null`); err != nil || strings.TrimSpace(out) != "True" {
		return segment{}, false
	}

	name := fmt.Sprintf("ClassicStackTest-%d", time.Now().UnixNano())
	if _, err := runPS(fmt.Sprintf(`New-VMSwitch -Name '%s' -SwitchType Private -ErrorAction Stop | Out-Null`, name)); err != nil {
		// Almost always "not elevated" or Hyper-V not fully enabled — fall back quietly.
		t.Logf("driver segment: Hyper-V switch create failed (%v); falling back to an existing adapter", firstLine(err.Error()))
		return segment{}, false
	}
	teardown := func() {
		_, _ = runPS(fmt.Sprintf(`Remove-VMSwitch -Name '%s' -Force -ErrorAction SilentlyContinue`, name))
	}
	t.Cleanup(teardown)

	// The vEthernet adapter appears in Npcap's device list a moment after creation; poll for
	// a device whose description matches the switch and passes the round-trip probe.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if dev, ok := findAdapterForSwitch(name); ok {
			if probeSegment(dev) {
				t.Logf("driver segment: created Hyper-V private switch %q → %s", name, dev)
				return segment{dev: dev, teardown: teardown}, true
			}
		}
		time.Sleep(time.Second)
	}
	t.Logf("driver segment: Hyper-V switch %q never surfaced a send-capable pcap device; falling back", name)
	return segment{}, false
}

// findAdapterForSwitch maps a Hyper-V switch name to its pcap device name via the
// "vEthernet (<switch>)" NDIS adapter Npcap exposes.
func findAdapterForSwitch(switchName string) (string, bool) {
	devs, err := pcap.ListDevices()
	if err != nil {
		return "", false
	}
	want := "vethernet (" + strings.ToLower(switchName) + ")"
	for _, d := range devs {
		if strings.Contains(strings.ToLower(d.Description), want) {
			return d.Name, true
		}
	}
	return "", false
}

// findWorkingVirtualAdapter scans the pcap device list for an isolated virtual adapter
// (Host-Only / VMnet / vEthernet, never the loopback capture adapter — Npcap cannot SEND
// on loopback) and returns the first that round-trips a raw self-probe frame. An explicit
// CLASSICSTACK_DRIVER_IFACE overrides the scan.
func findWorkingVirtualAdapter(t *testing.T) (string, bool) {
	if forced := os.Getenv("CLASSICSTACK_DRIVER_IFACE"); forced != "" {
		if probeSegment(forced) {
			return forced, true
		}
		t.Logf("driver segment: CLASSICSTACK_DRIVER_IFACE=%s did not pass the send/capture probe", forced)
		return "", false
	}
	devs, err := pcap.ListDevices()
	if err != nil {
		return "", false
	}
	for _, d := range devs {
		desc := strings.ToLower(d.Description)
		if strings.Contains(d.Name, "NPF_Loopback") {
			continue // capture-only: Npcap cannot inject raw frames on loopback
		}
		isolated := strings.Contains(desc, "host-only") || strings.Contains(desc, "vmnet") ||
			strings.Contains(desc, "vethernet") || strings.Contains(desc, "virtualbox")
		if !isolated {
			continue
		}
		if probeSegment(d.Name) {
			return d.Name, true
		}
	}
	return "", false
}

// probeSegment opens two pcap handles on dev, sends a unique-EtherType frame on one, and
// reports whether the other captures it — the definitive "can this adapter carry our raw
// wire?" check (loopback fails the send, so it never passes).
func probeSegment(dev string) bool {
	cfg := pcap.Config{Interface: dev, SnapLen: 65535, Promiscuous: true, ImmediateMode: true, ReadTimeout: 200 * time.Millisecond}
	rx, err := pcap.Open(cfg)
	if err != nil {
		return false
	}
	defer rx.Close()
	tx, err := pcap.Open(cfg)
	if err != nil {
		return false
	}
	defer tx.Close()

	frame := []byte{0x02, 0, 0, 0, 0, 0xAA, 0x02, 0, 0, 0, 0, 0xBB, probeEtherType[0], probeEtherType[1]}
	frame = append(frame, []byte("CLASSICSTACK-SEGMENT-PROBE")...)
	for len(frame) < 60 {
		frame = append(frame, 0)
	}

	got := make(chan bool, 1)
	go func() {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			f, err := rx.Read()
			if err == link.ErrTimeout {
				continue
			}
			if err != nil {
				got <- false
				return
			}
			if len(f) >= 14 && f[12] == probeEtherType[0] && f[13] == probeEtherType[1] {
				got <- true
				return
			}
		}
		got <- false
	}()

	time.Sleep(200 * time.Millisecond)
	for i := 0; i < 6; i++ {
		if err := tx.Write(frame); err != nil {
			// A send failure (loopback: error 87) means this device cannot inject — but keep
			// draining the receiver until its deadline in case an earlier write partially landed.
		}
		time.Sleep(120 * time.Millisecond)
	}
	return <-got
}

// runPS runs a PowerShell one-liner and returns its stdout (trimmed on error into the
// returned error). Used only for the optional Hyper-V switch lifecycle.
func runPS(script string) (string, error) {
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
