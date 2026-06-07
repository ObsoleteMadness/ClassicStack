package control

import (
	"context"
	"errors"
)

// ErrNoSupervisor is returned by lifecycle methods when the plane was
// constructed without a Supervisor (e.g. a read-only diagnostic build).
var ErrNoSupervisor = errors.New("control: no supervisor configured")

// ErrNoConfigPath is returned by Save when the plane has no backing file.
var ErrNoConfigPath = errors.New("control: no config path configured; use Export to download")

// Config returns the current effective config and whether there are
// unsaved edits. When edits have been staged the staged model is returned
// so the UI reflects what the operator is editing; otherwise the live
// model is returned. dirty is true whenever staged edits have not been
// written to disk.
func (p *Plane) Config() (cfg ConfigModel, dirty bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.staged != nil {
		return p.staged, p.dirty
	}
	return p.live, p.dirty
}

// Stage records an edited config model in memory without touching disk or
// the running stack. It marks the plane dirty.
func (p *Plane) Stage(edit ConfigModel) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.staged = edit
	p.dirty = true
}

// Apply re-wires the running stack to the staged config. On success the
// staged model becomes the live model. The dirty flag is NOT cleared —
// only writing to disk (Save) clears it — so the UI keeps warning about
// unsaved changes even after a live apply.
func (p *Plane) Apply(ctx context.Context) error {
	p.mu.Lock()
	staged := p.staged
	p.mu.Unlock()
	if staged == nil {
		return nil // nothing staged; no-op
	}
	if p.sup == nil {
		return ErrNoSupervisor
	}
	if err := p.sup.Apply(ctx, staged); err != nil {
		return err
	}
	p.mu.Lock()
	p.live = staged
	p.mu.Unlock()
	return nil
}

// Dirty reports whether there are unsaved edits.
func (p *Plane) Dirty() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.dirty
}

// Export serialises the current (staged-or-live) config to TOML for
// download/backup, regardless of whether a backing file is configured.
func (p *Plane) Export() ([]byte, error) {
	cfg, _ := p.Config()
	if cfg == nil {
		return nil, errors.New("control: no config to export")
	}
	return cfg.ToTOML()
}

// Saver writes a config model to disk and returns the backup path it
// created. config.Save satisfies this; it is injected so pkg/control need
// not import package config's file I/O.
type Saver func(path string, cfg ConfigModel) (backupPath string, err error)

var saveFn Saver

// SetSaver installs the function Save uses to persist config. main.go wires
// config.Save here at startup.
func SetSaver(s Saver) { saveFn = s }

// Save writes the current config to the backing file (backing up the
// previous file first) and clears the dirty flag. The staged model, if
// any, becomes live.
func (p *Plane) Save() (backupPath string, err error) {
	p.mu.Lock()
	cfg := p.staged
	if cfg == nil {
		cfg = p.live
	}
	path := p.path
	p.mu.Unlock()

	if path == "" {
		return "", ErrNoConfigPath
	}
	if saveFn == nil {
		return "", errors.New("control: no saver configured")
	}
	backupPath, err = saveFn(path, cfg)
	if err != nil {
		return "", err
	}
	p.mu.Lock()
	p.live = cfg
	p.staged = nil
	p.dirty = false
	p.mu.Unlock()
	return backupPath, nil
}
