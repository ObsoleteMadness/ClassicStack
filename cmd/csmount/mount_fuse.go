//go:build (darwin || linux) && fuse && cgo

package main

import (
	"io"

	csfuse "github.com/ObsoleteMadness/ClassicStack/client/fuse"
	"github.com/ObsoleteMadness/ClassicStack/cmd/internal/csconnect"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

type mounter interface {
	Unmount()
	Wait()
}

func traceMount(w io.Writer) { csfuse.TraceTo(w) }

func mountAt(remote fs.ForkFS, mountpoint, volume string, cfg csconnect.Config) (mounter, error) {
	return csfuse.MountAt(remote, mountpoint, csfuse.Options{
		VolumeLabel: volume,
		NativeForks: nativeForksUnix(cfg.Fork),
	})
}

func usageText() string {
	return `csmount — mount a ClassicStack share via FUSE (macFUSE on macOS, libfuse on Linux)

Usage:
  csmount [flags] <uri> <mountpoint>

  <mountpoint> is an empty directory. Rebuild with: go build -tags fuse -o csmount ./cmd/csmount
  Requires macFUSE (https://macfuse.github.io/) on macOS, or libfuse on Linux.

Flags:
  -ifacetype  transport: ltoudp | tashtalk | pcap | tcp (scheme-validated)
  -iface      interface: IPv4 addr (ltoudp), device (pcap), serial (tashtalk), host (tcp)
  -transport  SMB pcap carrier: ipx (default) | nbipx | nbf
  -mac        virtual-station MAC for raw-Ethernet SMB carriers (empty = random)
  -fork       fork container: appledouble | applesingle | macbinary | derez | passthrough | native | hfs | xattr | ads | nofork
              Sidecar layouts PROJECT remote forks into the mount as ._name / .rdump files.
              passthrough/native/hfs/xattr/ads (and the default empty fork) instead map
              resource forks and Finder info to host xattrs: com.apple.FinderInfo +
              com.apple.ResourceFork on macOS, user.org.netatalk.Metadata + ResourceFork
              on Linux.
  -v          verbose: NBP + AFP wire-trace + FUSE op names to stderr (ATP off)
  -list-ifaces  list the capturable pcap NICs (the names -iface accepts) and exit

Examples:
  csmount -ifacetype tcp afp://server/Volume /Volumes/Classic
  csmount -fork appledouble afp://vmac1/System\ 7.5.3 /mnt/sys75
`
}
