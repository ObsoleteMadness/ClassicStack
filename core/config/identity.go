package config

import (
	"errors"
	"strings"
)

// IdentityKey is the well-known section key for server identity. Identity is a typed
// field on Model (like Logging/Router/Bridge), not a registered component section, so
// this key is the codec/UI handle, not a Sections map entry.
const IdentityKey = "Identity"

// NetBIOSNameMaxLen is the NetBIOS name limit (15 bytes + a 1-byte suffix the
// protocol layer adds). It is a CONSUMER constraint (NetBIOS), applied to Identity
// only when the NetBIOS service is enabled — see Identity.ValidateForNetBIOS (§4-bis).
const NetBIOSNameMaxLen = 15

// Identity is the server's cross-cutting identity: one source of truth consumed by
// SMB, NetBIOS, and the browser, owned by NO single service (§4-bis). It is a
// well-known top-level section of the Model (alongside Logging/Router/Bridge).
//
// The trap it removes: NetBIOS used to take a server name in its constructor while
// SMB carried an independent workgroup and had no server-name field — nothing
// connected them. Here Hostname/Workgroup/Description live in ONE place and the
// registry hands the same values to whichever consumers are enabled, so they cannot
// diverge. A NetBIOS-less deployment (SMB on direct-TCP :445, or AFP-only) still has a
// Hostname — SMB advertises it in NEGOTIATE with no NetBIOS layer present.
type Identity struct {
	// Hostname is the server name. SMB advertises it (even over direct-TCP :445 with
	// NO NetBIOS); NetBIOS claims it as its workstation/file-server name when running;
	// the browser announces it. Empty → a consumer derives a default (SMB/browser fall
	// back to "CLASSICSTACK"). The NetBIOS ≤15-byte/upper-case rule is a CONSUMER
	// constraint (ValidateForNetBIOS), not intrinsic to the field.
	Hostname string `toml:"hostname"`
	// Workgroup is the SMB NEGOTIATE domain and the browser DomainAnnounce group.
	// Default WORKGROUP. NetBIOS-flavoured but, like Hostname, used by SMB without
	// NetBIOS.
	Workgroup string `toml:"workgroup"`
	// Description is the human server comment: SMB's server remark (the comment in a
	// NetServerEnum2 SERVER_INFO_1 record / the browser self-announcement comment), as
	// shown in a Windows browse list next to the server name. Optional; empty = no
	// comment. Not NetBIOS-constrained (it is a free-text comment, not a name).
	Description string `toml:"description"`
}

// ErrHostnameInvalid is returned by Identity.Validate when the hostname carries a
// path separator or control character (it surfaces as a name on the wire).
var ErrHostnameInvalid = errors.New("config: identity hostname contains an illegal character")

// ErrHostnameTooLongForNetBIOS is returned by ValidateForNetBIOS when the hostname
// exceeds the NetBIOS name limit while the NetBIOS service is enabled.
var ErrHostnameTooLongForNetBIOS = errors.New("config: hostname exceeds the 15-byte NetBIOS name limit (constraint from the enabled NetBIOS service)")

// Key returns the well-known section key.
func (Identity) Key() string { return IdentityKey }

// Clone returns a copy. Identity is all value-typed fields, so a shallow copy is a
// deep copy.
func (i Identity) Clone() Identity { return i }

// Validate is the baseline check that always applies (§4-bis): a hostname, once a
// consumer has defaulted it, must not carry a path separator or control character —
// it is surfaced as a name on the SMB/NetBIOS/browser wire. An empty hostname is
// allowed here (a consumer derives a default); the NetBIOS length/case rule is a
// separate, consumer-gated check (ValidateForNetBIOS).
func (i Identity) Validate() error {
	for _, r := range i.Hostname {
		if r < 0x20 || r == 0x7f || r == '/' || r == '\\' {
			return ErrHostnameInvalid
		}
	}
	return nil
}

// ValidateForNetBIOS layers the NetBIOS consumer constraint onto the single Identity
// value: the hostname must fit the 15-byte NetBIOS name limit. It is applied ONLY
// when the NetBIOS service is enabled, so a 20-char hostname stays legal for an
// SMB-over-:445 / AFP-only server but is rejected once NetBIOS is turned on — keeping
// the limit where it belongs (NetBIOS) instead of baking a NetBIOS rule into a field
// SMB-without-NetBIOS also uses (§4-bis). Callers run Validate first.
func (i Identity) ValidateForNetBIOS() error {
	if len(i.NetBIOSName()) > NetBIOSNameMaxLen {
		return ErrHostnameTooLongForNetBIOS
	}
	return nil
}

// NetBIOSName renders the hostname as the NetBIOS consumer claims it: upper-cased and
// trimmed (NetBIOS names are case-insensitive upper-case). It does NOT truncate — an
// over-length name is a validation failure (ValidateForNetBIOS), not silently cut.
func (i Identity) NetBIOSName() string {
	return strings.ToUpper(strings.TrimSpace(i.Hostname))
}
