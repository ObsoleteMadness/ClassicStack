// Package etherdfs is the EtherDFS ("The Ethernet DOS File System", by Mateusz
// Viste) server re-expressed over the §9 storage seam. It serves DOS clients that
// map a remote drive to a local drive letter over raw Ethernet (EtherType 0xEDF5,
// no IP/TCP/NetBIOS), translating the redirector's 8.3 / FAT-attribute requests
// into operations on the shared fs.ForkFS that AFP and SMB also drive.
//
// The service is BOTH the wire endpoint and the file server. The wire half is a
// core/port/etherdfs.Port the service EMBEDS (so it satisfies component.Component
// + Enableable/Bindable/Statful/Metered/Configurable and is restartable via the
// injected NIC opener); the port owns the read loop / link reopen / frame dedup /
// metering and the EtherType 0xEDF5 demux, and calls the service's installed
// Handler for each request. There is no separate component and no compose
// cross-wire (EtherDFS framing is single-purpose) — the registry builds the port
// and the service in one factory.
//
// Security posture: this is a compatibility server, not an authentication server.
// EtherDFS has no login; every client on the segment that can reach the server's
// MAC may use any configured drive (gated only by a drive's read-only flag and
// AllowedUsers allow-list, which with no user store means world-accessible). This
// matches the original ethersrv and is the intentional weakness that lets vintage
// DOS clients connect.
//
// Attribution: the EtherDFS protocol and the reference ethersrv/ethflop servers are
// the work of Mateusz Viste, Copyright © 2017-2023 Mateusz Viste
// (http://etherdfs.sourceforge.net / https://github.com/mateuszviste). This is a clean
// re-implementation of the EtherType 0xEDF5 wire protocol over the §9 storage seam, not
// a code port, but it owes a clear debt to his work (CLAUDE.md #7).
package etherdfs

import (
	"context"
	"sort"
	"strings"
	"sync"

	"github.com/ObsoleteMadness/ClassicStack/core/bus"
	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	etherport "github.com/ObsoleteMadness/ClassicStack/core/port/etherdfs"
)

// Name is the component name for the EtherDFS service.
const Name = "EtherDFS"

// OriginEtherDFS tags FS-mutation events this service produces on the shared §10d
// FS bus, so a same-host-path AFP volume / SMB share reactor acts on them and
// EtherDFS's own writes are not re-delivered to it.
const OriginEtherDFS = "etherdfs"

// defaultServerName is reported in AL_INSTALLCHK replies when no name is configured.
const defaultServerName = "CLASSICSTACK"

// Service is the EtherDFS component. It embeds the EtherDFS port (the wire half)
// and adds the file-service half: the configured drives, per-client session
// state, and the server name advertised in install checks.
type Service struct {
	*etherport.Port

	logger log.Logger

	mu       sync.Mutex
	drives   map[uint8]*Drive // by drive number (0=A … 25=Z)
	server   string
	nameFor  func() string // fallback server name (Identity.Hostname) for a hot-apply with no section name
	busFor   func(fs.ShareSpec) bus.Bus
	resolver func() ([]DriveSpec, error)

	sessions *sessionTable
}

// New builds the EtherDFS service over an already-built EtherDFS port (the wire
// half). The port is the embedded component the supervisor drives; the service
// installs its dispatch as the port's request handler. A nil port (the section
// was disabled) yields a nil service so the registry returns (nil, nil).
func New(p *etherport.Port, logger log.Logger) *Service {
	if p == nil {
		return nil
	}
	s := &Service{
		Port:     p,
		logger:   logger,
		drives:   make(map[uint8]*Drive),
		sessions: newSessionTable(),
	}
	p.SetHandler(s.dispatch)
	return s
}

// Name returns the component name (overrides the embedded port's so the service
// is addressed as "EtherDFS").
func (s *Service) Name() string { return Name }

// Stop tears down the per-client sessions, then stops the embedded port (closing
// the link).
func (s *Service) Stop(ctx context.Context) error {
	s.sessions.closeAll()
	// Definitive teardown: close each drive's FS backend to release any GC-invisible
	// resource (zipfs handles, macgarden goroutine). A plain backend's Close is a no-op.
	s.mu.Lock()
	drives := make([]*Drive, 0, len(s.drives))
	for _, d := range s.drives {
		drives = append(drives, d)
	}
	s.mu.Unlock()
	for _, d := range drives {
		_ = d.Close()
	}
	return s.Port.Stop(ctx)
}

// SetServerName sets the name reported in AL_INSTALLCHK replies (the shared
// Identity.Hostname). Unset defaults to CLASSICSTACK. Idempotent.
func (s *Service) SetServerName(name string) {
	s.mu.Lock()
	s.server = name
	s.mu.Unlock()
}

// serverName returns the configured server name, defaulting to CLASSICSTACK.
func (s *Service) serverName() string {
	s.mu.Lock()
	name := s.server
	s.mu.Unlock()
	if name != "" {
		return name
	}
	return defaultServerName
}

// SetServerNameResolver installs the fallback the advertised name re-resolves
// through when a hot-applied ServerSection carries no server_name (the shared
// Identity.Hostname; §4-bis). nil keeps the built-in CLASSICSTACK default.
func (s *Service) SetServerNameResolver(f func() string) {
	s.mu.Lock()
	s.nameFor = f
	s.mu.Unlock()
}

// SetBusResolver installs the resolver that returns the shared FS-mutation bus for
// a drive's host path (§10d). Set BEFORE ReconcileDrives so the initial drive set
// is built over the shared bus. nil = isolated drives.
func (s *Service) SetBusResolver(f func(fs.ShareSpec) bus.Bus) {
	s.mu.Lock()
	s.busFor = f
	s.mu.Unlock()
}

