//go:build darwin

package main

import "os/exec"

// showNotification raises a native banner via osascript's `display
// notification` — a Notification Center banner, distinct from showAlert's
// modal dialog.
//
// activateURL is unused here: AppleScript's `display notification` has no
// click-activation hook (Apple removed that surface outside a properly
// signed app using UNUserNotificationCenter, which needs Objective-C/Cocoa
// bridging this build deliberately avoids — see the cgo-free rationale in
// credentials_darwin.go). Clicking the banner just dismisses it; opening the
// web UI still needs the tray's own "Open Interface" item. This is a real,
// disclosed limitation, not an oversight — see notify_windows.go for the
// platform where click-to-open is actually achievable.
func showNotification(title, message, activateURL string) {
	_ = activateURL
	script := `display notification ` + quoteAppleScriptString(message) +
		` with title ` + quoteAppleScriptString(title)
	_ = exec.Command("osascript", "-e", script).Run() // #nosec G204 -- fixed script text with escaped, non-attacker-controlled parameters
}
