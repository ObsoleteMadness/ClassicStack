//go:build windows

package main

import _ "embed"

// trayIconPNG holds the Windows tray icon despite the name (kept consistent
// with icon_darwin.go so main.go can reference trayIconPNG unconditionally):
// fyne.io/systray's SetIcon loads the bytes via LoadImage on Windows, which
// needs a real .ico, not a PNG — this is icons/classicstack.ico, which
// already ships 16x16/32x32 (and larger) resolutions.
//
//go:embed assets/tray-icon.ico
var trayIconPNG []byte
