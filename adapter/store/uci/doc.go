// Package uci is the OpenWRT UCI-tree config Store adapter (shelling uci /
// /etc/config on-target; a file-backed fake off-target for tests) (§4).
//
// Ring: ADAPTER (implements core/config.Store). Real impl lands in step D6.
package uci
