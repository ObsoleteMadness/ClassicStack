//go:build darwin

package main

import _ "embed"

// trayIconPNG is the menu bar glyph, generated from the ClassicStack app
// icon (icon256.png) via `sips -z 44 44`.
//
//go:embed assets/tray-icon.png
var trayIconPNG []byte
