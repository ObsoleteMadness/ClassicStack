//go:build darwin

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// launchAgentLabel is the LaunchAgent label / reverse-DNS identifier.
const launchAgentLabel = "com.obsoletemadness.classicstack"

// launchAgentPath returns the per-user LaunchAgent plist path.
func launchAgentPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", launchAgentLabel+".plist"), nil
}

// cmdInstall writes a LaunchAgent plist that runs `classicstackd run -config
// <cfg>` at login (headless) and loads it with launchctl.
func cmdInstall(args []string) error {
	f, err := parseFlags("install", args, true)
	if err != nil {
		return err
	}
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locating executable: %w", err)
	}
	self, err = filepath.Abs(self)
	if err != nil {
		return err
	}

	plistPath, err := launchAgentPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(plistPath), 0o755); err != nil {
		return fmt.Errorf("creating LaunchAgents directory: %w", err)
	}

	plist := renderPlist(self, f.config, f.logFile)
	if err := os.WriteFile(plistPath, []byte(plist), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", plistPath, err)
	}

	// Reload to pick up changes if it was already loaded, then load.
	_ = exec.Command("launchctl", "unload", plistPath).Run()
	if out, err := exec.Command("launchctl", "load", "-w", plistPath).CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl load: %v: %s", err, string(out))
	}

	fmt.Printf("installed LaunchAgent %s (config %s)\n", plistPath, f.config)
	return nil
}

// cmdUninstall unloads and removes the LaunchAgent plist.
func cmdUninstall(_ []string) error {
	plistPath, err := launchAgentPath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(plistPath); err != nil {
		return fmt.Errorf("no LaunchAgent installed at %s", plistPath)
	}
	if out, err := exec.Command("launchctl", "unload", "-w", plistPath).CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: launchctl unload: %v: %s\n", err, string(out))
	}
	if err := os.Remove(plistPath); err != nil {
		return fmt.Errorf("removing %s: %w", plistPath, err)
	}
	fmt.Printf("removed LaunchAgent %s\n", plistPath)
	return nil
}

// renderPlist builds the LaunchAgent plist XML. RunAtLoad starts it at login;
// KeepAlive restarts it if it exits. Output is appended to the log file.
func renderPlist(exePath, cfgPath, logPath string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
		<string>run</string>
		<string>-config</string>
		<string>%s</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
	<key>StandardOutPath</key>
	<string>%s</string>
	<key>StandardErrorPath</key>
	<string>%s</string>
</dict>
</plist>
`, launchAgentLabel, exePath, cfgPath, logPath, logPath)
}
