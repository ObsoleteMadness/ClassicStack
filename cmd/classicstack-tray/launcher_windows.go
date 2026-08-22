//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// appSupportDir returns (creating if needed) %LOCALAPPDATA%\ClassicStack,
// where the tray keeps a per-user config and log for the process it
// auto-starts — %LOCALAPPDATA% is always writable by the current user
// without elevation, unlike the C:\ProgramData path the classicstack-svc.exe
// README documents for a proper elevated service install.
func appSupportDir() (string, error) {
	root := os.Getenv("LOCALAPPDATA")
	if root == "" {
		cfgDir, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		root = cfgDir
	}
	dir := filepath.Join(root, "ClassicStack")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// bundleDir returns this executable's containing directory.
func bundleDir() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Dir(exe), nil
}

// daemonPath resolves classicstack-svc.exe, expected next to this
// executable (the Windows release zip already bundles it alongside the
// interactive classicstack.exe — see scripts/ci/package-release.ps1).
func daemonPath() (string, error) {
	dir, err := bundleDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "classicstack-svc.exe")
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("classicstack-svc.exe not found next to classicstack-tray.exe: %w", err)
	}
	return path, nil
}

// startDaemon launches classicstack-svc.exe directly in its console/debug
// "run" mode as a detached, windowless background process — the Windows
// equivalent of how the macOS tray runs `classicstackd start` (also no
// elevation, no OS-level service registration). This is deliberately
// distinct from installing ClassicStack as a real Windows Service
// (`classicstack-svc.exe install`, documented in README.md): that needs an
// elevated SCM connection (cmd/classicstack-svc/main_windows.go connects via
// "run as Administrator") and is left as a separate, manual step for anyone
// who wants boot-time auto-start — exactly like classicstackd's LaunchAgent
// on macOS isn't installed by the tray either. Once running (by either
// path), Restart/Shutdown/Status all go through the same HTTP control API
// regardless of how the process was started.
func startDaemon() error {
	dir, err := appSupportDir()
	if err != nil {
		return fmt.Errorf("locating %%LOCALAPPDATA%%\\ClassicStack: %w", err)
	}
	cfgPath := filepath.Join(dir, "server.toml")
	daemon, err := daemonPath()
	if err != nil {
		return err
	}

	logFile, err := os.OpenFile(filepath.Join(dir, "classicstack-svc.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644) // #nosec G304 -- fixed path under our own per-user app data dir
	if err != nil {
		return fmt.Errorf("opening log file: %w", err)
	}
	defer func() { _ = logFile.Close() }()

	cmd := exec.Command(daemon, "run", "-config", cfgPath) // #nosec G204 -- fixed args + bundled binary path, no attacker input
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	// HideWindow suppresses the console window classicstack-svc.exe would
	// otherwise briefly flash (it's a console-subsystem binary); the process
	// itself is unaffected — Windows doesn't tie a child's lifetime to its
	// parent's, so it keeps running after the tray exits without needing a
	// Setsid-style detach the way classicstackd does on Unix.
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting classicstack-svc.exe: %w", err)
	}
	_ = cmd.Process.Release()
	return nil
}
