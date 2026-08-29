// Package csnet provides shared MAC and IPv4 address parsing/formatting for both
// desktop and TinyGo/embedded builds: ParseMAC/FormatMAC, ParseIPv4/IPv4, and
// RandomMAC.
//
// ParseMAC and ParseIPv4 have two implementations each, selected by the tinygo
// build tag: the default (!tinygo) build wraps the standard library's net
// package; the tinygo build hand-rolls the same operation, since TinyGo's net
// package does not reliably provide ParseMAC/ParseIP on baremetal targets. This
// mirrors the split core/buf and core/hostinfo already use for target-specific
// behavior (§1) — callers use the same API regardless of which build produced
// it. RandomMAC needs no such split: crypto/rand builds and links fine under
// TinyGo (see random.go's doc comment for why it's allowed in core at all).
//
// See docs/build.md's "Embedded target tags" table for the full tinygo/pico/
// esp32 build-tag family this split belongs to.
package csnet
