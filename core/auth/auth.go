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
// core discipline: this package imports only stdlib crypto (crypto/sha256,
// crypto/rand, crypto/subtle, encoding/hex) — no net, no reflect, no
// encoding/binary, no sqlite — so it compiles for embedded/TinyGo targets and
// passes the archtest gate. A concrete file-backed store lives in the
// core/auth/local subpackage (it needs os); a netless target that does not need
// it simply does not import it.
package auth

import "errors"

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

var (
	// ErrNoSuchUser is returned by SetDisabled/RemoveUser for an unknown name.
	ErrNoSuchUser = errors.New("auth: no such user")
	// ErrEmptyUsername is returned by SetUser for a blank username.
	ErrEmptyUsername = errors.New("auth: empty username")
	// ErrEmptyPassword is returned by SetUser for a blank password (park an
	// account with SetDisabled instead).
	ErrEmptyPassword = errors.New("auth: empty password")
)
