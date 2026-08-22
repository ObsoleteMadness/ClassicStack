//go:build darwin

package main

import (
	"fmt"
	"os"
	"os/exec"
)

func openInterface(baseURL string) {
	if err := exec.Command("open", baseURL).Start(); err != nil { // #nosec G204 -- fixed "open" + our own control base URL, not attacker input
		fmt.Fprintf(os.Stderr, "classicstack-tray: opening %s failed: %v\n", baseURL, err)
	}
}

// recoveryHint points the user at where to look when Start/Shutdown didn't
// converge in time.
func recoveryHint() string {
	return "check ~/Library/Application Support/ClassicStack/classicstackd.log " +
		"and `launchctl list | grep classicstack` (a classicstackd LaunchAgent with KeepAlive would explain a shutdown that doesn't stick)."
}
