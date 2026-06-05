//go:build !windows && !darwin

package main

import (
	"fmt"
	"os"
)

// On Linux (and other non-darwin Unix) there is no LaunchAgent. The daemon
// itself needs no init-system integration — use start/stop/status. For boot
// persistence, point your existing init system at `classicstackd run`. These
// stubs make that explicit rather than coupling to systemd.
func cmdInstall(_ []string) error {
	fmt.Fprintln(os.Stderr, "install is not required on this platform: use `classicstackd start` to run in the background.")
	fmt.Fprintln(os.Stderr, "For boot persistence, add a unit/init script with ExecStart pointing at `classicstackd run -config <path>`.")
	return nil
}

func cmdUninstall(_ []string) error {
	fmt.Fprintln(os.Stderr, "uninstall is not applicable on this platform: stop the daemon with `classicstackd stop`.")
	return nil
}
