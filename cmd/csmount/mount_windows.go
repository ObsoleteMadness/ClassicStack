//go:build windows

package main

import (
	"io"

	"github.com/ObsoleteMadness/ClassicStack/client/winfsp"
	"github.com/ObsoleteMadness/ClassicStack/cmd/internal/csconnect"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

type mounter interface {
	Unmount()
	Wait()
}

func traceMount(w io.Writer) { winfsp.TraceTo(w) }

func mountAt(remote fs.ForkFS, mountpoint, volume string, cfg csconnect.Config) (mounter, error) {
	return winfsp.MountAt(remote, mountpoint, winfsp.Options{
		VolumeLabel:        volume,
		FileInfoTimeoutMs:  cfg.CacheMs,
		FileInfoTimeoutSet: cfg.CacheMsSet,
		NativeForks:        cfg.Fork == "native" || cfg.Fork == "ads" || cfg.Fork == "hfs",
	})
}

func usageText() string {
	return `csmount — mount a ClassicStack share on Windows (WinFsp)

Usage:
  csmount [flags] <uri> <mountpoint>

  <mountpoint> is a drive letter ("X:") or an empty directory.

Flags:
  -ifacetype  transport: ltoudp | tashtalk | pcap | tcp (scheme-validated)
  -iface      interface: IPv4 addr (ltoudp), device (pcap), COM3//dev/tty (tashtalk), host (tcp)
              (pcap: omit to auto-detect the host's primary/default-route NIC)
  -transport  SMB pcap carrier: ipx (default) | nbipx | nbf
  -mac        virtual-station MAC for raw-Ethernet SMB carriers (empty = random)
  -fork       fork container: appledouble | applesingle | macbinary | derez | passthrough | native | ads | nofork
              Sidecar layouts (derez/appledouble) PROJECT remote forks into the mount as
              .rdump/.idump or ._name files. "native" (= "ads" on Windows) instead exposes
              the resource fork / Finder info / comment as NTFS SFM streams (:AFP_Resource,
              :AFP_AfpInfo, :Comments) so Windows tools see them like a real SFM server.
  -cache-ms   WinFsp FileInfoTimeout in ms (default 1000). 0 disables FSD metadata cache;
              -1 is infinite (also enables kernel data caching).
  -v          verbose: NBP + AFP wire-trace + WinFsp Behaviour* call names to stderr (ATP off)
  -list-ifaces  list the capturable pcap NICs (the names -iface accepts) and exit

Examples:
  csmount -ifacetype tcp afp://server/Volume X:
  csmount -fork derez afp://vmac1/System\ 7.5.3 X:
  csmount smb://server,nbf/Share M:
  csmount ncp://SERVER/SYS N:
`
}
