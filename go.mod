module github.com/ObsoleteMadness/ClassicStack

go 1.25.12

require (
	fyne.io/systray v1.12.2
	github.com/danieljoos/wincred v1.2.3
	github.com/fsnotify/fsnotify v1.9.0
	github.com/google/gopacket v1.1.19
	github.com/jacobsa/go-serial v0.0.0-20180131005756-15cf729a72d4
	github.com/pelletier/go-toml/v2 v2.2.4
	golang.org/x/net v0.55.0
	golang.org/x/sys v0.45.0
	modernc.org/sqlite v1.35.0
)

require (
	github.com/godbus/dbus/v5 v5.1.0 // indirect
)

require (
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/ncruces/go-strftime v0.1.9 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/winfsp/cgofuse v1.6.0
	github.com/winfsp/go-winfsp v1.0.3
	golang.org/x/exp v0.0.0-20240119083558-1b970713d09a // indirect
	golang.org/x/mod v0.35.0 // indirect
	golang.org/x/tools v0.44.0 // indirect
	modernc.org/libc v1.61.13 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.8.2 // indirect
)

// Local patch: expose FileInfoTimeout Option (upstream leaves it at 0).
replace github.com/winfsp/go-winfsp => ./third_party/go-winfsp

// Local patch: Darwin getxattr/setxattr position for com.apple.ResourceFork.
replace github.com/winfsp/cgofuse => ./third_party/cgofuse
