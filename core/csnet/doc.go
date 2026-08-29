// Package csnet provides shared MAC and IPv4 address parsing/formatting for both
// desktop and TinyGo/embedded builds: ParseMAC/FormatMAC and ParseIPv4/IPv4.
//
// Two implementations exist per concern, selected by the tinygo build tag: the
// default (!tinygo) build wraps the standard library's net package; the tinygo
// build hand-rolls the same operation, since TinyGo's net package does not
// reliably provide ParseMAC/ParseIP on baremetal targets. This mirrors the split
// core/buf and core/hostinfo already use for target-specific behavior (§1) —
// callers use the same API regardless of which build produced it.
//
// RandomMAC deliberately does NOT live here: it needs crypto/rand, which
// transitively imports reflect and is therefore forbidden in the core ring
// (core/internal/archtest, §1). See client/link.RandomMAC instead — every
// current caller (client/etherdfs, client/ncp, client/netbios, client/smb,
// cmd/internal/csconnect) already imports client/link.
package csnet
