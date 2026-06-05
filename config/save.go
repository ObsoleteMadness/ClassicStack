package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// Save writes the model to path as TOML. If path already exists it is first
// duplicated to the next free numbered backup (e.g. server.toml.0001,
// server.toml.0002, …) so a hand-edited file is never lost. The new file
// is written atomically via a temp file in the same directory followed by
// a rename. It returns the backup path created (empty when path did not
// previously exist).
func Save(path string, m *Model) (backupPath string, err error) {
	data, err := m.ToTOML()
	if err != nil {
		return "", fmt.Errorf("marshal config: %w", err)
	}

	if _, statErr := os.Stat(path); statErr == nil {
		backupPath, err = backupExisting(path)
		if err != nil {
			return "", err
		}
	} else if !os.IsNotExist(statErr) {
		return "", statErr
	}

	if err := atomicWrite(path, data); err != nil {
		return "", err
	}
	return backupPath, nil
}

// SaveBytes writes data to path, first duplicating any existing file to the
// next free numbered backup (path.NNNN), exactly like Save but for an
// arbitrary text file (e.g. the AFP extension map) rather than the TOML model.
// The write is atomic via a temp file + rename. It returns the backup path
// created (empty when path did not previously exist).
func SaveBytes(path string, data []byte) (backupPath string, err error) {
	if _, statErr := os.Stat(path); statErr == nil {
		backupPath, err = backupExisting(path)
		if err != nil {
			return "", err
		}
	} else if !os.IsNotExist(statErr) {
		return "", statErr
	}
	if err := atomicWrite(path, data); err != nil {
		return "", err
	}
	return backupPath, nil
}

// backupExisting copies path to the next free path.NNNN and returns the
// backup path.
func backupExisting(path string) (string, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	for i := 1; i <= 9999; i++ {
		candidate := fmt.Sprintf("%s.%04d", path, i)
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			if err := os.WriteFile(candidate, src, 0o600); err != nil {
				return "", err
			}
			return candidate, nil
		} else if err != nil {
			return "", err
		}
	}
	return "", fmt.Errorf("config: exhausted backup slots for %s", path)
}

// atomicWrite writes data to a temp file in path's directory and renames it
// over path so a crash mid-write cannot leave a truncated config.
func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".classicstack-config-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op once renamed

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
