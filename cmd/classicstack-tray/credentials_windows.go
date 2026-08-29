//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/danieljoos/wincred"
)

// Restart/Shutdown go through the same authGate as the web admin UI
// (adapter/control/http/auth.go): once an admin exists, every control route
// needs HTTP Basic credentials. The tray has no login screen of its own, so
// it borrows two standard Windows pieces instead of building one: the
// Credential Manager (via github.com/danieljoos/wincred) to remember the
// admin credential across restarts, and a small PowerShell/WinForms dialog
// to ask for it the first time or after a rejected password. PowerShell is
// already a stock Windows component — this avoids a cgo/native GUI
// dependency, mirroring how the macOS build shells out to osascript.

const credentialTarget = "ClassicStack Tray"

// loadCredentials reads the saved admin credential from Windows Credential
// Manager, if one was saved by a prior run.
func loadCredentials() (user, pass string, ok bool) {
	cred, err := wincred.GetGenericCredential(credentialTarget)
	if err != nil || cred.UserName == "" {
		return "", "", false
	}
	return cred.UserName, string(cred.CredentialBlob), true
}

// saveCredentials stores the admin credential in Windows Credential Manager,
// replacing any previously saved value.
func saveCredentials(user, pass string) error {
	cred := wincred.NewGenericCredential(credentialTarget)
	cred.UserName = user
	cred.CredentialBlob = []byte(pass)
	cred.Persist = wincred.PersistLocalMachine
	if err := cred.Write(); err != nil {
		return fmt.Errorf("saving credentials to Windows Credential Manager: %w", err)
	}
	return nil
}

// forgetCredentials removes a saved (evidently wrong) credential.
func forgetCredentials() {
	if cred, err := wincred.GetGenericCredential(credentialTarget); err == nil {
		_ = cred.Delete()
	}
}

// promptDialogScript is a small WinForms login prompt run via PowerShell —
// see runPowerShellScript for how $Reason/output are wired.
const promptDialogScript = `
param([string]$Reason)
Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing

$form = New-Object System.Windows.Forms.Form
$form.Text = "ClassicStack"
$form.Size = New-Object System.Drawing.Size(380,230)
$form.StartPosition = "CenterScreen"
$form.TopMost = $true
$form.FormBorderStyle = "FixedDialog"
$form.MaximizeBox = $false
$form.MinimizeBox = $false

$labelReason = New-Object System.Windows.Forms.Label
$labelReason.Location = New-Object System.Drawing.Point(10,10)
$labelReason.Size = New-Object System.Drawing.Size(350,40)
$labelReason.Text = $Reason
$form.Controls.Add($labelReason)

$labelUser = New-Object System.Windows.Forms.Label
$labelUser.Location = New-Object System.Drawing.Point(10,60)
$labelUser.Size = New-Object System.Drawing.Size(120,20)
$labelUser.Text = "Admin username:"
$form.Controls.Add($labelUser)

$textUser = New-Object System.Windows.Forms.TextBox
$textUser.Location = New-Object System.Drawing.Point(140,58)
$textUser.Size = New-Object System.Drawing.Size(210,20)
$form.Controls.Add($textUser)

$labelPass = New-Object System.Windows.Forms.Label
$labelPass.Location = New-Object System.Drawing.Point(10,90)
$labelPass.Size = New-Object System.Drawing.Size(120,20)
$labelPass.Text = "Admin password:"
$form.Controls.Add($labelPass)

$textPass = New-Object System.Windows.Forms.TextBox
$textPass.Location = New-Object System.Drawing.Point(140,88)
$textPass.Size = New-Object System.Drawing.Size(210,20)
$textPass.UseSystemPasswordChar = $true
$form.Controls.Add($textPass)

$okButton = New-Object System.Windows.Forms.Button
$okButton.Location = New-Object System.Drawing.Point(150,150)
$okButton.Size = New-Object System.Drawing.Size(90,30)
$okButton.Text = "OK"
$okButton.DialogResult = [System.Windows.Forms.DialogResult]::OK
$form.AcceptButton = $okButton
$form.Controls.Add($okButton)

$cancelButton = New-Object System.Windows.Forms.Button
$cancelButton.Location = New-Object System.Drawing.Point(250,150)
$cancelButton.Size = New-Object System.Drawing.Size(90,30)
$cancelButton.Text = "Cancel"
$cancelButton.DialogResult = [System.Windows.Forms.DialogResult]::Cancel
$form.CancelButton = $cancelButton
$form.Controls.Add($cancelButton)

$form.Add_Shown({ $textUser.Focus() })
$result = $form.ShowDialog()
if ($result -eq [System.Windows.Forms.DialogResult]::OK) {
    Write-Output ($textUser.Text + "` + "`t" + `" + $textPass.Text)
    exit 0
}
exit 1
`

// alertDialogScript shows a native message box — see runPowerShellScript.
const alertDialogScript = `
param([string]$Title, [string]$Message)
Add-Type -AssemblyName System.Windows.Forms
[System.Windows.Forms.MessageBox]::Show($Message, $Title, ` +
	`[System.Windows.Forms.MessageBoxButtons]::OK, [System.Windows.Forms.MessageBoxIcon]::Error) | Out-Null
`

// promptCredentials asks for the ClassicStack admin username/password via a
// native dialog. ok is false if the user cancelled.
func promptCredentials(reason string) (user, pass string, ok bool) {
	out, err := runPowerShellScript(promptDialogScript, reason)
	if err != nil {
		return "", "", false
	}
	parts := strings.SplitN(strings.TrimRight(out, "\r\n"), "\t", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// showAlert surfaces an error to the user via a native dialog, since a tray
// app has no window to print to.
func showAlert(title, message string) {
	_, _ = runPowerShellScript(alertDialogScript, title, message)
}

// runPowerShellScript runs script (a WinForms dialog) via a temp .ps1 file
// under -STA (WinForms requires a single-threaded apartment) with args bound
// to the script's param() block, returning trimmed stdout. A non-zero exit
// (Cancel, or PowerShell itself unavailable) is reported as an error.
func runPowerShellScript(script string, args ...string) (string, error) {
	f, err := os.CreateTemp("", "classicstack-tray-*.ps1")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if _, err := f.WriteString(script); err != nil {
		_ = f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}

	cmdArgs := append([]string{"-NoProfile", "-STA", "-ExecutionPolicy", "Bypass", "-File", f.Name()}, args...) // #nosec G204 -- fixed flags + our own temp script + fixed strings, no attacker input
	out, err := exec.Command("powershell", cmdArgs...).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}
