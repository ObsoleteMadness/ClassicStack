// Package file is the file-backed config Store adapter with numbered backups (§4).
// Save writes the bytes to the configured path after rotating the previous file
// to a numbered backup (path.1, path.2, …), so an apply is recoverable. It lives
// in the ADAPTER ring (implements core/config.Store).
package file

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/ObsoleteMadness/ClassicStack/core/config"
)

// Store reads and writes config bytes at Path, keeping numbered backups on Save.
type Store struct {
	// Path is the config file (e.g. "server.toml").
	Path string
	// MaxBackups caps the numbered backup chain (0 → defaultMaxBackups). The
	// oldest is dropped when the chain is full.
	MaxBackups int
	// Perm is the file mode for new writes (0 → 0o644).
	Perm os.FileMode
}

// New returns a file store for path with default backup depth and permissions.
func New(path string) *Store {
	return &Store{Path: path}
}

// compile-time assertion: *Store satisfies config.Store.
var _ config.Store = (*Store)(nil)

const defaultMaxBackups = 9

// Load reads the current config bytes. A missing file is not an error — it
// returns (nil, nil) so the caller can fall back to defaults.
func (s *Store) Load() ([]byte, error) {
	data, err := os.ReadFile(s.Path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return data, nil
}

// Save rotates the existing file into a numbered backup and writes data to Path.
// The returned revision is the backup path the prior contents moved to (empty on
// the very first save, when there was nothing to back up).
func (s *Store) Save(data []byte) (revision string, err error) {
	perm := s.Perm
	if perm == 0 {
		perm = 0o644
	}
	max := s.MaxBackups
	if max <= 0 {
		max = defaultMaxBackups
	}

	revision, err = s.rotate(max)
	if err != nil {
		return "", err
	}

	if dir := filepath.Dir(s.Path); dir != "" {
		// 0750: config stores may hold credentials, so the containing
		// directory should not be world-readable.
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return "", err
		}
	}
	if err := os.WriteFile(s.Path, data, perm); err != nil {
		return "", err
	}
	return revision, nil
}

// rotate shifts path.N→path.N+1 (dropping the oldest beyond max) and moves the
// current file to path.1. It returns the backup path the current file became, or
// "" if there is no current file to back up.
func (s *Store) rotate(max int) (revision string, err error) {
	if _, statErr := os.Stat(s.Path); os.IsNotExist(statErr) {
		return "", nil // nothing to back up yet
	} else if statErr != nil {
		return "", statErr
	}

	// Drop the oldest, then shift the chain up by one.
	oldest := s.backupPath(max)
	if err := os.Remove(oldest); err != nil && !os.IsNotExist(err) {
		return "", err
	}
	for i := max - 1; i >= 1; i-- {
		from := s.backupPath(i)
		to := s.backupPath(i + 1)
		if _, statErr := os.Stat(from); statErr == nil {
			if err := os.Rename(from, to); err != nil {
				return "", err
			}
		}
	}
	to := s.backupPath(1)
	if err := os.Rename(s.Path, to); err != nil {
		return "", err
	}
	return to, nil
}

// backupPath returns the Nth numbered backup path ("server.toml.1").
func (s *Store) backupPath(n int) string {
	return fmt.Sprintf("%s.%s", s.Path, strconv.Itoa(n))
}