// SetDriveResolver installs the resolver that re-reads the desired drive set from
// the model, for hot-apply reconciliation.
func (s *Service) SetDriveResolver(f func() ([]DriveSpec, error)) {
	s.mu.Lock()
	s.resolver = f
	s.mu.Unlock()
}

// ApplyConfig hot-applies a reconfigure (component.Configurable), overriding the
// embedded port's so both section shapes the supervisor addresses to "EtherDFS"
// land correctly:
//
//   - the singleton *ServerSection (the dashboard cog / Sharing-tab server panel):
//     the advertised name applies live; the wire binding is projected onto a
//     port.Section for the embedded port, which answers ErrNeedsRestart for a
//     structural (interface) change. An enabled-flag flip is also answered with
//     ErrNeedsRestart — the link is opened/closed per Start via the enabled-gated
//     opener, so only a restart makes the flip take effect on the wire.
//
//   - a drive instance, or nil (the owner-notify after an add/remove/edit of an
//     EtherDFSDrives entry): re-resolve the whole drive set from the model and
//     reconcile, mirroring AFP/SMB/NCP.
func (s *Service) ApplyConfig(section any) error {
	if srv, ok := section.(*ServerSection); ok && srv != nil {
		name := srv.ServerName
		s.mu.Lock()
		nameFor := s.nameFor
		s.mu.Unlock()
		if name == "" && nameFor != nil {
			name = nameFor()
		}
		s.SetServerName(name)
		wasEnabled := s.Port.Enabled()
		if err := s.Port.ApplyConfig(srv.PortSection()); err != nil {
			return err
		}
		if srv.IsEnabled != wasEnabled {
			return component.ErrNeedsRestart
		}
		return nil
	}
	s.mu.Lock()
	resolve := s.resolver
	s.mu.Unlock()
	if resolve == nil {
		return component.ErrNeedsRestart
	}
	desired, err := resolve()
	if err != nil {
		return err
	}
	return s.ReconcileDrives(desired)
}

// ReconcileDrives builds the drive set from specs, replacing any current set. A
// bad spec (invalid fs_type×fork×codec triple or missing required param) fails
// loudly here. Drive numbers are assigned from the configured drive letter
// (A=0 … Z=25); a spec whose name is not a single A–Z letter (or that collides)
// is assigned the next free number.
func (s *Service) ReconcileDrives(specs []DriveSpec) error {
	s.mu.Lock()
	busFor := s.busFor
	s.mu.Unlock()

	built := make(map[uint8]*Drive, len(specs))
	next := uint8(0)
	for _, spec := range specs {
		var b bus.Bus
		if busFor != nil {
			b = busFor(spec.Share)
		}
		drv, err := NewDriveWithBus(spec, b)
		if err != nil {
			return err
		}
		num, ok := driveNumber(spec.Name)
		if !ok || built[num] != nil {
			for built[next] != nil {
				next++
			}
			num = next
		}
		built[num] = drv
	}

	s.mu.Lock()
	s.drives = built
	s.mu.Unlock()
	return nil
}

// BoundDrives returns a snapshot of the configured EtherDFS drives (operator Finder).
func (s *Service) BoundDrives() []*Drive {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Drive, 0, len(s.drives))
	for _, d := range s.drives {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}

// DriveByName returns the bound drive with the given letter/name, if any.
func (s *Service) DriveByName(name string) (*Drive, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, d := range s.drives {
		if strings.EqualFold(d.Name(), name) {
			return d, true
		}
	}
	return nil, false
}

// drive returns the drive bound to a drive number, if any.
func (s *Service) drive(num uint8) (*Drive, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.drives[num]
	return d, ok
}

// driveCount returns the number of configured drives (diagnostics / Describable).
func (s *Service) driveCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.drives)
}

// driveNumber maps a one-letter drive name ("A".."Z", case-insensitive) to its
// DOS drive number (A=0 … Z=25). ok is false for any name that is not a single
// A–Z letter.
func driveNumber(name string) (uint8, bool) {
	if len(name) != 1 {
		return 0, false
	}
	c := name[0]
	switch {
	case c >= 'A' && c <= 'Z':
		return c - 'A', true
	case c >= 'a' && c <= 'z':
		return c - 'a', true
	}
	return 0, false
}

// Kind satisfies component.Describable for the dashboard card.
func (s *Service) Kind() string { return "DOS File Server" }

// Props reports a small live stat for the dashboard drill-down.
func (s *Service) Props() map[string]string {
	return map[string]string{"drives": itoa(s.driveCount())}
}

// Stats overrides the embedded port's snapshot (component.Statful) to add the
// file-service gauges — configured drives and live client sessions — on top of
// the port's frame/byte counters, for the dashboard stats line.
func (s *Service) Stats() component.Stats {
	st := s.Port.Stats()
	if st.Gauges == nil {
		st.Gauges = map[string]float64{}
	}
	st.Gauges["drives"] = float64(s.driveCount())
	st.Gauges["sessions"] = float64(s.sessions.count())
	return st
}

// itoa formats a small non-negative int without importing strconv.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// compile-time capability assertions: the embedded port supplies
// Component/Enableable/Bindable/Metered; the service adds Describable and
// overrides Name/Stop/Stats/ApplyConfig (Statful gains the drive/session gauges,
// Configurable routes ServerSection + drive-set reconfigures).
var (
	_ component.Component    = (*Service)(nil)
	_ component.Describable  = (*Service)(nil)
	_ component.Statful      = (*Service)(nil)
	_ component.Configurable = (*Service)(nil)
)
