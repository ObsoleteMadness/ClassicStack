package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ObsoleteMadness/ClassicStack/config"
	"github.com/ObsoleteMadness/ClassicStack/netlog"
	"github.com/ObsoleteMadness/ClassicStack/pkg/control"
	"github.com/ObsoleteMadness/ClassicStack/pkg/status"
	"github.com/ObsoleteMadness/ClassicStack/port/rawlink"
)

// The methods here adapt the Supervisor to the control.Supervisor
// interface the management plane drives. RestartService is already
// implemented in supervisor_lifecycle.go.

// webUIUnitName is the reserved status/hook name for the management UI.
const webUIUnitName = "WebUI"

// Apply re-wires the running stack to match the supplied config model. It is
// an atomic whole-stack rebuild — the stack is stopped, reconstructed from
// the new model, and started — with one exception: the Web UI server is
// preserved across the rebuild. The UI must outlive a reconfiguration
// because Apply is itself driven by an in-flight UI request; tearing the
// server down here would drop that request and the operator's connection.
// Finer-grained per-service application can layer on later using the
// dynamic-router primitives without changing the control-plane contract.
//
// Known limitation of the atomic rebuild: services that bind a fixed TCP
// port (AFP/DSI on :548, SMB on :139) are torn down and re-bound on every
// Apply. On some platforms the OS holds the port briefly in TIME_WAIT, so a
// rebind immediately after stop can fail. Per-service application (rebuild
// only what changed) is the planned remedy; until then, an Apply that only
// touched, say, AFP volumes still cycles every listener.
func (s *Supervisor) Apply(ctx context.Context, cfg control.ConfigModel) error {
	model, ok := cfg.(*config.Model)
	if !ok {
		return fmt.Errorf("supervisor: unexpected config type %T", cfg)
	}

	newCfg, err := appConfigFromModel(model)
	if err != nil {
		return fmt.Errorf("supervisor: invalid config: %w", err)
	}

	netlog.Info("[SUP] applying new configuration (atomic rebuild, web UI preserved)")

	// Detach the live Web UI hook so the stack stop does not tear it down.
	webui := s.detachWebUI()

	if err := s.Stop(); err != nil {
		netlog.Warn("[SUP] stop during apply: %v", err)
	}

	rebuilt, err := NewSupervisor(newCfg, s.source, model)
	if err != nil {
		return fmt.Errorf("supervisor: rebuild failed: %w", err)
	}
	s.adoptFrom(rebuilt)

	// Re-attach the preserved Web UI so it remains a managed (already
	// running) unit of the rebuilt stack.
	s.reattachWebUI(webui)

	if err := s.Start(ctx); err != nil {
		return fmt.Errorf("supervisor: restart failed: %w", err)
	}
	netlog.Info("[SUP] configuration applied")
	return nil
}

// detachWebUI removes the Web UI hook from the running stack without
// stopping it, returning it so Apply can re-attach it to the rebuilt stack.
func (s *Supervisor) detachWebUI() hook {
	s.mu.Lock()
	defer s.mu.Unlock()
	h := s.hooks[webUIUnitName]
	if h == nil {
		return nil
	}
	delete(s.hooks, webUIUnitName)
	for i, name := range s.order {
		if name == webUIUnitName {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
	return h
}

// reattachWebUI registers a preserved, already-running Web UI hook on the
// rebuilt stack and marks it running in the status registry. The hook is
// recorded in s.started-tracking via the order slice but is not (re)started
// by Start, since it never stopped.
func (s *Supervisor) reattachWebUI(h hook) {
	if h == nil {
		return
	}
	s.mu.Lock()
	s.hooks[webUIUnitName] = h
	s.alreadyRunning = map[string]bool{webUIUnitName: true}
	s.mu.Unlock()
	s.reg.Set(status.Unit{Name: webUIUnitName, Kind: status.KindHook, Enabled: true, Running: true})
}

// adoptFrom replaces this supervisor's built components with those from a
// freshly constructed one (used by Apply after Stop). The caller must hold
// no locks; Apply runs Stop/Start which lock internally.
func (s *Supervisor) adoptFrom(other *Supervisor) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg = other.cfg
	s.model = other.model
	s.router = other.router
	s.ports = other.ports
	s.portNames = other.portNames
	s.meters = other.meters
	s.hooks = other.hooks
	s.order = other.order
	s.captureSinks = other.captureSinks
	s.parseCleanup = other.parseCleanup
	s.nbp = other.nbp
	s.shortHook = other.shortHook
	s.macIP = other.macIP
	s.ipxGW = other.ipxGW
	s.netbios = other.netbios
	s.transportBindings = other.transportBindings
	s.started = false
}

// ListInterfaces returns the host's network interfaces for the UI dropdowns,
// each with the pcap device name (stored in config) plus a friendly
// description and addresses. On Windows the device name is a GUID, so the
// description is what makes the dropdown legible. Falls back to bare names
// when device enumeration (which needs Npcap/libpcap) is unavailable.
func (s *Supervisor) ListInterfaces() ([]control.InterfaceInfo, error) {
	devs, err := rawlink.ListPcapDevices()
	if err != nil {
		names, nerr := rawlink.InterfaceNames()
		if nerr != nil {
			return nil, nerr
		}
		out := make([]control.InterfaceInfo, 0, len(names))
		for _, n := range names {
			out = append(out, control.InterfaceInfo{Name: n})
		}
		return out, nil
	}
	out := make([]control.InterfaceInfo, 0, len(devs))
	for _, d := range devs {
		out = append(out, control.InterfaceInfo{
			Name:        d.Name,
			Description: d.Description,
			Addresses:   d.Addresses,
		})
	}
	return out, nil
}

// ListFSTypes returns the AFP filesystem types registered in this build.
func (s *Supervisor) ListFSTypes() []string {
	return registeredFSTypes()
}

// extMapPath resolves the configured AFP extension-map file path, resolving a
// relative path against the config directory exactly as AFP wiring does. It
// returns "" when no extension map is configured.
func (s *Supervisor) extMapPath() string {
	if s.model == nil {
		return ""
	}
	p := s.model.AFP.ExtensionMap
	if p != "" && !filepath.IsAbs(p) && s.source.ConfigDir != "" {
		p = filepath.Join(s.source.ConfigDir, p)
	}
	return p
}

// ReadExtMap returns the configured extension-map path and its current file
// contents. It is used by the management UI's extension-map editor. The path
// is returned even on read error so the UI can show what it tried to open;
// a missing file yields empty content and no error (the operator can create
// it by saving).
func (s *Supervisor) ReadExtMap() (path string, data []byte, err error) {
	path = s.extMapPath()
	if path == "" {
		return "", nil, fmt.Errorf("no AFP extension_map configured")
	}
	data, err = os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return path, nil, nil
	}
	return path, data, err
}

// WriteExtMap validates data as an extension-map file and, if it parses,
// writes it to the configured path (creating a numbered backup of any
// existing file). It returns the backup path created (empty when none).
// The change takes effect on the next configuration Apply, which reloads the
// map; WriteExtMap itself does not restart AFP.
func (s *Supervisor) WriteExtMap(data []byte) (backup string, err error) {
	path := s.extMapPath()
	if path == "" {
		return "", fmt.Errorf("no AFP extension_map configured")
	}
	if err := validateExtMap(data); err != nil {
		return "", err
	}
	return config.SaveBytes(path, data)
}
