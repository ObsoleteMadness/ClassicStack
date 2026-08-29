package uci

import (
	"os"
	"os/exec"
	"path/filepath"

	"github.com/ObsoleteMadness/ClassicStack/core/config"
)

// Store reads and writes config via the OpenWRT UCI subsystem.
type Store struct {
	// CfgPath is the file path to read/write off-target or as direct write path.
	// Defaults to /etc/config/classicstack.
	CfgPath string
	// uciCmd is the uci binary path ("uci").
	uciCmd string
}

// New returns a new uci store. If cfgPath is empty, it defaults to /etc/config/classicstack.
func New(cfgPath string) *Store {
	if cfgPath == "" {
		cfgPath = "/etc/config/classicstack"
	}
	return &Store{
		CfgPath: cfgPath,
		uciCmd:  "uci",
	}
}

// compile-time assertion: *Store satisfies config.Store.
var _ config.Store = (*Store)(nil)

// Load reads config. Runs 'uci export classicstack' if uci is present, else reads CfgPath.
func (s *Store) Load() ([]byte, error) {
	if s.hasUCI() {
		// Fixed binary ("uci") and literal arguments; no external input flows
		// into the command line.
		out, err := exec.Command(s.uciCmd, "export", "classicstack").Output() // #nosec G204 -- fixed command and literal args

		if err == nil {
			return out, nil
		}
		// If command failed (e.g. package not imported yet), fall back to file read.
	}

	data, err := os.ReadFile(s.CfgPath)
	if os.IsNotExist(err) {
		return nil, nil // return nil, nil on missing file as expected by core
	}
	return data, err
}

// Save writes config. Writes to CfgPath, and runs 'uci commit classicstack' if uci is present.
func (s *Store) Save(data []byte) (string, error) {
	if dir := filepath.Dir(s.CfgPath); dir != "" {
		// 0750: UCI config may hold credentials; keep the directory
		// non-world-readable. (On OpenWrt /etc/config is root-owned anyway.)
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return "", err
		}
	}
	// 0644 matches OpenWrt's /etc/config convention (UCI files are root-owned
	// and world-readable by design); the uci tool itself expects this mode.
	if err := os.WriteFile(s.CfgPath, data, 0o644); err != nil { // #nosec G306 -- matches /etc/config convention
		return "", err
	}

	if s.hasUCI() {
		// Run uci commit classicstack to apply/validate the changes on-target.
		// Fixed binary and literal arguments; no external input on the cmd line.
		_ = exec.Command(s.uciCmd, "commit", "classicstack").Run() // #nosec G204 -- fixed command and literal args
	}

	return s.CfgPath, nil
}

func (s *Store) hasUCI() bool {
	_, err := exec.LookPath(s.uciCmd)
	return err == nil
}
