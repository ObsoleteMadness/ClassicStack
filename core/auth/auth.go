// Package auth is the protocol-neutral authentication seam the file services
// (AFP, SMB) consult to decide who may use the server and which shares an
// identity may see. It is a coarse "who may connect / which shares are visible"
// gate, NOT file-level ACLs — matching the compatibility-server posture (a
// vintage-client server, not an enterprise auth product).
//
// The package keeps modern primitives at rest (salted PBKDF2-SHA256 hashes; see
// cred.go) even though the legacy wire protocols that USE the credential are weak
// (AFP "Cleartxt Passwrd", SMB cleartext logon). That asymmetry is deliberate:
// modern on our side of the bridge, faithful to the client's insecure dialect on
// the wire.
//
// core discipline: this package imports only stdlib crypto (crypto/hmac,
// crypto/rand, crypto/sha256, crypto/subtle) plus errors — no net, no
// encoding/binary, no encoding/json, no sqlite — so it compiles for
// embedded/TinyGo targets and passes the archtest gate (hex coding is
// hand-rolled in cred.go rather than encoding/hex, matching core/binaryprimitives'
// style elsewhere in core). A concrete file-backed store lives in the
// adapter/auth/local package (it needs os for file I/O — an adapter concern
// regardless of what core itself may import); a netless target that does not
// need it simply does not import it.
package auth

import (
	"errors"
	"strings"
)

// User is the stored view of one identity. It NEVER carries plaintext password or
// hash material — those stay internal to a UserStore implementation. This is the
// listing DTO the management UI displays.
type User struct {
	Name     string
	Disabled bool
}

// Authenticator validates a credential. It is the minimal contract a file service
// needs: there may be several implementations (the built-in local store, a future
// PAM adapter, a future Windows-SSPI adapter), but exactly one is wired per build,
// selected by config. A read-only backend (e.g. PAM) may implement only this.
type Authenticator interface {
	// Authenticate reports whether (username, password) is a valid credential.
	// A blank username / guest attempt is the caller's policy decision (the
	// services decide whether to permit guests), not this method's — callers
	// should not pass an empty username expecting a "guest OK" answer here.
	Authenticate(username, password string) (ok bool, err error)
}

// UserStore is an Authenticator that also manages its user set — the surface the
// web UI drives to enumerate, add, update, disable, and remove users. The
// built-in local store implements it in full.
type UserStore interface {
	Authenticator

	// Users enumerates the stored identities (no secret material).
	Users() ([]User, error)
	// SetUser adds a user, or resets an existing user's password. An empty
	// password is rejected (ErrEmptyPassword) — a disabled flag, not a blank
	// secret, is how an account is parked.
	SetUser(username, password string) error
	// SetDisabled parks/unparks an account without discarding its password.
	// A disabled user fails Authenticate. Unknown name → ErrNoSuchUser.
	SetDisabled(username string, disabled bool) error
	// RemoveUser deletes a user. Unknown name → ErrNoSuchUser.
	RemoveUser(username string) error
}

// GuestName is the well-known unauthenticated identity. It always appears in the
// user-administration list so operators can enable/disable guest logins; it is
// NOT a password account (Authenticate never succeeds for it). File services that
// support authentication consult GuestEnabled before admitting anonymous sessions.
const GuestName = "Guest"

var (
	// ErrNoSuchUser is returned by SetDisabled/RemoveUser for an unknown name.
	ErrNoSuchUser = errors.New("auth: no such user")
	// ErrEmptyUsername is returned by SetUser for a blank username.
	ErrEmptyUsername = errors.New("auth: empty username")
	// ErrEmptyPassword is returned by SetUser for a blank password (park an
	// account with SetDisabled instead).
	ErrEmptyPassword = errors.New("auth: empty password")
	// ErrGuestImmutable is returned when SetUser/RemoveUser targets GuestName —
	// Guest is a policy toggle (SetDisabled), not a password account.
	ErrGuestImmutable = errors.New("auth: Guest account cannot be added, removed, or given a password")
)

// GuestEnabler is an optional Authenticator capability: report whether unauthenticated
// (guest/anonymous) logins are currently permitted. Absent the interface, guests are
// allowed (the historical default). The built-in local store implements it via the
// always-present Guest row.
type GuestEnabler interface {
	GuestEnabled() bool
}

// GuestEnabled reports whether the authenticator currently permits guest logins.
// A nil authenticator, or one that does not implement GuestEnabler, returns true
// (compatibility default: guest open until an operator disables Guest). Accepts
// any Authenticator-shaped value (each file service defines its own Authenticator
// interface) so callers can pass their wired store without an import cycle.
func GuestEnabled(a any) bool {
	if a == nil {
		return true
	}
	if g, ok := a.(GuestEnabler); ok {
		return g.GuestEnabled()
	}
	return true
}

// IsGuestName reports whether name is the reserved Guest identity (case-insensitive).
func IsGuestName(name string) bool {
	return strings.EqualFold(strings.TrimSpace(name), GuestName)
}
