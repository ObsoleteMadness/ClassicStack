//go:build darwin

package main

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// volumesPlaceholder is the token in the bundled server.toml template that
// gets replaced with the real, per-user Volumes directory path at first run.
const volumesPlaceholder = "__VOLUMES__"

// appSupportDir returns (creating if needed) ~/Library/Application
// Support/ClassicStack, where the tray provisions a per-user config, sample
// share folders, PID file and log for the daemon it auto-starts.
func appSupportDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, "Library", "Application Support", "ClassicStack")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// bundleMacOSDir returns this executable's containing directory
// (Contents/MacOS when running from a proper .app bundle).
func bundleMacOSDir() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return "", err
	}
	return filepath.Dir(exe), nil
}

// bundleResource resolves a file or directory under the app bundle's
// Contents/Resources, relative to this executable's Contents/MacOS.
func bundleResource(name string) (string, error) {
	macOSDir, err := bundleMacOSDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(filepath.Dir(macOSDir), "Resources", name)
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("resource %s not found: %w", name, err)
	}
	return path, nil
}

// daemonPath resolves the bundled classicstackd binary, expected next to
// this executable in Contents/MacOS.
func daemonPath() (string, error) {
	macOSDir, err := bundleMacOSDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(macOSDir, "classicstackd")
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("bundled classicstackd not found: %w", err)
	}
	return path, nil
}

// ensureConfig returns the per-user server.toml path, provisioning it (and
// the sample share folders it points at) from the bundle on first run:
// Contents/Resources/Volumes is copied to <dir>/Volumes, and
// Contents/Resources/server.toml — a starter config with example
// AFP/SMB/NCP/EtherDFS shares — is written to <dir>/server.toml with
// volumesPlaceholder substituted for that real path. A missing config file
// is otherwise fine (cli.Run boots on the built-in default model with zero
// shares), so provisioning only ever happens once; after that the user's
// edits (by hand or via the web admin UI) are left alone.
func ensureConfig(dir string) (string, error) {
	cfgPath := filepath.Join(dir, "server.toml")
	if _, err := os.Stat(cfgPath); err == nil {
		return cfgPath, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}

	volumesSrc, err := bundleResource("Volumes")
	if err != nil {
		return "", err
	}
	volumesDst := filepath.Join(dir, "Volumes")
	if err := copyDir(volumesSrc, volumesDst); err != nil {
		return "", fmt.Errorf("copying sample share folders: %w", err)
	}

	tmplPath, err := bundleResource("server.toml")
	if err != nil {
		return "", err
	}
	tmpl, err := os.ReadFile(tmplPath) // #nosec G304 -- fixed path under our own app bundle, no attacker input
	if err != nil {
		return "", err
	}
	cfg := strings.ReplaceAll(string(tmpl), volumesPlaceholder, volumesDst)
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		return "", err
	}
	return cfgPath, nil
}

// copyDir recursively copies src to dst, creating directories as needed.
func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(path, target)
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src) // #nosec G304 -- fixed path under our own app bundle, no attacker input
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()

	_, err = io.Copy(out, in)
	return err
}

// startDaemon launches the bundled classicstackd as a detached background
// process (`classicstackd start ...`). classicstackd's default PID/log paths
// (/var/run, /var/log) need root, so the tray overrides both to a
// user-writable location under Application Support.
func startDaemon() error {
	dir, err := appSupportDir()
	if err != nil {
		return fmt.Errorf("locating Application Support directory: %w", err)
	}
	cfgPath, err := ensureConfig(dir)
	if err != nil {
		return fmt.Errorf("preparing config: %w", err)
	}
	daemon, err := daemonPath()
	if err != nil {
		return err
	}

	pidFile := filepath.Join(dir, "classicstackd.pid")
	logFile := filepath.Join(dir, "classicstackd.log")
	cmd := exec.Command(daemon, "start", "-config", cfgPath, "-pidfile", pidFile, "-log", logFile) // #nosec G204 -- fixed args + bundled binary path, no attacker input
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("classicstackd start: %v: %s", err, string(out))
	}
	return nil
}
