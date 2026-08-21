//go:build darwin

package main

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// Restart/Shutdown go through the same authGate as the web admin UI
// (adapter/control/http/auth.go): once an admin exists, every control route
// needs HTTP Basic credentials. The tray has no login screen of its own, so
// it borrows two standard macOS pieces instead of building one: the Keychain
// (via the `security` CLI) to remember the admin credential across restarts,
// and an AppleScript dialog (via `osascript`) to ask for it the first time
// or after a rejected password.

const (
	keychainService = "ClassicStack Tray"
	keychainAccount = "classicstack-admin"
)

// keychainCredential is the JSON blob stored as the Keychain item's password
// field, holding both the admin username and password.
type keychainCredential struct {
	User string `json:"user"`
	Pass string `json:"pass"`
}

// loadCredentials reads the saved admin credential from the login Keychain,
// if one was saved by a prior run.
func loadCredentials() (user, pass string, ok bool) {
	out, err := exec.Command("security", "find-generic-password",
		"-s", keychainService, "-a", keychainAccount, "-w").Output() // #nosec G204 -- fixed args, no attacker input
	if err != nil {
		return "", "", false
	}
	var cred keychainCredential
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(out))), &cred); err != nil {
		return "", "", false
	}
	return cred.User, cred.Pass, cred.User != ""
}

// saveCredentials stores the admin credential in the login Keychain,
// replacing any previously saved value.
func saveCredentials(user, pass string) error {
	blob, err := json.Marshal(keychainCredential{User: user, Pass: pass})
	if err != nil {
		return err
	}
	cmd := exec.Command("security", "add-generic-password", // #nosec G204 -- fixed args + our own JSON blob, no attacker input
		"-s", keychainService, "-a", keychainAccount, "-w", string(blob), "-U")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("saving credentials to Keychain: %v: %s", err, string(out))
	}
	return nil
}

// forgetCredentials removes a saved (evidently wrong) credential.
func forgetCredentials() {
	_ = exec.Command("security", "delete-generic-password", // #nosec G204 -- fixed args, no attacker input
		"-s", keychainService, "-a", keychainAccount).Run()
}

// promptCredentials asks for the ClassicStack admin username/password via a
// native dialog. ok is false if the user cancelled.
func promptCredentials(reason string) (user, pass string, ok bool) {
	script := fmt.Sprintf(`
set theReason to %s
set theUser to text returned of (display dialog theReason & return & return & "ClassicStack admin username:" default answer "" with title "ClassicStack")
set thePass to text returned of (display dialog "ClassicStack admin password:" default answer "" with hidden answer true with title "ClassicStack")
return theUser & linefeed & thePass
`, quoteAppleScriptString(reason))

	out, err := exec.Command("osascript", "-e", script).Output() // #nosec G204 -- fixed script text with one escaped, non-attacker-controlled parameter
	if err != nil {
		return "", "", false
	}
	lines := strings.SplitN(strings.TrimRight(string(out), "\n"), "\n", 2)
	if len(lines) != 2 {
		return "", "", false
	}
	return lines[0], lines[1], true
}

// showAlert surfaces an error to the user via a native alert, since a tray
// app has no window to print to.
func showAlert(title, message string) {
	script := fmt.Sprintf(`display alert %s message %s as critical`,
		quoteAppleScriptString(title), quoteAppleScriptString(message))
	_ = exec.Command("osascript", "-e", script).Run() // #nosec G204 -- fixed script text with escaped, non-attacker-controlled parameters
}

// quoteAppleScriptString renders s as a double-quoted AppleScript string
// literal, escaping backslashes and quotes.
func quoteAppleScriptString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}
