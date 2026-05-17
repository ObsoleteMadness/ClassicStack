package smb

// ShareConfig defines a single SMB share. The fs_type field selects a
// pkg/vfs backend (e.g. "local_fs", "macgarden"); when blank, the SMB
// service falls back to "local_fs". The shape mirrors AFP's
// VolumeConfig deliberately so per-volume TOML tables look the same
// across services.
type ShareConfig struct {
	Name     string `koanf:"name"`
	Path     string `koanf:"path"`
	FSType   string `koanf:"fs_type"`
	ReadOnly bool   `koanf:"read_only"`
}
