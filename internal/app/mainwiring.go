package app

import (
	"github.com/ObsoleteMadness/ClassicStack/config"
	"github.com/ObsoleteMadness/ClassicStack/pkg/control"
	"github.com/ObsoleteMadness/ClassicStack/pkg/logbuf"
	"github.com/ObsoleteMadness/ClassicStack/pkg/metrics"
	"github.com/ObsoleteMadness/ClassicStack/pkg/status"
)

// afpFlagOptions carries the AFP values from the CLI flags so buildModel can
// fold them into the config model on the flag-driven path (where the model
// is not loaded from a TOML source).
type afpFlagOptions struct {
	ServerName      string
	Zone            string
	Protocols       string
	Binding         string
	ExtensionMap    string
	DecomposedNames bool
	CNIDBackend     string
	AppleDoubleMode string
	Volumes         []string // raw "Name:Path" entries
}

// buildModel produces the serialisable config.Model that the management
// plane edits. When the configuration came from a TOML file, the model is
// loaded directly from the source so it captures everything (including AFP
// and SMB volume maps). On the flag-driven path the model is projected from
// the resolved appConfig and the AFP flag values are folded in.
func buildModel(cfg appConfig, src config.Source, fromConfigFile bool, afp afpFlagOptions) *config.Model {
	if fromConfigFile && src.K != nil {
		return config.FromSource(src)
	}
	m := modelFromAppConfig(cfg)
	applyAFPFlags(m, afp)
	return m
}

// applyAFPFlags folds CLI AFP flag values into the model's [AFP] section.
func applyAFPFlags(m *config.Model, afp afpFlagOptions) {
	m.AFP.Name = orDefault(afp.ServerName, m.AFP.Name)
	m.AFP.Zone = orDefault(afp.Zone, m.AFP.Zone)
	m.AFP.Protocols = orDefault(afp.Protocols, m.AFP.Protocols)
	m.AFP.Binding = orDefault(afp.Binding, m.AFP.Binding)
	m.AFP.ExtensionMap = orDefault(afp.ExtensionMap, m.AFP.ExtensionMap)
	m.AFP.CNIDBackend = orDefault(afp.CNIDBackend, m.AFP.CNIDBackend)
	m.AFP.UseDecomposedNames = afp.DecomposedNames
	m.AFP.AppleDoubleMode = orDefault(afp.AppleDoubleMode, m.AFP.AppleDoubleMode)
	if len(afp.Volumes) > 0 {
		if m.AFP.Volumes == nil {
			m.AFP.Volumes = map[string]config.VolumeModel{}
		}
		for _, raw := range afp.Volumes {
			parts := splitColon(raw)
			if len(parts) == 2 {
				m.AFP.Volumes[parts[0]] = config.VolumeModel{Name: parts[0], Path: parts[1], FSType: "local_fs"}
			}
		}
	}
}

// newControlPlane constructs the management plane over the supervisor and
// installs config.Save as the plane's saver. configPath may be empty (flag
// runs), in which case Save is disabled and the UI offers Download only.
func newControlPlane(sup *Supervisor, model *config.Model, configPath string) *control.Plane {
	// Tee the metrics hub into the expvar sink so streamed counters remain
	// visible at /debug/vars in addition to the SSE stream.
	metrics.Default.AddSink(metrics.NewExpvarSink())

	control.SetSaver(func(path string, cfg control.ConfigModel) (string, error) {
		m, ok := cfg.(*config.Model)
		if !ok {
			return "", control.ErrNoConfigPath
		}
		return config.Save(path, m)
	})

	return control.New(control.Deps{
		Supervisor: sup,
		Registry:   status.Default,
		Hub:        metrics.Default,
		Logs:       logbuf.Default,
		Config:     model,
		ConfigPath: configPath,
	})
}

// installWebUI constructs the web UI hook (a no-op stub in builds without
// -tags webui) and registers it with the supervisor so it shares the
// stack's lifecycle. The hook is added even when disabled so a future
// enable-via-UI can start it; the hook itself no-ops when off.
func installWebUI(sup *Supervisor, opts WebUIConfigOptions, plane *control.Plane) error {
	if err := opts.Validate(); err != nil {
		return err
	}
	h, err := wireWebUI(WebUIWiring{Options: opts, Plane: plane})
	if err != nil {
		return err
	}
	sup.AddExternalHook("WebUI", h, opts.Enabled)
	return nil
}
