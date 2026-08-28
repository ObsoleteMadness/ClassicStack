//go:build etherdfs || all

package registry

import (
	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/link"
	etherport "github.com/ObsoleteMadness/ClassicStack/core/port/etherdfs"
	"github.com/ObsoleteMadness/ClassicStack/core/service/etherdfs"
)

func init() {
	// Register the EtherDFS drive repeated-section schema and the singleton server
	// section so codecs round-trip them. Kept here (not in an etherdfs-package init)
	// so the sections exist exactly when the EtherDFS service is built.
	etherdfs.RegisterDrives()
	etherdfs.RegisterServer()

	// EtherDFS is BOTH the wire endpoint and the file server: a single component
	// whose port half (the EtherType-0xEDF5 raw-Ethernet link) the service embeds, so
	// it is registered as one service factory — there is no separate port component
	// and no transport cross-wire (EtherDFS framing is single-purpose). The factory
	// builds the port from the [EtherDFS] section's NIC binding, then the service over
	// it.
	Register(etherdfs.Name, func(ctx *BuildContext) (component.Component, error) {
		m := ctx.Model
		logger := ctx.Logger(etherdfs.Name)

		// Resolve the wire binding from the singleton server section and project it
		// onto a port.Section the NIC opener / EtherDFS port consume.
		srv := etherdfs.ServerSectionFromModel(m)
		sec := srv.PortSection()

		// Build the EtherDFS port: a NIC-bound raw-Ethernet link opened per Start via
		// the injected opener (nil → inert-but-configured). A DISABLED section still
		// builds the component (the MacIP pattern) so the dashboard shows it Disabled
		// and the web UI can configure/enable it live; the opener below re-reads the
		// section from the model on EVERY Start, so a disabled service starts inert
		// (no pcap handle) and an enable or interface change takes effect on the next
		// (re)start rather than needing a process restart.
		iface := m.EffectiveInterfaceFor(sec)
		gated := func() (link.FrameLink, error) {
			cur := etherdfs.ServerSectionFromModel(m)
			if !cur.IsEnabled {
				return nil, nil // disabled → inert start
			}
			csec := cur.PortSection()
			cIface := m.EffectiveInterfaceFor(csec)
			open := nicLinkOpener(ctx, csec, cIface, etherport.BPFFilter, sectionMACFor(ctx, csec, cIface))
			if open == nil {
				return nil, nil
			}
			return open()
		}
		// An empty section mac inherits the bound interface's hw_address so EtherDFS
		// frames carry a real Ethernet source (else 00:00:00:00:00:00).
		p, err := etherport.NewInstanceFromOpener(sec, gated, sectionMACFor(ctx, sec, iface), logger)
		if err != nil {
			return nil, err
		}
		svc := etherdfs.New(p, logger)
		if svc == nil {
			return nil, nil
		}
		// Publish session open/close pings on the telemetry bus so the web UI's
		// Sharing Monitor can refresh its EtherDFS tab without polling (mirrors reg_afp.go).
		if ctx.Telemetry != nil {
			svc.SetPublisher(ctx.Telemetry)
		}

		// Server identity is the shared Identity.Hostname (§4-bis), unless the section
		// overrides it: EtherDFS advertises this name in AL_INSTALLCHK replies. The
		// resolver lets a hot-applied section with no name re-derive the fallback.
		name := srv.ServerName
		if name == "" {
			name = m.Identity.Hostname
		}
		svc.SetServerName(name)
		svc.SetServerNameResolver(func() string { return m.Identity.Hostname })

		// §10d: build each drive over the shared FS-mutation bus for its host path, so a
		// same-host-path AFP volume / SMB share sees this drive's mutations (and
		// vice-versa). Set BEFORE the drives are built so the initial set gets it too.
		svc.SetBusResolver(fsBus.busFor)
		// Hot-apply resolver: a Reconfigure of a drive section reconciles the live drive
		// set against the model.
		svc.SetDriveResolver(func() ([]etherdfs.DriveSpec, error) {
			return etherdfs.SpecsFromModel(m), nil
		})
		// Populate the initial drive set through the reconcile path so it is built over
		// the shared bus. A bad spec fails the build loudly here; an empty model yields a
		// service with no drives (the zero-config default).
		if err := svc.ReconcileDrives(etherdfs.SpecsFromModel(m)); err != nil {
			return nil, err
		}
		return svc, nil
	})
	registerIdentityStamper(func(c component.Component, m *config.Model) bool {
		svc, ok := c.(*etherdfs.Service)
		if !ok {
			return false
		}
		srv := etherdfs.ServerSectionFromModel(m)
		name := srv.ServerName
		if name == "" {
			name = m.Identity.Hostname
		}
		svc.SetServerName(name)
		return true
	})
}
