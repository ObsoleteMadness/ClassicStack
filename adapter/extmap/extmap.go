// Package extmap is the adapter-edge file surface for the AFP extension map: read,
// validate, and write the Netatalk-style type/creator file an operator edits through
// the web UI. The PARSING + the on-disk format live in core/service/afp (the service
// that consumes the map); this package adds the file I/O (read/write + a numbered
// backup) that core does not do, and validation by round-tripping through the afp
// parser so a typo cannot produce a file AFP later fails to load.
//
// Ring: ADAPTER (it touches the filesystem). It is used by the HTTP control adapter's
// /extmap handlers; a path is server-local, so this is an HTTP-server-side concern, not
// part of the transport-agnostic control.Client contract (like first-run /setup).
package extmap

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/service/afp"
)

// Read returns the raw bytes of the extension-map file at path. A missing file yields
// empty content with no error (the UI shows an empty grid the operator can fill in).
func Read(path string) ([]byte, error) {
	// path is the operator-configured extension-map file (server.toml / UI),
	// i.e. trusted input, not an attacker-controlled request parameter.
	data, err := os.ReadFile(path) // #nosec G304 -- operator-configured config path
	if os.IsNotExist(err) {
		return nil, nil
	}
	return data, err
}

// Save validates content (it must parse as a Netatalk extension map) and writes it to
// path, first moving any existing file to a numbered backup (path.N) so an edit is
// recoverable. Returns the backup path written, or "" when there was no prior file.
func Save(path string, content []byte) (backup string, err error) {
	if err := afp.ValidateExtensionMap(content); err != nil {
		return "", err
	}
	if _, statErr := os.Stat(path); statErr == nil {
		backup = nextBackupPath(path)
		if err := os.Rename(path, backup); err != nil {
			return "", fmt.Errorf("extmap: backup %s: %w", path, err)
		}
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		return backup, fmt.Errorf("extmap: write %s: %w", path, err)
	}
	return backup, nil
}

// nextBackupPath returns the first unused "path.N" backup name (N starting at 1), so a
// burst of saves does not clobber earlier backups. Falls back to a timestamp suffix if
// the numbered slots are somehow exhausted.
func nextBackupPath(path string) string {
	for i := 1; i < 1000; i++ {
		cand := fmt.Sprintf("%s.%d", path, i)
		if _, err := os.Stat(cand); os.IsNotExist(err) {
			return cand
		}
	}
	return path + "." + filepath.Base(time.Now().Format("20060102-150405"))
}
