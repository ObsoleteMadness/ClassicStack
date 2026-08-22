//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
)

func openInterface(baseURL string) {
	// rundll32 url.dll,FileProtocolHandler is the standard way to hand a URL
	// to the default browser without going through cmd.exe/start's quoting
	// quirks (start treats the first quoted arg as a window title).
	if err := exec.Command("rundll32", "url.dll,FileProtocolHandler", baseURL).Start(); err != nil { // #nosec G204 -- fixed rundll32 verb + our own control base URL, not attacker input
		fmt.Fprintf(os.Stderr, "classicstack-tray: opening %s failed: %v\n", baseURL, err)
	}
}

// recoveryHint points the user at where to look when Start/Shutdown didn't
// converge in time.
func recoveryHint() string {
	return "check %LOCALAPPDATA%\\ClassicStack\\classicstack-svc.log and the Application event log " +
		"(Get-EventLog -LogName Application -Source ClassicStack) — or, if it's installed as a Windows " +
		"service with automatic recovery, `sc.exe qfailure ClassicStack`."
}
