// Package local is the built-in, always-available file-backed auth.UserStore. It
// keeps users in an smbpasswd-style line-oriented file (one record per user,
// colon-separated, salted PBKDF2-SHA256 hashes), separate from server.toml so
// secrets never ride the main config or its numbered backups.
//
// It lives in the ADAPTER ring, not core, for one reason: generating a random
// salt needs crypto/rand, which transitively imports reflect — banned in core by
// the archtest gate (§1). The hashing/verification itself stays in core/auth
// (reflection-free PBKDF2); this adapter supplies the randomness and the os file
// I/O. It is the default store the way adapter/store/file and
// adapter/metastore/sqlite are defaults — pure stdlib, no new dependency, no build
// tag. A future PAM/Windows store is an additional adapter under adapter/auth/*.
//
// File format (one line per user; '#' comments and blank lines ignored):
//
//	username:saltHex:hashHex:flags
//
// flags is a (possibly empty) set of single letters; "D" marks the account
// disabled. The hash is PBKDF2-HMAC-SHA256 (see core/auth/cred.go) of the
// password under the per-user salt.
package local

import (
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ObsoleteMadness/ClassicStack/core/auth"
)

// record is one in-memory user row.
type record struct {
	cred     auth.Credential
	disabled bool
}

// Store is the file-backed user store. It loads the whole file into memory on
// Open and rewrites it atomically (temp + rename) on every mutation, mirroring
// the metastore mem-snapshot discipline. All methods are safe for concurrent use.
type Store struct {
	mu    sync.RWMutex
	path  string
	users map[string]*record // key: lower-cased username (case-insensitive match)
	names map[string]string  // lower-cased → original-case display name
}

// compile-time assertion: *Store is a full auth.UserStore.
var _ auth.UserStore = (*Store)(nil)

// ErrMalformedFile is returned by Open when a non-comment line cannot be parsed.
var ErrMalformedFile = errors.New("auth/local: malformed users file")

// Open loads (or, for a missing file, starts empty against) the users file at
// path. A missing file is not an error — the first SetUser creates it.
func Open(path string) (*Store, error) {
	s := &Store{
		path:  path,
		users: make(map[string]*record),
		names: make(map[string]string),
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

// Authenticate reports whether (username, password) is a valid, enabled
// credential. Unknown user, disabled account, or wrong password all return
// (false, nil) — the caller cannot distinguish them (no user-enumeration oracle).
func (s *Store) Authenticate(username, password string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.users[strings.ToLower(username)]
	if !ok || r.disabled {
		return false, nil
	}
	return r.cred.Verify(password), nil
}

// Users returns the stored identities (no secret material), sorted by name.
func (s *Store) Users() ([]auth.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]auth.User, 0, len(s.users))
	for key, r := range s.users {
		out = append(out, auth.User{Name: s.names[key], Disabled: r.disabled})
	}
	sortUsers(out)
	return out, nil
}

// SetUser adds a user or resets an existing user's password (preserving the
// disabled flag). An empty username/password is rejected.
func (s *Store) SetUser(username, password string) error {
	if strings.TrimSpace(username) == "" {
		return auth.ErrEmptyUsername
	}
	if password == "" {
		return auth.ErrEmptyPassword
	}
	salt := make([]byte, auth.SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return err
	}
	cred := auth.DeriveCredential(password, salt)

	s.mu.Lock()
	defer s.mu.Unlock()
	key := strings.ToLower(username)
	if r, ok := s.users[key]; ok {
		r.cred = cred // reset password; keep disabled flag and display name
	} else {
		s.users[key] = &record{cred: cred}
		s.names[key] = username
	}
	return s.save()
}

// SetDisabled parks/unparks an account. Unknown name → auth.ErrNoSuchUser.
func (s *Store) SetDisabled(username string, disabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.users[strings.ToLower(username)]
	if !ok {
		return auth.ErrNoSuchUser
	}
	r.disabled = disabled
	return s.save()
}

// RemoveUser deletes a user. Unknown name → auth.ErrNoSuchUser.
func (s *Store) RemoveUser(username string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := strings.ToLower(username)
	if _, ok := s.users[key]; !ok {
		return auth.ErrNoSuchUser
	}
	delete(s.users, key)
	delete(s.names, key)
	return s.save()
}

// load reads the file into memory. A missing file leaves an empty store. A line
// that is not a comment/blank but does not parse fails loudly (ErrMalformedFile).
func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		fields := strings.Split(line, ":")
		if len(fields) < 3 {
			return ErrMalformedFile
		}
		name := fields[0]
		cred, err := auth.ParseCredential(fields[1], fields[2])
		if err != nil {
			return ErrMalformedFile
		}
		flags := ""
		if len(fields) >= 4 {
			flags = fields[3]
		}
		key := strings.ToLower(name)
		s.users[key] = &record{cred: cred, disabled: strings.ContainsRune(flags, 'D')}
		s.names[key] = name
	}
	return nil
}

// save rewrites the whole file atomically (temp in the same dir + rename). Caller
// holds the write lock, so it builds the ordered view directly (it must NOT call
// the RLock-taking Users() — sync.RWMutex is not re-entrant). The file is written
// 0600 (secrets).
func (s *Store) save() error {
	ordered := make([]auth.User, 0, len(s.users))
	for key, r := range s.users {
		ordered = append(ordered, auth.User{Name: s.names[key], Disabled: r.disabled})
	}
	sortUsers(ordered)

	var b strings.Builder
	b.WriteString("# classicstack users — name:saltHex:hashHex:flags (D=disabled). Do not edit hashes by hand.\n")
	for _, u := range ordered {
		r := s.users[strings.ToLower(u.Name)]
		flags := ""
		if r.disabled {
			flags = "D"
		}
		b.WriteString(u.Name)
		b.WriteByte(':')
		b.WriteString(r.cred.SaltHex())
		b.WriteByte(':')
		b.WriteString(r.cred.HashHex())
		b.WriteByte(':')
		b.WriteString(flags)
		b.WriteByte('\n')
	}

	dir := filepath.Dir(s.path)
	tmp, err := os.CreateTemp(dir, ".cs-users-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() { tmp.Close(); os.Remove(tmpName) }
	if err := tmp.Chmod(0o600); err != nil {
		cleanup()
		return err
	}
	if _, err := tmp.WriteString(b.String()); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, s.path)
}

func sortUsers(u []auth.User) {
	// insertion sort (small lists; avoids importing sort for one call).
	for i := 1; i < len(u); i++ {
		for j := i; j > 0 && strings.ToLower(u[j-1].Name) > strings.ToLower(u[j].Name); j-- {
			u[j-1], u[j] = u[j], u[j-1]
		}
	}
}
