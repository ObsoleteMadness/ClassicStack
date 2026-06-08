// Package buf holds per-target buffer-size constants implementing the §1
// allocation discipline: small values on tinygo/embedded, large on desktop.
// One file per target build tag plus a default.
//
// Ring: CORE (stdlib only). Real consts land in step A3.
package buf
